package share

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientHealthChecksAdminAndPublic(t *testing.T) {
	t.Parallel()

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer publicServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s"}`, publicServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	if err := client.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClientHealthUsesLoopbackHealthURLWhenTailnetURLIsUnresolvable(t *testing.T) {
	t.Parallel()

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer healthServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"http://unresolvable.invalid:39124","health_base_url":"%s"}`, healthServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	if err := client.Health(); err != nil {
		t.Fatalf("Health should use the loopback health URL: %v", err)
	}
}

func TestClientHealthRejectsNonLoopbackHealthURLWithoutFallingBack(t *testing.T) {
	t.Parallel()

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer publicServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s","health_base_url":"https://example.com"}`, publicServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil {
		t.Fatal("expected non-loopback health_base_url to be rejected")
	}
	if !strings.Contains(err.Error(), "loopback IP address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildHealthURLRejectsNonLiteralLoopbackHosts(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{
		"http://localhost:39124",
		"http://localhost.:39124",
		"http://127.0.0.1.:39124",
		"https://example.com",
	} {
		baseURL := baseURL
		t.Run(baseURL, func(t *testing.T) {
			t.Parallel()
			if _, err := buildHealthURL(baseURL, true); err == nil {
				t.Fatalf("expected %q to be rejected", baseURL)
			}
		})
	}
}

func TestClientHealthRejectsRedirects(t *testing.T) {
	t.Parallel()

	var redirectedHits atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/healthz", http.StatusFound)
	}))
	defer redirectServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"health_base_url":"%s"}`, redirectServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil || !strings.Contains(err.Error(), "public health status 302") {
		t.Fatalf("expected redirect to fail closed, got %v", err)
	}
	if got := redirectedHits.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want 0", got)
	}
}

func TestClientHealthFailsWhenPublicHealthFails(t *testing.T) {
	t.Parallel()

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer publicServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s"}`, publicServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil {
		t.Fatal("expected Health to fail when public health is not ok")
	}
	if !strings.Contains(err.Error(), "public health status") {
		t.Fatalf("expected public health error, got: %v", err)
	}
}

// shortSocketDir returns a tempdir under /tmp so unix socket paths stay
// inside the macOS sun_path length limit (104 bytes). The default
// t.TempDir() path lives under /var/folders/... which is too long.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ferry-uds-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestClientDialsUnixDomainSocket(t *testing.T) {
	t.Parallel()

	dir := shortSocketDir(t)
	socketPath := filepath.Join(dir, "admin.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/shares", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	go func() { _ = http.Serve(ln, mux) }()

	client := NewClient(socketPath)
	shares, err := client.ListShares()
	if err != nil {
		t.Fatalf("ListShares over unix socket: %v", err)
	}
	if len(shares) != 0 {
		t.Fatalf("expected empty share list, got %d", len(shares))
	}
}

// TestUnixClientHealthProbesPublicURLOverTCP guards against a regression
// where the UDS admin client also routed the public health probe to the
// admin socket because http.Transport.DialContext ignored the requested
// address.
func TestUnixClientHealthProbesPublicURLOverTCP(t *testing.T) {
	t.Parallel()

	dir := shortSocketDir(t)
	socketPath := filepath.Join(dir, "admin.sock")

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer publicServer.Close()

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/admin/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s"}`, publicServer.URL)
	})
	go func() { _ = http.Serve(ln, adminMux) }()

	client := NewClient(socketPath)
	if err := client.Health(); err != nil {
		t.Fatalf("Health over UDS admin + TCP public: %v", err)
	}
}

func TestListenAdminCreatesPrivateSocket(t *testing.T) {
	t.Parallel()

	dir := shortSocketDir(t)
	socketPath := filepath.Join(dir, "nested", "admin.sock")

	ln, err := listenAdmin(socketPath)
	if err != nil {
		t.Fatalf("listenAdmin: %v", err)
	}
	defer func() { _ = ln.Close() }()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", got)
	}

	parent, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if got := parent.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent perm = %o, want 0700", got)
	}
}

func TestListenAdminReplacesStaleSocket(t *testing.T) {
	t.Parallel()

	dir := shortSocketDir(t)
	socketPath := filepath.Join(dir, "admin.sock")

	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	ln, err := listenAdmin(socketPath)
	if err != nil {
		t.Fatalf("listenAdmin with stale socket: %v", err)
	}
	defer func() { _ = ln.Close() }()
}

func TestClientHealthFailsWhenPublicURLMissing(t *testing.T) {
	t.Parallel()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil {
		t.Fatal("expected Health to fail when public_base_url is missing")
	}
	if !strings.Contains(err.Error(), "missing public base url") {
		t.Fatalf("unexpected error: %v", err)
	}
}
