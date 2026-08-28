package share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPublicPort    = 39124
	defaultShareTTL      = 7 * 24 * time.Hour
	defaultGCInterval    = 1 * time.Hour
	defaultServerTimeout = 10 * time.Second
)

// adminAddrTCPPrefix forces a TCP listener for tests and explicit overrides.
// Production callers leave AdminAddr empty so the daemon binds the UDS path
// from StatePaths.AdminSocket.
const adminAddrTCPPrefix = "tcp:"

type DaemonConfig struct {
	Paths       StatePaths
	AdminAddr   string
	PublicPort  int
	TokenBytes  int
	ExternalURL string
}

type Daemon struct {
	cfg          DaemonConfig
	store        *Store
	secret       []byte
	publicBase   string
	externalBase string
	failedAuth   *failedAuthLimiter
	mu           sync.RWMutex
}

func NewDaemon(cfg DaemonConfig) (*Daemon, error) {
	if cfg.PublicPort == 0 {
		cfg.PublicPort = DefaultPublicPort
	}
	if cfg.TokenBytes == 0 {
		cfg.TokenBytes = DefaultTokenBytes
	}
	if cfg.TokenBytes < MinTokenBytes {
		return nil, fmt.Errorf("token_bytes=%d is below minimum %d", cfg.TokenBytes, MinTokenBytes)
	}
	if cfg.Paths.BaseDir == "" {
		paths, err := DefaultStatePaths()
		if err != nil {
			return nil, err
		}
		cfg.Paths = paths
	}
	if cfg.AdminAddr == "" {
		cfg.AdminAddr = cfg.Paths.AdminSocket
	}
	if cfg.AdminAddr == "" {
		return nil, fmt.Errorf("admin address is empty and StatePaths.AdminSocket is unset")
	}
	if err := cfg.Paths.Ensure(); err != nil {
		return nil, err
	}

	store, err := OpenStore(cfg.Paths.DBPath)
	if err != nil {
		return nil, err
	}
	secret, err := LoadOrCreateSecret(cfg.Paths.SecretPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	return &Daemon{
		cfg:        cfg,
		store:      store,
		secret:     secret,
		failedAuth: newFailedAuthLimiter(defaultFailedAuthLimit, defaultFailedAuthWindow),
	}, nil
}

func (d *Daemon) Close() error {
	if d.store != nil {
		return d.store.Close()
	}
	return nil
}

func (d *Daemon) Run(ctx context.Context) error {
	ip, err := LocalTailscaleIPv4()
	if err != nil {
		return err
	}
	dnsName, err := LocalTailscaleMagicDNS()
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.publicBase = fmt.Sprintf("http://%s:%d", dnsName, d.cfg.PublicPort)
	d.externalBase = d.publicBase
	if d.cfg.ExternalURL != "" {
		d.externalBase = strings.TrimRight(d.cfg.ExternalURL, "/")
	} else if servedBase, resolveErr := ExternalShareBaseURL(d.cfg.PublicPort); resolveErr == nil && servedBase != "" {
		d.externalBase = servedBase
	}
	d.mu.Unlock()

	publicListener, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", ip, d.cfg.PublicPort))
	if err != nil {
		return fmt.Errorf("listen public: %w", err)
	}
	defer func() { _ = publicListener.Close() }()

	loopbackListener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", d.cfg.PublicPort))
	if err != nil {
		return fmt.Errorf("listen loopback: %w", err)
	}
	defer func() { _ = loopbackListener.Close() }()

	adminListener, err := listenAdmin(d.cfg.AdminAddr)
	if err != nil {
		return fmt.Errorf("listen admin: %w", err)
	}
	defer func() { _ = adminListener.Close() }()

	publicHandler := d.publicMux()
	publicServer := &http.Server{
		Handler:           publicHandler,
		ReadHeaderTimeout: defaultServerTimeout,
	}
	loopbackServer := &http.Server{
		Handler:           d.publicMux(),
		ReadHeaderTimeout: defaultServerTimeout,
	}
	adminServer := &http.Server{
		Handler:           d.adminMux(),
		ReadHeaderTimeout: defaultServerTimeout,
	}

	errCh := make(chan error, 3)
	go func() {
		err := publicServer.Serve(publicListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("public server: %w", err)
		}
	}()
	go func() {
		err := loopbackServer.Serve(loopbackListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("loopback server: %w", err)
		}
	}()
	go func() {
		err := adminServer.Serve(adminListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin server: %w", err)
		}
	}()

	gcTicker := time.NewTicker(defaultGCInterval)
	defer gcTicker.Stop()
	failedAuthCleanupTicker := time.NewTicker(defaultFailedAuthCleanupInterval)
	defer failedAuthCleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = publicServer.Shutdown(shutdownCtx)
			_ = loopbackServer.Shutdown(shutdownCtx)
			_ = adminServer.Shutdown(shutdownCtx)
			return nil
		case err := <-errCh:
			return err
		case <-gcTicker.C:
			d.gcExpiredShares(time.Now().UTC())
		case <-failedAuthCleanupTicker.C:
			if d.failedAuth != nil {
				d.failedAuth.Cleanup(time.Now().UTC())
			}
		}
	}
}

func (d *Daemon) PublicBaseURL() string {
	d.mu.RLock()
	base := d.publicBase
	d.mu.RUnlock()
	return base
}

func (d *Daemon) ExternalBaseURL() string {
	d.mu.RLock()
	base := d.externalBase
	if base == "" {
		base = d.publicBase
	}
	d.mu.RUnlock()
	return base
}

func (d *Daemon) publicMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", d.handlePublicHealth)
	mux.HandleFunc("/s/", d.handlePreview)
	mux.HandleFunc("/h/", d.handleHTMLArtifact)
	mux.HandleFunc("/r/", d.handleRaw)
	return securityHeaders(failedAuthRateLimit(d.failedAuth, mux))
}

