package share

import (
	"fmt"
	"html/template"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// previewThemeCSS defines CSS custom properties matching GitHub Primer design
// tokens for both light and dark color schemes.
const previewThemeCSS = `:root{color-scheme:light dark;` +
	`--preview-bg:#f6f8fa;--preview-canvas:#fff;--preview-inset:#f6f8fa;` +
	`--preview-elevated:rgba(255,255,255,.97);` +
	`--preview-border:#d0d7de;--preview-border-muted:#d8dee4;` +
	`--preview-text:#1f2328;--preview-text-muted:#656d76;` +
	`--preview-link:#0969da;--preview-code-bg:rgba(175,184,193,.2);` +
	`--preview-shadow:0 1px 0 rgba(31,35,40,.04);` +
	`--preview-inline-shadow:inset 0 0 0 1px rgba(208,215,222,.4)}` +
	`@media(prefers-color-scheme:dark){:root{` +
	`--preview-bg:#0d1117;--preview-canvas:#0d1117;--preview-inset:#161b22;` +
	`--preview-elevated:rgba(22,27,34,.97);` +
	`--preview-border:#30363d;--preview-border-muted:#21262d;` +
	`--preview-text:#e6edf3;--preview-text-muted:#8b949e;` +
	`--preview-link:#58a6ff;--preview-code-bg:rgba(110,118,129,.4);` +
	`--preview-shadow:0 1px 0 rgba(0,0,0,.3);` +
	`--preview-inline-shadow:inset 0 0 0 1px rgba(110,118,129,.2)}}` +
	`*{box-sizing:border-box}`

// previewBaseCSS defines shared layout styles reused across all preview types:
// body reset, links, container, box card, header, and action buttons.
const previewBaseCSS = `body{margin:0;background:var(--preview-bg);color:var(--preview-text);` +
	`font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif;` +
	`font-size:14px;line-height:1.5}` +
	`a{color:var(--preview-link);text-decoration:none}a:hover{text-decoration:underline}` +
	`.container{max-width:1012px;margin:0 auto;padding:24px 16px}` +
	`.box{border:1px solid var(--preview-border);border-radius:6px;overflow:hidden;background:var(--preview-canvas)}` +
	`.box-header{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px 16px;` +
	`background:var(--preview-inset);border-bottom:1px solid var(--preview-border);min-height:40px}` +
	`.filename{font-weight:600;font-family:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace;` +
	`font-size:14px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}` +
	`.actions{display:flex;gap:6px;flex-shrink:0}` +
	`.action{display:inline-flex;align-items:center;justify-content:center;width:2rem;height:2rem;` +
	`border:1px solid var(--preview-border);border-radius:6px;background:var(--preview-inset);` +
	`color:var(--preview-text);text-decoration:none}` +
	`.action:disabled{cursor:default;opacity:.8}` +
	`.action:hover{background:var(--preview-border-muted);text-decoration:none}` +
	`.action.is-error{color:#d1242f}` +
	`.action .icon-check{display:none}` +
	`.action.is-success .icon-copy{display:none}` +
	`.action.is-success .icon-check{display:block}` +
	`.action svg{width:1rem;height:1rem;stroke:currentColor;fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}` +
	`.breadcrumb{display:flex;align-items:center;gap:4px;min-width:0;font-size:14px}` +
	`.breadcrumb a{color:var(--preview-link);font-weight:400}` +
	`.breadcrumb .sep{color:var(--preview-text-muted);flex-shrink:0}` +
	`.breadcrumb .current{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}`

// Breadcrumb represents a single segment in the navigation path.
type Breadcrumb struct {
	Name string
	URL  string
}

type PreviewKind string

const (
	PreviewCode     PreviewKind = "code"
	PreviewDiff     PreviewKind = "diff"
	PreviewMarkdown PreviewKind = "markdown"
	PreviewHTML     PreviewKind = "html"
	PreviewCSV      PreviewKind = "csv"
	PreviewPDF      PreviewKind = "pdf"
	PreviewImage    PreviewKind = "image"
	PreviewAudio    PreviewKind = "audio"
	PreviewVideo    PreviewKind = "video"
	PreviewBinary   PreviewKind = "binary"
)

type DirEntry struct {
	Name       string
	IsDir      bool
	Size       int64
	ModTime    time.Time
	PreviewURL string
	RawURL     string
	CanCopy    bool
}

func canCopyContents(kind PreviewKind) bool {
	switch kind {
	case PreviewCode, PreviewDiff, PreviewMarkdown, PreviewHTML, PreviewCSV:
		return true
	default:
		return false
	}
}

var markdownPreviewExtensions = []string{".markdown", ".mdown", ".mkdn", ".mkd", ".md"}

var previewTemplateSuffixes = []string{
	".handlebars",
	".mustache",
	".jinja2",
	".jinja",
	".gotmpl",
	".ctmpl",
	".tmpl",
	".hbs",
	".erb",
	".tpl",
	".j2",
}

func previewNameForExtension(name string) (string, bool) {
	stripped := false
	for {
		lowerName := strings.ToLower(name)
		matched := false
		for _, suffix := range previewTemplateSuffixes {
			if strings.HasSuffix(lowerName, suffix) && len(name) > len(suffix) {
				name = name[:len(name)-len(suffix)]
				stripped = true
				matched = true
				break
			}
		}
		if !matched {
			return name, stripped
		}
	}
}

// IsMarkdownPreviewName reports whether a filename should use the rendered
// Markdown preview, including common template suffixes such as .md.tmpl.
func IsMarkdownPreviewName(name string) bool {
	previewName, _ := previewNameForExtension(name)
	return isMarkdownPreviewExtension(filepath.Ext(previewName))
}

func isMarkdownPreviewExtension(ext string) bool {
	ext = strings.ToLower(ext)
	for _, markdownExt := range markdownPreviewExtensions {
		if ext == markdownExt {
			return true
		}
	}
	return false
}

func ClassifyPreviewKind(name string) PreviewKind {
	previewName, isTemplate := previewNameForExtension(name)
	ext := strings.ToLower(filepath.Ext(previewName))
	if isTemplate {
		if isMarkdownPreviewExtension(ext) {
			return PreviewMarkdown
		}
		return PreviewCode
	}

	switch ext {
	case ".diff", ".patch":
		return PreviewDiff
	}
	if isMarkdownPreviewExtension(ext) {
		return PreviewMarkdown
	}
	switch ext {
	case ".html", ".htm":
		return PreviewHTML
	case ".csv", ".tsv":
		return PreviewCSV
	case ".pdf":
		return PreviewPDF
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".tif", ".tiff", ".avif", ".ico":
		return PreviewImage
	case ".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac":
		return PreviewAudio
	case ".mp4", ".webm", ".mov", ".m4v", ".avi", ".mkv":
		return PreviewVideo
	}
	if isCodeLikeExtension(ext) || ext == "" {
		return PreviewCode
	}
	return PreviewBinary
}

func CodeLanguageForName(name string) string {
	previewName, _ := previewNameForExtension(name)
	ext := strings.ToLower(filepath.Ext(previewName))
	if isMarkdownPreviewExtension(ext) {
		return "markdown"
	}
	switch ext {
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".json":
		return "json"
	case ".css", ".scss", ".less":
		return "css"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rb":
		return "ruby"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".php":
		return "php"
	case ".xml":
		return "xml"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".ini", ".env":
		return "ini"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".sql":
		return "sql"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	default:
		return "plaintext"
	}
}

func isCodeLikeExtension(ext string) bool {
	switch ext {
	case ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".ini", ".xml",
		".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
		".go", ".py", ".rb", ".java", ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp",
		".rs", ".php", ".swift", ".kt", ".kts", ".cs", ".scala",
		".sh", ".bash", ".zsh", ".fish", ".sql", ".css", ".scss", ".less":
		return true
	default:
		return false
	}
}

const (
	iconRaw      = `<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>`
	iconDownload = `<path d="M12 3v11"/><path d="m7 11 5 5 5-5"/><path d="M5 21h14"/>`
	iconCopy     = `<rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>`
	iconCheck    = `<path d="M20 6 9 17l-5-5"/>`
	iconZoomOut  = `<line x1="5" y1="12" x2="19" y2="12"/>`
	iconZoomIn   = `<line x1="5" y1="12" x2="19" y2="12"/><line x1="12" y1="5" x2="12" y2="19"/>`
	iconFitWidth = `<polyline points="8 3 3 3 3 8"/><polyline points="16 3 21 3 21 8"/><polyline points="8 21 3 21 3 16"/><polyline points="16 21 21 21 21 16"/>`
)

const previewActionScriptTag = `<script>
const ferryCopyResetTimers = new WeakMap()

async function ferryWriteTextToClipboard(text) {
	if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
		await navigator.clipboard.writeText(text)
		return
	}

	const textarea = document.createElement("textarea")
	textarea.value = text
	textarea.setAttribute("readonly", "")
	textarea.style.position = "fixed"
	textarea.style.top = "-9999px"
	textarea.style.left = "-9999px"
	document.body.appendChild(textarea)
	textarea.select()
	textarea.setSelectionRange(0, textarea.value.length)
	const copied = document.execCommand("copy")
	document.body.removeChild(textarea)
	if (!copied) {
		throw new Error("Clipboard copy failed")
	}
}

function ferrySetCopyButtonState(button, state) {
	const idleLabel = button.dataset.copyLabel || "Copy contents"
	const labels = {
		idle: idleLabel,
		loading: "Copying...",
		success: "Copied",
		error: "Copy failed",
	}
	const label = labels[state] || labels.idle

	button.classList.remove("is-success", "is-error")
	if (state === "success") {
		button.classList.add("is-success")
	}
	if (state === "error") {
		button.classList.add("is-error")
	}
	button.setAttribute("aria-label", label)
	button.setAttribute("title", label)

	const previousTimer = ferryCopyResetTimers.get(button)
	if (previousTimer) {
		clearTimeout(previousTimer)
		ferryCopyResetTimers.delete(button)
	}
	if (state === "success" || state === "error") {
		const timer = window.setTimeout(() => {
			button.classList.remove("is-success", "is-error")
			button.setAttribute("aria-label", labels.idle)
			button.setAttribute("title", labels.idle)
			ferryCopyResetTimers.delete(button)
		}, 2000)
		ferryCopyResetTimers.set(button, timer)
	}
}

async function ferryReadActionCopyText(button) {
	const response = await fetch(button.dataset.copyUrl)
	if (!response.ok) {
		throw new Error("HTTP " + response.status)
	}
	return response.text()
}

async function ferryWriteActionCopyToClipboard(button) {
	const url = button.dataset.copyUrl
	if (typeof ClipboardItem !== "undefined"
		&& navigator.clipboard
		&& typeof navigator.clipboard.write === "function") {
		const blobPromise = fetch(url).then(response => {
			if (!response.ok) {
				throw new Error("HTTP " + response.status)
			}
			return response.blob().then(blob => blob.slice(0, blob.size, "text/plain"))
		})
		await navigator.clipboard.write([new ClipboardItem({"text/plain": blobPromise})])
		return
	}

	const text = await ferryReadActionCopyText(button)
	await ferryWriteTextToClipboard(text)
}

function ferryNormalizeClipboardLineEndings(text) {
	return String(text).replace(/\r\n/g, "\n")
}

function ferryReadBlockCopyText(button) {
	const target = button.closest(".copyable-block")
	if (!target) {
		throw new Error("Copy target not found")
	}

	if (button.dataset.copyKind === "code") {
		const code = target.querySelector("code")
		return ferryNormalizeClipboardLineEndings(code ? code.textContent || "" : "")
	}

	const visibleText = target.innerText || target.textContent || ""
	return ferryNormalizeClipboardLineEndings(visibleText).trim()
}

document.addEventListener("click", async event => {
	const button = event.target.closest(".action-copy, .block-copy-button")
	if (!button) {
		return
	}
	event.preventDefault()
	if (button.disabled) {
		return
	}

	button.disabled = true
	ferrySetCopyButtonState(button, "loading")

	try {
		if (button.classList.contains("action-copy")) {
			await ferryWriteActionCopyToClipboard(button)
		} else {
			await ferryWriteTextToClipboard(ferryReadBlockCopyText(button))
		}
		ferrySetCopyButtonState(button, "success")
	} catch (error) {
		console.error("Failed to copy Ferry contents", error)
		ferrySetCopyButtonState(button, "error")
	} finally {
		button.disabled = false
	}
})
</script>`

func renderActionsHTML(rawURL string, canCopy bool, showRaw bool) string {
	u := template.HTMLEscapeString(rawURL)
	var b strings.Builder
	b.WriteString(`<div class="actions">`)
	if canCopy {
		b.WriteString(`<button class="action action-copy" type="button" data-copy-url="`)
		b.WriteString(u)
		b.WriteString(`" data-copy-label="Copy contents" title="Copy contents" aria-label="Copy contents">`)
		b.WriteString(renderCopyButtonIconsHTML())
		b.WriteString(`</button>`)
	}
	if showRaw {
		b.WriteString(`<a class="action" href="`)
		b.WriteString(u)
		b.WriteString(`" title="Raw" aria-label="Raw"><svg viewBox="0 0 24 24">`)
		b.WriteString(iconRaw)
		b.WriteString(`</svg></a>`)
	}
	b.WriteString(`<a class="action" href="`)
	b.WriteString(u)
	b.WriteString(`" download title="Download" aria-label="Download"><svg viewBox="0 0 24 24">`)
	b.WriteString(iconDownload)
	b.WriteString(`</svg></a></div>`)
	return b.String()
}

func renderImageActionsHTML(rawURL string) string {
	u := template.HTMLEscapeString(rawURL)
	return `<div class="actions">` +
		`<button class="action image-zoom-out" type="button" title="Zoom out" aria-label="Zoom out" disabled><svg viewBox="0 0 24 24">` + iconZoomOut + `</svg></button>` +
		`<button class="action image-zoom-fit" type="button" title="Fit width" aria-label="Fit width"><svg viewBox="0 0 24 24">` + iconFitWidth + `</svg></button>` +
		`<button class="action image-zoom-in" type="button" title="Zoom in" aria-label="Zoom in"><svg viewBox="0 0 24 24">` + iconZoomIn + `</svg></button>` +
		`<a class="action" href="` + u + `" download title="Download" aria-label="Download"><svg viewBox="0 0 24 24">` + iconDownload + `</svg></a>` +
		`</div>`
}

const imageViewerScriptTag = `<script>
(() => {
 const viewport = document.querySelector(".image-viewport")
 const image = document.getElementById("image-preview")
 const zoomOut = document.querySelector(".image-zoom-out")
 const zoomFit = document.querySelector(".image-zoom-fit")
 const zoomIn = document.querySelector(".image-zoom-in")
 if (!(viewport && image && zoomOut && zoomFit && zoomIn)) return

 const scales = [1, 1.5, 2, 3, 4]
 let scaleIndex = 0
 let fitWidth = 0
 let resizeFrame = 0

 const measureFitWidth = () => {
  const styles = getComputedStyle(viewport)
  const horizontalPadding = (Number.parseFloat(styles.paddingLeft) || 0) + (Number.parseFloat(styles.paddingRight) || 0)
  const availableWidth = Math.max(1, viewport.clientWidth - horizontalPadding)
  const naturalWidth = Number(image.naturalWidth) || availableWidth
  fitWidth = image.dataset.vector === "true" ? availableWidth : Math.min(naturalWidth, availableWidth)
 }

 const updateControls = () => {
  const percent = Math.round(scales[scaleIndex] * 100)
  zoomOut.disabled = scaleIndex === 0
  zoomIn.disabled = scaleIndex === scales.length - 1
  zoomFit.disabled = scaleIndex === 0
  zoomFit.title = "Fit width (" + percent + "% zoom)"
  zoomFit.setAttribute("aria-label", "Fit width, currently " + percent + "% zoom")
 }

 const render = () => {
  image.style.width = Math.max(1, Math.round(fitWidth * scales[scaleIndex])) + "px"
  image.style.maxWidth = "none"
  updateControls()
 }

 const setScaleIndex = nextIndex => {
  const boundedIndex = Math.max(0, Math.min(scales.length - 1, nextIndex))
  if (boundedIndex === scaleIndex) return
  const centerRatio = (viewport.scrollLeft + viewport.clientWidth / 2) / Math.max(viewport.scrollWidth, 1)
  scaleIndex = boundedIndex
  render()
  requestAnimationFrame(() => {
   viewport.scrollLeft = centerRatio * viewport.scrollWidth - viewport.clientWidth / 2
  })
 }

 const fit = () => {
  scaleIndex = 0
  measureFitWidth()
  render()
  viewport.scrollLeft = 0
 }

 zoomOut.addEventListener("click", () => setScaleIndex(scaleIndex - 1))
 zoomFit.addEventListener("click", fit)
 zoomIn.addEventListener("click", () => setScaleIndex(scaleIndex + 1))

 const initialize = () => {
  measureFitWidth()
  render()
 }
 if (image.complete) initialize()
 else image.addEventListener("load", initialize, {once:true})

 addEventListener("resize", () => {
  cancelAnimationFrame(resizeFrame)
  resizeFrame = requestAnimationFrame(() => {
   measureFitWidth()
   render()
  })
 }, {passive:true})
})()
</script>`

func renderCopyButtonIconsHTML() string {
	return `<svg class="icon-copy" viewBox="0 0 24 24">` + iconCopy +
		`</svg><svg class="icon-check" viewBox="0 0 24 24">` + iconCheck + `</svg>`
}

func renderBreadcrumbHTML(crumbs []Breadcrumb, current string) string {
	if len(crumbs) == 0 && current == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<nav class="breadcrumb">`)
	for _, c := range crumbs {
		fmt.Fprintf(&b, `<a href="%s">%s</a><span class="sep">/</span>`,
			template.HTMLEscapeString(c.URL), template.HTMLEscapeString(c.Name))
	}
	if current != "" {
		fmt.Fprintf(&b, `<span class="current">%s</span>`, template.HTMLEscapeString(current))
	}
	b.WriteString(`</nav>`)
	return b.String()
}

func RenderHTMLPreviewPage(baseName string, rawURL string, breadcrumbs []Breadcrumb) string {
	title := template.HTMLEscapeString(baseName)
	nav := renderBreadcrumbHTML(breadcrumbs, baseName)
	if nav == "" {
		nav = `<span class="filename">` + title + `</span>`
	}
	actions := renderActionsHTML(rawURL, true, true)
	return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<style>`+previewThemeCSS+previewBaseCSS+`
html,body{width:100%%;max-width:100%%;height:100%%;overflow:hidden;overflow-x:hidden;overscroll-behavior-x:none;touch-action:pan-y pinch-zoom}
body{display:flex}
.artifact-shell{display:grid;grid-template-columns:minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);width:var(--ferry-usable-width,100%%);max-width:var(--ferry-usable-width,100%%);height:100%%;overflow:hidden;background:var(--preview-canvas)}
.artifact-shell .box-header,.artifact-frame{min-width:0}
.artifact-shell .box-header{border:0;border-bottom:1px solid var(--preview-border);border-radius:0}
.artifact-frame{display:block;width:100%%;max-width:100%%;height:100%%;border:0;background:#fff;overscroll-behavior-x:none;touch-action:pan-y pinch-zoom}
</style></head><body>
<main class="artifact-shell">
<header class="box-header">%s%s</header>
<iframe class="artifact-frame" sandbox="allow-scripts" referrerpolicy="no-referrer" title="%s"></iframe>
</main>
<script>
(() => {
 const root = document.documentElement
 const viewport = window.visualViewport
 const isIOSWebKit = navigator.maxTouchPoints > 0 && CSS.supports("-webkit-touch-callout","none")
 const clampVisibleWidth = () => {
  if (!isIOSWebKit) return
  // Screen dimensions describe the device, not split-view or rotated windows.
  // Keep the last layout width while pinching so zoom does not reflow content.
  if (!viewport || viewport.scale > 1.01) return
  // Safari may initially shrink overflowing content below scale 1.
  const safeWidth = Math.floor((Number(viewport.width) || 0) * Math.min(Number(viewport.scale) || 1, 1))
  const layoutWidth = root.clientWidth
  if (safeWidth >= 280 && layoutWidth>safeWidth+8) {
   root.style.setProperty("--ferry-usable-width",safeWidth+"px")
  } else {
   root.style.removeProperty("--ferry-usable-width")
  }
 }
 clampVisibleWidth()
 addEventListener("resize",clampVisibleWidth,{passive:true})
 if (viewport) viewport.addEventListener("resize",clampVisibleWidth,{passive:true})
 const frame = document.querySelector(".artifact-frame")
 const artifactURL = new URL(window.location.href)
 const marker = "/s/"
 const markerIndex = artifactURL.pathname.lastIndexOf(marker)
 if (markerIndex < 0) return
 artifactURL.pathname = artifactURL.pathname.slice(0, markerIndex) + "/h/" + artifactURL.pathname.slice(markerIndex + marker.length)
 artifactURL.searchParams.delete("pv")
 artifactURL.hash = ""
 frame.src = artifactURL.pathname + artifactURL.search
})()
</script>%s</body></html>`, title, nav, actions, title, previewActionScriptTag)
}

