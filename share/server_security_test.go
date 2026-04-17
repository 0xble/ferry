package share

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublicMuxSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	d.publicMux().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("publicMux status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q, want %q", got, "noindex, nofollow")
	}
	if got := res.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "no-referrer")
	}
}

func TestPublicMuxRateLimitsFailedAuthByIP(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	share := createTestShare(t, d)
	handler := d.publicMux()

	for i := 1; i <= defaultFailedAuthLimit; i++ {
		res := serveFromIP(handler, http.MethodGet, "/s/"+share.ID+"?t=bad-token", "203.0.113.10:1234")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", i, res.Code, http.StatusUnauthorized)
		}
	}

	blocked := serveFromIP(handler, http.MethodGet, "/s/"+share.ID+"?t=bad-token", "203.0.113.10:1234")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want %d", blocked.Code, http.StatusTooManyRequests)
	}
	if got := blocked.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("blocked Referrer-Policy = %q, want %q", got, "no-referrer")
	}

	otherIP := serveFromIP(handler, http.MethodGet, "/r/"+share.ID+"?t=bad-token", "203.0.113.11:4321")
	if otherIP.Code != http.StatusUnauthorized {
		t.Fatalf("other IP status = %d, want %d", otherIP.Code, http.StatusUnauthorized)
	}
}

func TestFailedAuthRateLimitIgnoresSuccessfulRequests(t *testing.T) {
	t.Parallel()

	limiter := newFailedAuthLimiter(2, time.Minute)
	handler := failedAuthRateLimit(limiter, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ok") == "1" {
			w.WriteHeader(http.StatusOK)
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
	}))

	if res := serveFromIP(handler, http.MethodGet, "/s/test?ok=1", "203.0.113.20:9999"); res.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d", res.Code, http.StatusOK)
	}
	if res := serveFromIP(handler, http.MethodGet, "/s/test", "203.0.113.20:9999"); res.Code != http.StatusUnauthorized {
		t.Fatalf("first failure status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	if res := serveFromIP(handler, http.MethodGet, "/s/test", "203.0.113.20:9999"); res.Code != http.StatusUnauthorized {
		t.Fatalf("second failure status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	if res := serveFromIP(handler, http.MethodGet, "/s/test", "203.0.113.20:9999"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("third failure status = %d, want %d", res.Code, http.StatusTooManyRequests)
	}
}

func TestFailedAuthLimiterExpiresEntries(t *testing.T) {
	t.Parallel()

	limiter := newFailedAuthLimiter(1, time.Minute)
	now := time.Unix(1, 0).UTC()
	ip := "203.0.113.30"

	limiter.RecordFailure(ip, now)
	if limiter.Allow(ip, now.Add(30*time.Second)) {
		t.Fatal("Allow() before expiry = true, want false")
	}
	if !limiter.Allow(ip, now.Add(time.Minute)) {
		t.Fatal("Allow() at expiry = false, want true")
	}

	limiter.RecordFailure(ip, now)
	limiter.Cleanup(now.Add(time.Minute))
	if _, ok := limiter.entries[ip]; ok {
		t.Fatal("Cleanup() did not remove expired entry")
	}
}

func TestHandlePreviewDisablesHTMLExecution(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	root := t.TempDir()
	htmlPath := filepath.Join(root, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<script>alert("owned")</script>`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	share := createDirectoryShare(t, d, root)
	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest(http.MethodGet, "/s/"+share.ID+"/index.html?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("handlePreview status = %d, want %d", res.Code, http.StatusOK)
	}
	body := res.Body.String()
	if !strings.Contains(body, "HTML preview is disabled for safety.") {
		t.Fatalf("expected HTML preview warning, got %q", body)
	}
	if strings.Contains(body, `alert("owned")`) {
		t.Fatalf("unexpected raw html in preview response: %q", body)
	}
}

func TestHandleRawForcesHTMLDownload(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	root := t.TempDir()
	htmlPath := filepath.Join(root, "unsafe.html")
	if err := os.WriteFile(htmlPath, []byte(`<script>alert("owned")</script>`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	share := createDirectoryShare(t, d, root)
	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest(http.MethodGet, "/r/"+share.ID+"/unsafe.html?t="+token, nil)
	res := httptest.NewRecorder()

	d.handleRaw(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("handleRaw status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	if got := res.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/octet-stream") {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
}

func createDirectoryShare(t *testing.T, d *Daemon, root string) Share {
	t.Helper()

	now := time.Now().UTC()
	share := Share{
		ID:         "share-dir",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	return share
}

func createTestShare(t *testing.T, d *Daemon) Share {
	t.Helper()

	now := time.Now().UTC()
	share := Share{
		ID:         "share-auth",
		SourcePath: t.TempDir(),
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	return share
}

func serveFromIP(handler http.Handler, method string, target string, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remoteAddr
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