// previewCSP restricts what the rendered preview pages can load.
// script-src and style-src include 'unsafe-inline' because the preview pages
// embed inline CSS and short bootstrap scripts; SRI on every external CDN
// asset is the durable defense against supply-chain compromise. The other
// directives close clickjacking, base-uri tampering, form-action exfil, and
// arbitrary worker/connect destinations.
const previewCSP = "default-src 'none'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
	"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
	"img-src 'self' data: https:; " +
	"media-src 'self'; " +
	"font-src 'self' data: https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
	"connect-src 'self'; " +
	"frame-src 'self'; " +
	"worker-src 'self' blob: https://cdn.jsdelivr.net; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'"

// rawCSP applies to /r/ raw responses. The route already forces
// Content-Disposition: attachment for HTML and X-Content-Type-Options: nosniff,
// so this is defense-in-depth: even if a browser ignored the disposition
// header, the sandbox plus default-src 'none' keeps the document inert.
const rawCSP = "default-src 'none'; sandbox"

// htmlArtifactCSP permits self-contained HTML artifacts to execute their own
// inline CSS and JavaScript while denying fetch/XHR, external runtime subresources,
// forms, nested frames, plugins, parent/top navigation, and access to Ferry's
// origin. The sandbox intentionally omits allow-same-origin so artifact scripts
// run under an opaque origin. An artifact may still navigate its own sandboxed
// frame, but the token is stripped first and referrers are suppressed.
const htmlArtifactCSP = "default-src 'none'; " +
	"script-src 'unsafe-inline'; " +
	"style-src 'unsafe-inline'; " +
	"img-src data: blob:; " +
	"media-src data: blob:; " +
	"font-src data:; " +
	"connect-src 'none'; " +
	"object-src 'none'; " +
	"frame-src 'none'; " +
	"frame-ancestors 'self'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"sandbox allow-scripts"

// securityHeaders wraps the public handler with response headers that
// prevent indexing, referrer leakage, MIME-type sniffing, and frame
// embedding, and applies a Content-Security-Policy appropriate for each
// route family.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		h.Set("Pragma", "no-cache")
		switch {
		case strings.HasPrefix(r.URL.Path, "/r/"):
			h.Set("Content-Security-Policy", rawCSP)
		case strings.HasPrefix(r.URL.Path, "/h/"):
			h.Set("Content-Security-Policy", htmlArtifactCSP)
			h.Set("X-Frame-Options", "SAMEORIGIN")
		case strings.HasPrefix(r.URL.Path, "/s/"):
			h.Set("Content-Security-Policy", previewCSP)
		}
		next.ServeHTTP(w, r)
	})
}

// listenAdmin binds the admin API. A path-shaped address (or a "unix:" prefix)
// listens on a Unix domain socket with mode 0600 in a 0700 parent. A "tcp:"
// prefix or a host:port address listens on TCP for tests.
func listenAdmin(addr string) (net.Listener, error) {
	network, address := parseAdminAddr(addr)
	if network == "unix" {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove stale admin socket: %w", err)
		}
		if err := ensureDirMode(filepath.Dir(address), privateDirMode); err != nil {
			return nil, fmt.Errorf("admin socket dir: %w", err)
		}
		ln, err := net.Listen("unix", address)
		if err != nil {
			return nil, err
		}
		if err := os.Chmod(address, privateFileMode); err != nil {
			_ = ln.Close()
			_ = os.Remove(address)
			return nil, fmt.Errorf("chmod admin socket: %w", err)
		}
		return ln, nil
	}
	return net.Listen(network, address)
}

// parseAdminAddr decides whether the daemon should listen on a Unix domain
// socket or a TCP address. The default for production callers is unix.
func parseAdminAddr(addr string) (string, string) {
	if strings.HasPrefix(addr, "unix:") {
		return "unix", strings.TrimPrefix(addr, "unix:")
	}
	if strings.HasPrefix(addr, adminAddrTCPPrefix) {
		return "tcp", strings.TrimPrefix(addr, adminAddrTCPPrefix)
	}
	if strings.HasPrefix(addr, "/") {
		return "unix", addr
	}
	return "tcp", addr
}

func (d *Daemon) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/health", d.handleAdminHealth)
	mux.HandleFunc("/admin/share", d.handleAdminCreateShare)
	mux.HandleFunc("/admin/shares", d.handleAdminListShares)
	mux.HandleFunc("/admin/shares/", d.handleAdminShareByID)
	return mux
}

func splitSharePath(rawPath string, prefix string) (string, string, bool) {
	if !strings.HasPrefix(rawPath, prefix) {
		return "", "", false
	}
	tail := strings.TrimPrefix(rawPath, prefix)
	tail = strings.TrimPrefix(tail, "/")
	if tail == "" {
		return "", "", false
	}

	parts := strings.SplitN(tail, "/", 2)
	id := strings.TrimSpace(parts[0])
	if id == "" {
		return "", "", false
	}
	if len(parts) == 1 {
		return id, "", true
	}
	rel := strings.TrimPrefix(parts[1], "/")
	return id, rel, true
}

func escapeRel(rel string) string {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	if rel == "" {
		return ""
	}
	parts := strings.Split(rel, "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.Join(escaped, "/")
}

func (d *Daemon) gcExpiredShares(now time.Time) {
	shares, err := d.store.ExpiredShares(now)
	if err != nil {
		return
	}
	for _, share := range shares {
		if share.Mode == ModeSnapshot {
			_ = os.RemoveAll(filepath.Join(d.cfg.Paths.SnapshotsDir, share.ID))
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, errCode string, message string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{
			"code":    errCode,
			"message": message,
		},
	})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
