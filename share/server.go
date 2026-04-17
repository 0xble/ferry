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
	DefaultAdminAddr     = "127.0.0.1:39125"
	DefaultPublicPort    = 39124
	defaultShareTTL      = 7 * 24 * time.Hour
	defaultGCInterval    = 1 * time.Hour
	defaultServerTimeout = 10 * time.Second
)

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
	if cfg.AdminAddr == "" {
		cfg.AdminAddr = DefaultAdminAddr
	}
	if cfg.PublicPort == 0 {
		cfg.PublicPort = DefaultPublicPort
	}
	if cfg.TokenBytes == 0 {
		cfg.TokenBytes = DefaultTokenBytes
	}
	if cfg.Paths.BaseDir == "" {
		paths, err := DefaultStatePaths()
		if err != nil {
			return nil, err
		}
		cfg.Paths = paths
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

	adminListener, err := net.Listen("tcp", d.cfg.AdminAddr)
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
	mux.HandleFunc("/r/", d.handleRaw)
	return noIndex(failedAuthRateLimit(d.failedAuth, mux))
}

// noIndex wraps a handler to set response headers on every public response,
// preventing indexing and referrer leakage from shared URLs.
func noIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
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
