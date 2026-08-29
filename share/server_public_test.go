package share

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestContainHTMLArtifactViewportInjectsGuardAfterDoctype(t *testing.T) {
	t.Parallel()

	source := `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="style-src 'none'"><style>html{overflow-x:auto!important}</style></head><body><script>const closingTag = "</html>"</script><main>Artifact</main></body></html>`
	artifact := renderHTMLArtifactForTest(t, source)
	want := `<!doctype html>` + htmlViewportContainmentMarkup + strings.TrimPrefix(source, `<!doctype html>`)

	if artifact != want {
		t.Fatalf("artifact = %q, want guard immediately after doctype", artifact)
	}
	if !strings.Contains(artifact, `<script>const closingTag = "</html>"</script>`) {
		t.Fatalf("viewport guard must not split authored script contents, got %q", artifact)
	}
}

func TestContainHTMLArtifactViewportSupportsHTMLFragments(t *testing.T) {
	t.Parallel()

	source := `<main style="width:200vw">Artifact</main><!--`
	artifact := renderHTMLArtifactForTest(t, source)
	if artifact != htmlViewportContainmentMarkup+source {
		t.Fatalf("fragment artifact = %q, want viewport guard before source", artifact)
	}
}

func TestHTMLViewportGuardOffsetPreservesLeadingCommentsAndDoctype(t *testing.T) {
	t.Parallel()

	source := []byte("\ufeff\n<!-- generated -->\n<!DOCTYPE html><html></html>")
	offset := htmlViewportGuardOffset(source)
	if got := string(source[:offset]); got != "\ufeff\n<!-- generated -->\n<!DOCTYPE html>" {
		t.Fatalf("doctype prefix = %q", got)
	}
}

func TestHTMLViewportGuardOffsetStreamsLongLeadingComments(t *testing.T) {
	t.Parallel()

	prefix := "<!--" + strings.Repeat("generated-", 8<<10) + "--->\n<!doctype html>"
	source := []byte(prefix + "<html></html>")
	offset, err := htmlViewportGuardOffsetReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatalf("htmlViewportGuardOffsetReader: %v", err)
	}
	if got := string(source[:offset]); got != prefix {
		t.Fatalf("long doctype prefix length = %d, want %d", len(got), len(prefix))
	}
}

func TestHTMLArtifactReadSeekerSupportsRangesAcrossInjection(t *testing.T) {
	t.Parallel()

	source := `<!doctype html><main>Artifact</main>`
	reader, err := newHTMLArtifactReadSeeker(bytes.NewReader([]byte(source)), int64(len(source)))
	if err != nil {
		t.Fatalf("newHTMLArtifactReadSeeker: %v", err)
	}
	want := `<!doctype html>` + htmlViewportContainmentMarkup + `<main>Artifact</main>`
	for _, tc := range []struct {
		name   string
		offset int64
		length int
	}{
		{name: "source prefix", offset: 0, length: len(`<!doctype html>`)},
		{name: "inside injection", offset: int64(len(`<!doctype html>`)) + 7, length: 23},
		{name: "source suffix", offset: int64(len(want) - len(`</main>`)), length: len(`</main>`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := reader.Seek(tc.offset, io.SeekStart); err != nil {
				t.Fatalf("Seek: %v", err)
			}
			got := make([]byte, tc.length)
			if _, err := io.ReadFull(reader, got); err != nil {
				t.Fatalf("ReadFull: %v", err)
			}
			if string(got) != want[tc.offset:tc.offset+int64(tc.length)] {
				t.Fatalf("range = %q, want %q", got, want[tc.offset:tc.offset+int64(tc.length)])
			}
		})
	}
}

func renderHTMLArtifactForTest(t *testing.T, source string) string {
	t.Helper()
	reader, err := newHTMLArtifactReadSeeker(bytes.NewReader([]byte(source)), int64(len(source)))
	if err != nil {
		t.Fatalf("newHTMLArtifactReadSeeker: %v", err)
	}
	artifact, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(artifact)
}
