package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xble/ferry/share"
)

// Ported from cmd/ferry/main_test.go. The tests that drove the old command
// functions through package globals (unshare's revoke failure, --open) now
// run the generated CLI in surfaces_test.go.

func TestFindExistingLiveShareIn(t *testing.T) {
	t.Parallel()

	target := "/tmp/docs/test.md"
	shares := []share.ShareResponse{
		{
			ID:   "snapshot",
			Path: target,
			Mode: share.ModeSnapshot,
		},
		{
			ID:   "other",
			Path: "/tmp/other.md",
			Mode: share.ModeLive,
		},
		{
			ID:   "live",
			Path: target,
			Mode: share.ModeLive,
		},
	}

	got, ok := findExistingLiveShareIn(shares, target)
	if !ok {
		t.Fatal("expected existing live share match")
	}
	if got.ID != "live" {
		t.Fatalf("expected live share id, got %q", got.ID)
	}
}

func TestResolvePublishPlanUsesDirectoryBundleForMarkdownWithLocalAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docPath := filepath.Join(root, "plan.md")
	if err := os.WriteFile(docPath, []byte(`# Hello

![Diagram](./img/diagram.png)
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	plan, err := resolvePublishPlan(docPath)
	if err != nil {
		t.Fatalf("resolvePublishPlan: %v", err)
	}
	if plan.SharePath != root {
		t.Fatalf("expected share root %q, got %q", root, plan.SharePath)
	}
	if plan.EntryRel != "plan.md" {
		t.Fatalf("expected entry rel plan.md, got %q", plan.EntryRel)
	}
}

func TestResolvePublishPlanUsesDirectoryBundleForMarkdownTemplateWithLocalAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docPath := filepath.Join(root, "plan.md.tmpl")
	if err := os.WriteFile(docPath, []byte(`# Hello

![Diagram](./img/diagram.png)
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	plan, err := resolvePublishPlan(docPath)
	if err != nil {
		t.Fatalf("resolvePublishPlan: %v", err)
	}
	if plan.SharePath != root {
		t.Fatalf("expected share root %q, got %q", root, plan.SharePath)
	}
	if plan.EntryRel != "plan.md.tmpl" {
		t.Fatalf("expected entry rel plan.md.tmpl, got %q", plan.EntryRel)
	}
}

func TestResolvePublishPlanRejectsEscapingMarkdownAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docPath := filepath.Join(root, "plan.md")
	if err := os.WriteFile(docPath, []byte(`# Hello

![Diagram](../img/diagram.png)
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := resolvePublishPlan(docPath)
	if err == nil {
		t.Fatal("expected escaping markdown target to be rejected")
	}
}

func TestResolvePublishURLAppendsEntryPath(t *testing.T) {
	t.Parallel()

	got, err := resolvePublishURL("https://share.example.com/s/share123/?t=token123", "docs/readme.md")
	if err != nil {
		t.Fatalf("resolvePublishURL: %v", err)
	}

	want := "https://share.example.com/s/share123/docs/readme.md?t=token123"
	if got != want {
		t.Fatalf("resolvePublishURL() = %q, want %q", got, want)
	}
}

func TestFormatShareTextUsesConsistentFieldOrder(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.April, 1, 18, 4, 0, 0, time.UTC)
	expiresAt := createdAt.Add(24 * time.Hour)
	shareResp := share.ShareResponse{
		ID:        "abc123",
		IsDir:     true,
		Mode:      share.ModeLive,
		Path:      "/tmp/bundle",
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		URL:       "https://example.com/s/abc123/readme.md",
	}

	got := formatShareText(shareResp, shareTextOptions{
		Path:       "/tmp/bundle/readme.md",
		BundleRoot: "/tmp/bundle",
	})

	want := "id: abc123\n" +
		"kind: directory\n" +
		"mode: live\n" +
		"path: /tmp/bundle/readme.md\n" +
		"bundle_root: /tmp/bundle\n" +
		"created: " + createdAt.Local().Format(time.RFC3339) + "\n" +
		"expires: " + expiresAt.Local().Format(time.RFC3339) + "\n" +
		"url: https://example.com/s/abc123/readme.md"
	if got != want {
		t.Fatalf("formatShareText() = %q, want %q", got, want)
	}
}

func TestFormatShareListTextSeparatesEntriesWithBlankLine(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.April, 1, 18, 4, 0, 0, time.UTC)
	shares := []share.ShareResponse{
		{
			ID:        "one",
			IsDir:     false,
			Mode:      share.ModeLive,
			Path:      "/tmp/one.txt",
			CreatedAt: base,
			ExpiresAt: base.Add(2 * time.Hour),
			URL:       "https://example.com/s/one",
		},
		{
			ID:        "two",
			IsDir:     true,
			Mode:      share.ModeSnapshot,
			Path:      "/tmp/two",
			CreatedAt: base.Add(time.Hour),
			ExpiresAt: base.Add(3 * time.Hour),
			URL:       "https://example.com/s/two",
		},
	}

	got := formatShareListText(shares)
	want := formatShareText(shares[0], shareTextOptions{}) + "\n\n" + formatShareText(shares[1], shareTextOptions{})
	if got != want {
		t.Fatalf("formatShareListText() = %q, want %q", got, want)
	}
}
