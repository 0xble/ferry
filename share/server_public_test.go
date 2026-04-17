package share

import "testing"

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
