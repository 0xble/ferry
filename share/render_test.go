package share

import (
	"strings"
	"testing"
)

func TestRenderHTMLPreviewPageClampsFrameToVisibleMobileWidth(t *testing.T) {
	t.Parallel()

	html := RenderHTMLPreviewPage("artifact.html", "/r/share123/artifact.html?t=token123", nil)
	for _, want := range []string{
		`grid-template-columns:minmax(0,1fr)`,
		`width:var(--ferry-usable-width,100%)`,
		`.artifact-shell .box-header,.artifact-frame{min-width:0}`,
		`const isIOSWebKit = navigator.maxTouchPoints > 0 && CSS.supports("-webkit-touch-callout","none")`,
		`if (!isIOSWebKit) return`,
		`if (!viewport || viewport.scale > 1.01) return`,
		`const safeWidth = Math.floor((Number(viewport.width) || 0) * Math.min(Number(viewport.scale) || 1, 1))`,
		`layoutWidth>safeWidth+8`,
		`root.style.setProperty("--ferry-usable-width",safeWidth+"px")`,
		`addEventListener("resize",clampVisibleWidth,{passive:true})`,
		`viewport.addEventListener("resize",clampVisibleWidth,{passive:true})`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected mobile viewport clamp %q in HTML preview shell, got %q", want, html)
		}
	}
	viewportIndex := strings.Index(html, `const viewport = window.visualViewport`)
	clampIndex := strings.Index(html, `const clampVisibleWidth = () =>`)
	if viewportIndex < 0 || viewportIndex > clampIndex {
		t.Fatalf("expected visual viewport reference to be declared outside the clamp callback, got %q", html)
	}
}

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

func TestRenderPreviewPageProvidesZoomableImageViewer(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("diagram.svg", PreviewImage, "/r/share123/diagram.svg?t=token123", nil)

	for _, want := range []string{
		`class="image-viewport"`,
		`id="image-preview"`,
		`data-vector="true"`,
		`class="action image-zoom-out"`,
		`class="action image-zoom-fit"`,
		`class="action image-zoom-in"`,
		`touch-action:pan-x pan-y pinch-zoom`,
		`const scales = [1, 1.5, 2, 3, 4]`,
		`image.dataset.vector === "true" ? availableWidth : Math.min(naturalWidth, availableWidth)`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected zoomable image viewer markup %q, got %q", want, html)
		}
	}
	if strings.Contains(html, `title="Raw"`) {
		t.Fatalf("expected image viewer not to link to the mobile-hostile raw image view, got %q", html)
	}
	if !strings.Contains(html, `href="/r/share123/diagram.svg?t=token123" download title="Download"`) {
		t.Fatalf("expected image viewer to retain the original-file download, got %q", html)
	}
	if strings.Contains(html, `max-height:80vh`) {
		t.Fatalf("expected image previews not to be capped to viewport height, got %q", html)
	}
}

func TestRenderPreviewPagePreservesSmallRasterImageFit(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("icon.png", PreviewImage, "/r/share123/icon.png?t=token123", nil)

	if !strings.Contains(html, `data-vector="false"`) {
		t.Fatalf("expected raster image to use intrinsic-width-aware fit behavior, got %q", html)
	}
}

func TestRenderPreviewPageKeepsVideoWithinViewport(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("demo.mp4", PreviewVideo, "/r/share123/demo.mp4?t=token123", nil)

	if !strings.Contains(html, `.media video{max-width:100%;max-height:80vh}`) {
		t.Fatalf("expected video previews to retain the viewport-height cap, got %q", html)
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
	if strings.Contains(html, `href="/r/share123/photo.png?t=token123" title="Raw"`) {
		t.Fatalf("expected image file row to omit the mobile-hostile raw view, got %q", html)
	}
	if !strings.Contains(html, `href="/r/share123/photo.png?t=token123" download title="Download"`) {
		t.Fatalf("expected image file row to retain the original-file download, got %q", html)
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

func TestClassifyPreviewKindSupportsTemplateSuffixes(t *testing.T) {
	t.Parallel()

	cases := map[string]PreviewKind{
		"SKILL.md.tmpl":       PreviewMarkdown,
		"README.markdown.tpl": PreviewMarkdown,
		"config.yaml.tmpl":    PreviewCode,
		"settings.json.j2":    PreviewCode,
		"template.tmpl":       PreviewCode,
		"index.html.tmpl":     PreviewCode,
	}
	for name, want := range cases {
		if got := ClassifyPreviewKind(name); got != want {
			t.Fatalf("ClassifyPreviewKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCodeLanguageForNameUsesUnderlyingTemplateExtension(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"config.yaml.tmpl": "yaml",
		"settings.json.j2": "json",
		"install.sh.tmpl":  "shell",
		"template.tmpl":    "plaintext",
	}
	for name, want := range cases {
		if got := CodeLanguageForName(name); got != want {
			t.Fatalf("CodeLanguageForName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIsMarkdownPreviewNameSupportsTemplateSuffixes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"SKILL.md.tmpl", "README.markdown.tpl", "notes.mkd.jinja2"} {
		if !IsMarkdownPreviewName(name) {
			t.Fatalf("expected %q to be treated as markdown", name)
		}
	}
	if IsMarkdownPreviewName("config.yaml.tmpl") {
		t.Fatal("expected yaml template not to be treated as markdown")
	}
}

func TestRenderPreviewPageCodeTemplateShowsCopyAction(t *testing.T) {
	t.Parallel()

	html := RenderPreviewPage("config.yaml.tmpl", PreviewCode, "/r/share123/config.yaml.tmpl?t=token123", nil)

	if strings.Contains(html, `This file type does not have an in-browser preview.`) {
		t.Fatalf("expected template file to render code preview, got %q", html)
	}
	if !strings.Contains(html, `class="language-yaml"`) {
		t.Fatalf("expected underlying yaml language for template preview, got %q", html)
	}
	if !strings.Contains(html, `class="action action-copy"`) {
		t.Fatalf("expected copy action in template preview, got %q", html)
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
