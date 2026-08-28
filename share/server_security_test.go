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

func TestAdminHealthUsesLoopbackProbeURL(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.PublicPort = 4242
	res := httptest.NewRecorder()
	d.handleAdminHealth(res, httptest.NewRequest(http.MethodGet, "/admin/health", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("admin health status = %d, want %d", res.Code, http.StatusOK)
	}
	if !strings.Contains(res.Body.String(), `"health_base_url":"http://127.0.0.1:4242"`) {
		t.Fatalf("expected loopback health URL, got %q", res.Body.String())
	}
}

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
	if got := res.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := res.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want %q", got, "DENY")
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestPreviewRouteSetsContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	share := createTestShare(t, d)
	handler := d.publicMux()

	req := httptest.NewRequest(http.MethodGet, "/s/"+share.ID+"?t=bad-token", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	csp := res.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatalf("expected Content-Security-Policy on /s/, got empty")
	}
	for _, want := range []string{
		"default-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"script-src",
		"https://cdn.jsdelivr.net",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %q\ngot: %s", want, csp)
		}
	}
}

func TestRawRouteSetsRestrictiveCSP(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	share := createTestShare(t, d)
	handler := d.publicMux()

	req := httptest.NewRequest(http.MethodGet, "/r/"+share.ID+"?t=bad-token", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	csp := res.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatalf("expected Content-Security-Policy on /r/, got empty")
	}
	for _, want := range []string{"default-src 'none'", "sandbox"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("raw CSP missing %q\ngot: %s", want, csp)
		}
	}
}

func TestPreviewPagesIncludeSubresourceIntegrity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind PreviewKind
	}{
		{"diff", PreviewDiff},
		{"code", PreviewCode},
		{"csv", PreviewCSV},
		{"pdf", PreviewPDF},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := RenderPreviewPage("sample."+string(tc.kind), tc.kind, "/r/abc?t=tok", nil)
			if !strings.Contains(body, `integrity="sha384-`) {
				t.Fatalf("preview kind %s missing SRI integrity attribute", tc.kind)
			}
			if !strings.Contains(body, `crossorigin="anonymous"`) {
				t.Fatalf("preview kind %s missing crossorigin=anonymous", tc.kind)
			}
		})
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

func TestHandlePreviewEmbedsHTMLInScriptOnlySandbox(t *testing.T) {
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
	if !strings.Contains(body, `sandbox="allow-scripts"`) {
		t.Fatalf("expected script-only sandboxed iframe, got %q", body)
	}
	if strings.Contains(body, "allow-same-origin") {
		t.Fatalf("HTML iframe must not retain the Ferry origin: %q", body)
	}
	for _, want := range []string{`const marker = "/s/"`, `+ "/h/" +`, `frame.src = artifactURL.pathname + artifactURL.search`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected proxy-prefix-safe HTML artifact routing marker %q, got %q", want, body)
		}
	}
	if strings.Contains(body, `alert("owned")`) {
		t.Fatalf("unexpected raw HTML in preview shell: %q", body)
	}
}

func TestHTMLArtifactRouteRunsInlineScriptsAfterTokenStrippingRedirect(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	root := t.TempDir()
	source := `<!doctype html><button id="demo">Before</button><script>document.querySelector('#demo').textContent='After'</script>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	share := createDirectoryShare(t, d, root)
	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	initialReq := httptest.NewRequest(http.MethodGet, "/h/"+share.ID+"/index.html?t="+token, nil)
	initialRes := httptest.NewRecorder()

	d.publicMux().ServeHTTP(initialRes, initialReq)

	if initialRes.Code != http.StatusSeeOther {
		t.Fatalf("initial HTML artifact status = %d, want %d", initialRes.Code, http.StatusSeeOther)
	}
	if got := initialRes.Header().Get("Location"); got != "index.html" {
		t.Fatalf("redirect Location = %q, want token-free artifact URL", got)
	}
	cookies := initialRes.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("artifact cookie must be HttpOnly and SameSite=Strict: %#v", cookie)
	}
	if cookie.Path != "/" {
		t.Fatalf("artifact cookie Path = %q, want / for proxy-prefix compatibility", cookie.Path)
	}

	req := httptest.NewRequest(http.MethodGet, "/h/"+share.ID+"/index.html", nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	d.publicMux().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("HTML artifact status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if got := res.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	csp := res.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"style-src 'unsafe-inline'",
		"connect-src 'none'",
		"frame-ancestors 'self'",
		"form-action 'none'",
		"sandbox allow-scripts",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("HTML artifact CSP missing %q\ngot: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("HTML artifact CSP must not retain the Ferry origin: %s", csp)
	}
	if body := res.Body.String(); body != source {
		t.Fatalf("HTML artifact body = %q, want %q", body, source)
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