func RenderPreviewPage(baseName string, kind PreviewKind, rawURL string, breadcrumbs []Breadcrumb) string {
	title := template.HTMLEscapeString(baseName)
	raw := template.JSEscapeString(rawURL)
	nav := renderBreadcrumbHTML(breadcrumbs, baseName)
	if nav == "" {
		nav = `<span class="filename">` + title + `</span>`
	}
	actions := renderActionsHTML(rawURL, canCopyContents(kind), true)
	actionScript := previewActionScriptTag

	switch kind {
	case PreviewDiff:
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.8.0/styles/github.min.css" integrity="sha384-eFTL69TLRZTkNfYZOLM+G04821K1qZao/4QLJbet1pP4tcF+fdXq/9CdqAbWRl/L" crossorigin="anonymous" referrerpolicy="no-referrer" media="screen and (prefers-color-scheme: light)" />
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.8.0/styles/github-dark.min.css" integrity="sha384-wH75j6z1lH97ZOpMOInqhgKzFkAInZPPSPlZpYKYTOqsaizPvhQZmAtLcPKXpLyH" crossorigin="anonymous" referrerpolicy="no-referrer" media="screen and (prefers-color-scheme: dark)" />
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/diff2html@3.4.56/bundles/css/diff2html.min.css" integrity="sha384-PdRCG/+r1waybtXfuDB9Kmv2h7AGoN6WaTZ5OF+ctPeR9BGne0FaKRQnUeGV4nDL" crossorigin="anonymous" referrerpolicy="no-referrer" />
<style>`+previewThemeCSS+previewBaseCSS+`
.container{max-width:1400px}
#diff{padding:16px;overflow-x:auto;overflow-y:hidden}
#diff .d2h-wrapper{margin:0}
#diff .d2h-file-header{position:sticky;top:0;z-index:1}
#diff .d2h-file-list-wrapper,
#diff .d2h-file-wrapper{margin-bottom:16px}
#diff .d2h-file-wrapper,
#diff .d2h-file-list-wrapper{overflow-x:auto}
#diff .d2h-diff-table{min-width:100%%}
#diff .d2h-code-linenumber,
#diff .d2h-code-side-linenumber{
 position:static;
 display:table-cell;
 overflow:visible;
 text-overflow:clip;
 white-space:nowrap
}
#diff .d2h-code-linenumber{
 width:7.5em;
 min-width:7.5em;
 max-width:7.5em
}
#diff .d2h-code-side-linenumber{
 width:4em;
 min-width:4em;
 max-width:4em
}
#diff .line-num1,
#diff .line-num2{
 overflow:visible;
 text-overflow:clip
}
#diff .d2h-code-line,
#diff .d2h-code-side-line{
 padding:0;
 width:auto;
 min-width:100%%
}
#diff .d2h-code-linenumber,
#diff .d2h-code-side-linenumber{
 box-shadow:none
}
#diff .d2h-code-line-ctn{white-space:pre}
#diff .d2h-tag{border-radius:999px}
.message{padding:48px 24px;text-align:center;color:var(--preview-text-muted)}
@media (max-width: 720px){
 #diff{padding:12px}
 #diff .d2h-file-header{height:auto;min-height:35px;padding:6px 8px}
 #diff .d2h-file-name-wrapper{font-size:13px}
 #diff .d2h-diff-table{font-size:12px}
 #diff .d2h-code-linenumber{width:6em;min-width:6em;max-width:6em}
 #diff .d2h-code-side-linenumber{width:3em;min-width:3em;max-width:3em}
 #diff .line-num1,#diff .line-num2{width:2.75em}
}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">
%s
%s
</div>
<div id="diff" class="message">Loading diff preview...</div>
</div></div>
<script src="https://cdn.jsdelivr.net/npm/diff2html@3.4.56/bundles/js/diff2html-ui-slim.min.js" integrity="sha384-WV/85F8QlW9zrU6tCec7YGnPJWFExGkFiGU9Ir/5QHjLhu1jcvhe8XoVdWsCLZ7b" crossorigin="anonymous" referrerpolicy="no-referrer"></script>
<script>
fetch("%s").then(async response=>{
 if (!response.ok) {
  throw new Error("Failed to load diff: HTTP "+response.status)
 }
 return response.text()
}).then(text=>{
 const target=document.getElementById("diff")
 if (!(window.Diff2HtmlUI && typeof window.Diff2HtmlUI === "function")) {
  target.className="message"
  target.textContent="Diff viewer failed to load."
  return
 }
 target.className=""
 const ui=new window.Diff2HtmlUI(target, text, {
  drawFileList:true,
  fileListToggle:true,
  fileListStartVisible:false,
  fileContentToggle:true,
  matching:"lines",
  colorScheme:"auto",
  outputFormat:"line-by-line",
  highlight:true
 })
 ui.draw()
 if (typeof ui.highlightCode === "function") {
  ui.highlightCode()
 }
}).catch(err=>{
 const target=document.getElementById("diff")
 target.className="message"
 target.textContent=String(err)
})
</script>%s</body></html>`, title, nav, actions, raw, actionScript)
	case PreviewCode:
		language := template.JSEscapeString(CodeLanguageForName(baseName))
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.11.1/build/highlight.min.js" integrity="sha384-RH2xi4eIQ/gjtbs9fUXM68sLSi99C7ZWBRX1vDrVv6GQXRibxXLbwO2NGZB74MbU" crossorigin="anonymous" referrerpolicy="no-referrer"></script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.11.1/build/styles/github.min.css" integrity="sha384-eFTL69TLRZTkNfYZOLM+G04821K1qZao/4QLJbet1pP4tcF+fdXq/9CdqAbWRl/L" crossorigin="anonymous" referrerpolicy="no-referrer" media="(prefers-color-scheme: light)" />
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.11.1/build/styles/github-dark.min.css" integrity="sha384-wH75j6z1lH97ZOpMOInqhgKzFkAInZPPSPlZpYKYTOqsaizPvhQZmAtLcPKXpLyH" crossorigin="anonymous" referrerpolicy="no-referrer" media="(prefers-color-scheme: dark)" />
<style>`+previewThemeCSS+previewBaseCSS+`
pre{margin:0;padding:16px;overflow:auto}
code{font-size:13px;line-height:1.45;font-family:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">
%s
%s
</div>
<pre><code id="code" class="language-%s">Loading...</code></pre>
</div></div>
<script>
fetch("%s").then(r=>r.text()).then(text=>{
 const node=document.getElementById("code")
 node.textContent=text
 if (window.hljs && typeof window.hljs.highlightElement === "function") {
  window.hljs.highlightElement(node)
 }
}).catch(err=>{document.getElementById("code").textContent=String(err)})
</script>%s</body></html>`, title, nav, actions, language, raw, actionScript)
	case PreviewCSV:
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<link href="https://cdn.jsdelivr.net/npm/tabulator-tables@6.3.0/dist/css/tabulator.min.css" rel="stylesheet" integrity="sha384-p6hY5g6BjbrEVoCLwj4gZMkotMaHdQrPkd4S0vRpErDxL9/kvjyaUIq/S9RAPQfL" crossorigin="anonymous" referrerpolicy="no-referrer" media="(prefers-color-scheme: light)">
<link href="https://cdn.jsdelivr.net/npm/tabulator-tables@6.3.0/dist/css/tabulator_midnight.min.css" rel="stylesheet" integrity="sha384-vBqRsRIPXb03PYCog2z5jjcEBrrvg3yilshFbdVOmFtVX27t9rYeO7lXit0xIcou" crossorigin="anonymous" referrerpolicy="no-referrer" media="(prefers-color-scheme: dark)">
<style>`+previewThemeCSS+previewBaseCSS+`</style>
</head><body>
<div class="container" style="max-width:1280px">
<div class="box">
<div class="box-header">%s%s</div>
<div id="table" style="padding:4px"></div>
</div></div>
<script src="https://cdn.jsdelivr.net/npm/papaparse@5.5.3/papaparse.min.js" integrity="sha384-Jd2/X5FXVKahwaY2nivvw4LfSTg9idSj8yNXWtT4qef0fHSYr6M7M8bJAfbFYoMc" crossorigin="anonymous" referrerpolicy="no-referrer"></script>
<script src="https://cdn.jsdelivr.net/npm/tabulator-tables@6.3.0/dist/js/tabulator.min.js" integrity="sha384-gb+8dgIcSEcX2zwyEPEEKZiwD8km3OTz47IVU3CXdP/pYz4fRAdb9pJdlHW8VvM0" crossorigin="anonymous" referrerpolicy="no-referrer"></script>
<script>
fetch("%s").then(r=>r.text()).then(text=>{
 const parsed=Papa.parse(text,{header:true,skipEmptyLines:true})
 const rows=parsed.data||[]
 const fields=(parsed.meta&&parsed.meta.fields)||(rows.length?Object.keys(rows[0]):[])
 const columns=fields.map(f=>({title:f,field:f}))
 new Tabulator("#table",{data:rows,columns:columns,layout:"fitDataStretch",height:"calc(100vh - 140px)",movableColumns:true,pagination:rows.length>500,paginationSize:100})
})
</script>%s</body></html>`, title, nav, actions, raw, actionScript)
	case PreviewPDF:
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<link rel="modulepreload" href="https://cdn.jsdelivr.net/npm/pdfjs-dist@4.10.38/build/pdf.min.mjs" integrity="sha384-+0ti2moQlmLN7WZHE2RHIf5lV8hHxhxEalN0il3YZceG26fUPyOkR0hp9daxk1i7" crossorigin="anonymous" referrerpolicy="no-referrer" />
<style>`+previewThemeCSS+previewBaseCSS+`
#viewer{padding:16px;display:grid;gap:16px}
canvas{width:100%%;height:auto;background:#fff;border-radius:4px}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">
%s
%s
</div>
<div id="viewer"><p style="padding:16px;color:var(--preview-text-muted)">Loading PDF...</p></div>
</div></div>
<script type="module">
import * as pdfjsLib from "https://cdn.jsdelivr.net/npm/pdfjs-dist@4.10.38/build/pdf.min.mjs";
pdfjsLib.GlobalWorkerOptions.workerSrc = "https://cdn.jsdelivr.net/npm/pdfjs-dist@4.10.38/build/pdf.worker.min.mjs";
const viewer=document.getElementById("viewer");
try {
 const doc=await pdfjsLib.getDocument("%s").promise;
 viewer.innerHTML="";
 for (let pageNum=1; pageNum<=doc.numPages; pageNum++) {
  const page=await doc.getPage(pageNum);
  const viewport=page.getViewport({ scale: 1.3 });
  const canvas=document.createElement("canvas");
  const ctx=canvas.getContext("2d");
  canvas.width=viewport.width; canvas.height=viewport.height;
  viewer.appendChild(canvas);
 await page.render({ canvasContext: ctx, viewport }).promise;
 }
} catch(err) { viewer.innerHTML = "<pre>" + String(err) + "</pre>" }
</script>%s</body></html>`, title, nav, actions, raw, actionScript)
	case PreviewImage:
		rawHTML := template.HTMLEscapeString(rawURL)
		vector := strings.EqualFold(filepath.Ext(baseName), ".svg")
		imageActions := renderImageActionsHTML(rawURL)
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<style>`+previewThemeCSS+previewBaseCSS+`
.image-box{overflow:visible}
.image-viewport{overflow-x:auto;padding:24px;overscroll-behavior-x:contain;touch-action:pan-x pan-y pinch-zoom;-webkit-overflow-scrolling:touch}
.image-stage{width:max-content;min-width:100%%}
.image-stage img{display:block;width:auto;max-width:100%%;height:auto;margin:0 auto;border-radius:4px}
@media(max-width:600px){
 .container{max-width:none!important;padding:0}
 .image-box{border:0;border-radius:0}
 .image-box .box-header{position:sticky;top:0;z-index:2}
 .image-viewport{padding:0}
 .image-stage img{border-radius:0}
}
</style></head><body>
<div class="container" style="max-width:1280px">
<div class="box image-box">
<div class="box-header">
%s
%s
</div>
<div class="image-viewport" tabindex="0" aria-label="Image viewer"><div class="image-stage"><img id="image-preview" src="%s" alt="%s" data-vector="%t" draggable="false" /></div></div>
</div></div>%s</body></html>`, title, nav, imageActions, rawHTML, title, vector, imageViewerScriptTag)
	case PreviewAudio:
		rawHTML := template.HTMLEscapeString(rawURL)
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<style>`+previewThemeCSS+previewBaseCSS+`
.media{display:flex;align-items:center;justify-content:center;padding:32px}
.media audio{width:min(600px,90vw)}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">
%s
%s
</div>
<div class="media"><audio controls preload="metadata" src="%s"></audio></div>
</div></div>%s</body></html>`, title, nav, actions, rawHTML, actionScript)
	case PreviewVideo:
		rawHTML := template.HTMLEscapeString(rawURL)
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<style>`+previewThemeCSS+previewBaseCSS+`
.media{display:flex;align-items:center;justify-content:center;padding:16px}
.media video{max-width:100%%;max-height:80vh}
</style></head><body>
<div class="container" style="max-width:1280px">
<div class="box">
<div class="box-header">
%s
%s
</div>
<div class="media"><video controls preload="metadata" src="%s"></video></div>
</div></div>%s</body></html>`, title, nav, actions, rawHTML, actionScript)
	default:
		return fmt.Sprintf(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>%s</title>
<style>`+previewThemeCSS+previewBaseCSS+`
.message{padding:48px 24px;text-align:center;color:var(--preview-text-muted)}
.message p{margin:0 0 16px}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">%s%s</div>
<div class="message">
<p>This file type does not have an in-browser preview.</p>
</div></div></div>%s</body></html>`, title, nav, actions, actionScript)
	}
}

