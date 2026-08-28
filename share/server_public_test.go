package share

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildPreviewPathUsesExternalBaseURL(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		externalBase: "https://host.example.ts.net/share",
	}

	got := d.buildPreviewPath("share123", "docs/readme.md", "token123")
	want := "https://host.example.ts.net/share/s/share123/docs/readme.md?t=token123"
	if got != want {
		t.Fatalf("buildPreviewPath() = %q, want %q", got, want)
	}
}

func TestBuildRawPathFallsBackToDaemonRoot(t *testing.T) {
	t.Parallel()

	d := &Daemon{}

	got := d.buildRawPath("share123", "docs/readme.md", "token123")
	want := "/r/share123/docs/readme.md?t=token123"
	if got != want {
		t.Fatalf("buildRawPath() = %q, want %q", got, want)
	}
}

func TestBuildPreviewPathMarksPDFNativeViewerLinks(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		externalBase: "https://host.example.ts.net/share",
	}

	got := d.buildPreviewPath("share123", "docs/sample.pdf", "token123")
	want := "https://host.example.ts.net/share/s/share123/docs/sample.pdf?t=token123&pv=native"
	if got != want {
		t.Fatalf("buildPreviewPath() = %q, want %q", got, want)
	}
}

func TestPDFPreviewRedirectsToRawRoute(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-pdf",
		SourcePath: dir,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest(http.MethodGet, "/s/"+share.ID+"/sample.pdf?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != http.StatusFound {
		t.Fatalf("handlePreview status = %d, want %d", res.Code, http.StatusFound)
	}
	want := "/r/" + share.ID + "/sample.pdf?t=" + token
	if got := res.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}
