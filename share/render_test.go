package share

import (
	"strings"
	"testing"
)

func TestRenderPreviewPageCodeGuardHighlightsGracefully(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("notes.txt", PreviewCode, "/r/share123/notes.txt?t=token123", nil)

	if !strings.Contains(html, `window.hljs && typeof window.hljs.highlightElement === "function"`) {
		t.Fatalf("expected highlight.js guard in code preview, got %q", html)
	}
	if strings.Contains(html, `hljs.highlightElement(node)`) && !strings.Contains(html, `window.hljs.highlightElement(node)`) {
		t.Fatalf("expected guarded highlight invocation, got %q", html)
	}
}

func TestRenderPreviewPageShowsCopyActionForTextLikePreviews(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("notes.txt", PreviewCode, "/r/share123/notes.txt?t=token123", nil)

	if !strings.Contains(html, `class="action action-copy"`) {
		t.Fatalf("expected copy action in text preview, got %q", html)
	}
	if !strings.Contains(html, `data-copy-url="/r/share123/notes.txt?t=token123"`) {
		t.Fatalf("expected copy action to target raw file contents, got %q", html)
	}
	if !strings.Contains(html, `class="icon-check"`) {
		t.Fatalf("expected copy action to include check icon markup, got %q", html)
	}
	if !strings.Contains(html, `const ferryCopyResetTimers = new WeakMap()`) {
		t.Fatalf("expected clipboard helper script in preview page, got %q", html)
	}
	if !strings.Contains(html, `closest(".action-copy, .block-copy-button")`) {
		t.Fatalf("expected clipboard helper script to support block copy buttons, got %q", html)
	}
	if !strings.Contains(html, `new ClipboardItem({"text/plain": blobPromise})`) {
		t.Fatalf("expected mobile-safe ClipboardItem path for action copy, got %q", html)
	}
}

func TestRenderPreviewPageOmitsCopyActionForBinaryPreviews(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("photo.png", PreviewImage, "/r/share123/photo.png?t=token123", nil)

	if strings.Contains(html, `class="action action-copy"`) {
		t.Fatalf("expected binary preview to omit copy action, got %q", html)
	}
}

func TestRenderDirectoryPageUsesCopyActionOnlyForCopyableFiles(t *testing.T) {
	t.Parallel()

	html, err := RenderDirectoryPage("docs", []DirEntry{
		{
			Name:       "notes.txt",
			PreviewURL: "/s/share123/notes.txt?t=token123",
			RawURL:     "/r/share123/notes.txt?t=token123",
			CanCopy:    true,
		},
		{
			Name:       "photo.png",
			PreviewURL: "/s/share123/photo.png?t=token123",
			RawURL:     "/r/share123/photo.png?t=token123",
			CanCopy:    false,
		},
	}, nil)
	if err != nil {
		t.Fatalf("RenderDirectoryPage: %v", err)
	}

	if count := strings.Count(html, `class="action action-copy"`); count != 1 {
		t.Fatalf("expected exactly one copy action in directory listing, got %d in %q", count, html)
	}
	if !strings.Contains(html, `data-copy-url="/r/share123/notes.txt?t=token123"`) {
		t.Fatalf("expected text file row to expose copy action, got %q", html)
	}
	if strings.Contains(html, `data-copy-url="/r/share123/photo.png?t=token123"`) {
		t.Fatalf("expected binary file row to omit copy action, got %q", html)
	}
}

func TestClassifyPreviewKindTreatsDiffsAsStructuredDiffs(t *testing.T) {
	t.Parallel()

	cases := map[string]PreviewKind{
		"changes.diff":  PreviewDiff,
		"changes.patch": PreviewDiff,
		"notes.txt":     PreviewCode,
	}
	for name, want := range cases {
		if got := ClassifyPreviewKind(name); got != want {
			t.Fatalf("ClassifyPreviewKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRenderPreviewPageDiffUsesDiff2Html(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("changes.diff", PreviewDiff, "/r/share123/changes.diff?t=token123", nil)

	for _, needle := range []string{
		`github.min.css`,
		`github-dark.min.css`,
		`diff2html.min.css`,
		`diff2html-ui-slim.min.js`,
		`Loading diff preview...`,
		`new window.Diff2HtmlUI`,
		`outputFormat:"line-by-line"`,
		`drawFileList:true`,
		`fileListStartVisible:false`,
		`colorScheme:"auto"`,
		`highlight:true`,
		`ui.highlightCode()`,
		`fetch("/r/share123/changes.diff?t\u003Dtoken123")`,
		`#diff .d2h-code-linenumber,`,
		`position:static;`,
		`display:table-cell`,
		`text-overflow:clip;`,
		`#diff .d2h-code-linenumber{`,
		`min-width:7.5em;`,
		`#diff .d2h-code-side-linenumber{`,
		`min-width:4em;`,
		`#diff .d2h-code-line,`,
		`min-width:100%`,
		`@media (max-width: 720px)`,
	} {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected diff preview html to contain %q, got %q", needle, html)
		}
	}
}