func RenderDirectoryPage(title string, entries []DirEntry, breadcrumbs []Breadcrumb) (string, error) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	tmpl := template.Must(template.New("dir").Funcs(template.FuncMap{
		"size": humanSize,
		"ts": func(t time.Time) string {
			return t.Local().Format("2006-01-02 15:04")
		},
		"actions": func(rawURL string, name string, canCopy bool) template.HTML {
			return template.HTML(renderActionsHTML(rawURL, canCopy, ClassifyPreviewKind(name) != PreviewImage))
		},
	}).Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>{{ .Title }}</title>
<style>` + previewThemeCSS + previewBaseCSS + `
.files{width:100%;border-collapse:collapse;border-spacing:0}
.files td{padding:6px 16px;border-top:1px solid var(--preview-border)}
.files tr:first-child td{border-top:none}
.files tr:hover td{background:var(--preview-inset)}
.files .name{font-family:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace}
.files .meta{color:var(--preview-text-muted);white-space:nowrap}
.files .size{text-align:right}
.files .row-actions{text-align:right}
.files .row-actions .actions{display:inline-flex;justify-content:flex-end}
.files .row-actions .action{width:1.6rem;height:1.6rem}
.files .row-actions .action svg{width:.8rem;height:.8rem}
</style></head><body>
<div class="container">
<div class="box">
<div class="box-header">
{{ .Nav }}
</div>
<table class="files">
{{ range .Entries }}
<tr>
<td class="name"><a href="{{ .PreviewURL }}">{{ .Name }}{{ if .IsDir }}/{{ end }}</a></td>
<td class="meta size">{{ if .IsDir }}-{{ else }}{{ size .Size }}{{ end }}</td>
<td class="meta">{{ ts .ModTime }}</td>
<td class="row-actions">{{ if not .IsDir }}{{ actions .RawURL .Name .CanCopy }}{{ end }}</td>
</tr>
{{ end }}
</table>
</div></div>` + previewActionScriptTag + `</body></html>`))

	nav := renderBreadcrumbHTML(breadcrumbs, title)
	if nav == "" {
		nav = `<strong>` + template.HTMLEscapeString(title) + `</strong>`
	}

	data := struct {
		Title   string
		Nav     template.HTML
		Entries []DirEntry
	}{
		Title:   title,
		Nav:     template.HTML(nav),
		Entries: entries,
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
