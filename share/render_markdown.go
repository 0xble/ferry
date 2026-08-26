package share

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	nethtml "golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

type markdownLinkResolver func(target string, isImage bool) string

type MarkdownDirectoryShareAnalysis struct {
	NeedsDirectoryShare bool
	HasEscapingTargets  bool
}

var (
	markdownCheckboxAttrPattern = regexp.MustCompile(`(?i)^(|checked|disabled)$`)
	markdownCheckboxTypePattern = regexp.MustCompile(`(?i)^checkbox$`)
	markdownMermaidClassPattern = regexp.MustCompile(`^language-mermaid$`)
	markdownFrontmatterFence    = []byte("---")
	markdownDirectiveTagPattern = regexp.MustCompile(`^</?([a-z][a-z0-9_:-]*)(?:\s+[^<>]*)?>$`)
	markdownAlertMarkerPattern  = regexp.MustCompile(`(?i)^\s*\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\][ \t]*(?:\r?\n)?[ \t]*`)
)

var markdownDirectiveTags = map[string]struct{}{
	"constraints":   {},
	"examples":      {},
	"guardrails":    {},
	"notify":        {},
	"on_error":      {},
	"output":        {},
	"qualification": {},
	"reply_logic":   {},
	"scope":         {},
	"style":         {},
}

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

func RenderMarkdownDocument(source []byte) (string, map[string]any, error) {
	content, meta, err := stripMarkdownFrontmatter(source)
	if err != nil {
		return "", nil, err
	}

	rendered, err := renderMarkdownDocument(content)
	if err != nil {
		return "", nil, err
	}
	return rendered, meta, nil
}

func renderMarkdownDocument(source []byte) (string, error) {
	source = preserveMarkdownDirectiveTags(source)

	var buf bytes.Buffer
	if err := markdownRenderer.Convert(source, &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}

	sanitized := markdownSanitizer.Sanitize(buf.String())
	decorated, err := decorateMarkdownHTML(sanitized)
	if err != nil {
		return "", fmt.Errorf("decorate markdown: %w", err)
	}
	return decorated, nil
}

func AnalyzeMarkdownForDirectoryShare(source []byte) (MarkdownDirectoryShareAnalysis, error) {
	content, _, err := stripMarkdownFrontmatter(source)
	if err != nil {
		return MarkdownDirectoryShareAnalysis{}, err
	}

	rendered, err := renderMarkdownDocument(content)
	if err != nil {
		return MarkdownDirectoryShareAnalysis{}, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(wrapMarkdownFragment(rendered)))
	if err != nil {
		return MarkdownDirectoryShareAnalysis{}, fmt.Errorf("parse markdown links: %w", err)
	}

	var analysis MarkdownDirectoryShareAnalysis
	doc.Find("a[href], img[src]").Each(func(_ int, sel *goquery.Selection) {
		attr := "href"
		if goquery.NodeName(sel) == "img" {
			attr = "src"
		}
		target, ok := sel.Attr(attr)
		if !ok {
			return
		}
		isLocal, escapes := analyzeMarkdownStandaloneTarget(target)
		if !isLocal {
			return
		}
		analysis.NeedsDirectoryShare = true
		if escapes {
			analysis.HasEscapingTargets = true
		}
	})

	return analysis, nil
}

func stripMarkdownFrontmatter(source []byte) ([]byte, map[string]any, error) {
	const delimiterPrefix = "---\n"

	if !bytes.HasPrefix(source, []byte(delimiterPrefix)) {
		return source, nil, nil
	}

	start := len(delimiterPrefix)
	offset := start
	for offset <= len(source) {
		lineEnd := bytes.IndexByte(source[offset:], '\n')
		if lineEnd < 0 {
			if bytes.Equal(source[offset:], markdownFrontmatterFence) {
				return parseMarkdownFrontmatter(source[start:offset], nil)
			}
			return source, nil, nil
		}

		line := source[offset : offset+lineEnd]
		if bytes.Equal(line, markdownFrontmatterFence) {
			return parseMarkdownFrontmatter(source[start:offset], source[offset+lineEnd+1:])
		}
		offset += lineEnd + 1
	}

	return source, nil, nil
}

func parseMarkdownFrontmatter(frontmatter []byte, body []byte) ([]byte, map[string]any, error) {
	meta := map[string]any{}
	if len(bytes.TrimSpace(frontmatter)) == 0 {
		return body, meta, nil
	}
	if err := yaml.Unmarshal(frontmatter, &meta); err != nil {
		return nil, nil, fmt.Errorf("parse markdown frontmatter: %w", err)
	}
	return body, meta, nil
}

func RewriteMarkdownLinks(rendered string, currentRel string, resolver markdownLinkResolver) (string, error) {
	if rendered == "" || resolver == nil {
		return rendered, nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(wrapMarkdownFragment(rendered)))
	if err != nil {
		return "", fmt.Errorf("parse markdown links: %w", err)
	}

	currentDir := path.Dir(currentRel)
	if currentDir == "." {
		currentDir = ""
	}

	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href, ok := sel.Attr("href")
		if !ok {
			return
		}
		if rewritten, ok := rewriteMarkdownTarget(href, currentDir, false, resolver); ok {
			sel.SetAttr("href", rewritten)
		}
	})

	rewriteMediaSources(doc, func(src string) (string, bool) {
		return rewriteMarkdownTarget(src, currentDir, true, resolver)
	})

	html, err := doc.Find("#share-markdown-root").Html()
	if err != nil {
		return "", fmt.Errorf("extract markdown links: %w", err)
	}
	return html, nil
}

const markdownMediaSrcSelector = "img[src], video[src], audio[src], source[src], track[src]"

func rewriteMediaSources(doc *goquery.Document, rewriter func(src string) (string, bool)) {
	doc.Find(markdownMediaSrcSelector).Each(func(_ int, sel *goquery.Selection) {
		src, ok := sel.Attr("src")
		if !ok {
			return
		}
		if rewritten, ok := rewriter(src); ok {
			sel.SetAttr("src", rewritten)
		}
	})
}

func RewriteServePreviewImageSources(rendered string, externalBase string) (string, error) {
	if rendered == "" {
		return rendered, nil
	}

	baseURL, _ := url.Parse(strings.TrimSpace(externalBase))

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(wrapMarkdownFragment(rendered)))
	if err != nil {
		return "", fmt.Errorf("parse markdown images: %w", err)
	}

	rewriteMediaSources(doc, func(src string) (string, bool) {
		return rewriteServePreviewImageSource(src, baseURL)
	})

	html, err := doc.Find("#share-markdown-root").Html()
	if err != nil {
		return "", fmt.Errorf("extract markdown images: %w", err)
	}
	return html, nil
}

var markdownSanitizer = buildMarkdownSanitizer()

func buildMarkdownSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("input")
	p.AllowAttrs("type").Matching(markdownCheckboxTypePattern).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(markdownCheckboxAttrPattern).OnElements("input")
	p.AllowAttrs("class").Matching(markdownMermaidClassPattern).OnElements("code")
	p.AllowAttrs("target").Matching(bluemonday.Paragraph).OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	allowInlineSVG(p)
	allowInlineMedia(p)
	return p
}

var inlineMediaElements = []string{"video", "audio", "source", "track"}

// allowInlineMedia extends p with the safe subset of HTML media elements so
// hand-authored <video>/<audio> blocks survive sanitization. URL schemes for
// src/poster are still constrained by UGCPolicy's AllowStandardURLs.
func allowInlineMedia(p *bluemonday.Policy) {
	p.AllowElements(inlineMediaElements...)

	p.AllowAttrs(
		"src", "controls", "preload", "muted", "loop",
		"autoplay", "playsinline", "controlslist", "crossorigin",
	).OnElements("video", "audio")
	p.AllowAttrs("width", "height", "poster").OnElements("video")

	p.AllowAttrs("src", "type", "media").OnElements("source")
	p.AllowAttrs("src", "kind", "srclang", "label", "default").OnElements("track")
}

var inlineSVGElements = []string{
	"svg", "g", "defs", "symbol", "use", "title", "desc",
	"path", "rect", "circle", "ellipse", "line", "polyline", "polygon",
	"text", "tspan",
	"linearGradient", "radialGradient", "stop",
	"marker", "mask", "clipPath", "pattern",
}

// allowInlineSVG extends p with the safe subset of SVG needed to render
// hand-authored diagrams embedded in markdown. Scripting surfaces
// (<script>, <foreignObject>, on* handlers) are intentionally omitted.
func allowInlineSVG(p *bluemonday.Policy) {
	p.AllowElements(inlineSVGElements...)

	p.AllowAttrs(
		"xmlns", "viewBox", "preserveAspectRatio",
		"width", "height",
		"x", "y", "x1", "y1", "x2", "y2",
		"cx", "cy", "r", "rx", "ry",
		"d", "points", "transform",
		"fill", "fill-rule", "fill-opacity", "clip-rule",
		"stroke", "stroke-width", "stroke-linecap", "stroke-linejoin",
		"stroke-dasharray", "stroke-dashoffset", "stroke-miterlimit", "stroke-opacity",
		"opacity", "id", "class", "role", "aria-label",
		"font-family", "font-size", "font-weight", "text-anchor",
		"dominant-baseline", "letter-spacing",
		"offset", "stop-color", "stop-opacity",
		"gradientUnits", "gradientTransform", "spreadMethod",
		"markerWidth", "markerHeight", "markerUnits", "refX", "refY", "orient",
		"patternUnits", "patternTransform", "maskUnits", "maskContentUnits",
		"clipPathUnits",
	).OnElements(inlineSVGElements...)
}

func decorateMarkdownHTML(rendered string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(wrapMarkdownFragment(rendered)))
	if err != nil {
		return "", err
	}

	doc.Find("h1[id], h2[id], h3[id], h4[id], h5[id], h6[id]").Each(func(_ int, sel *goquery.Selection) {
		id, ok := sel.Attr("id")
		if !ok || strings.TrimSpace(id) == "" {
			return
		}
		anchor := fmt.Sprintf(
			`<a class="heading-anchor" href="#%s" aria-label="Link to this section"></a>`,
			html.EscapeString(id),
		)
		sel.PrependHtml(anchor)
	})

	doc.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		src, ok := sel.Attr("src")
		if !ok {
			return
		}
		switch mediaKindForSrc(src) {
		case PreviewVideo:
			retagImgAsMediaElement(sel, "video")
		case PreviewAudio:
			retagImgAsMediaElement(sel, "audio")
		}
	})

	doc.Find("pre").Each(func(_ int, sel *goquery.Selection) {
		if decorateMarkdownMermaidBlock(sel) {
			return
		}
		decorateMarkdownCopyBlock(sel, "code", "Copy code block")
	})
	doc.Find("blockquote").Each(func(_ int, sel *goquery.Selection) {
		decorateMarkdownAlert(sel)
		decorateMarkdownCopyBlock(sel, "quote", "Copy quote")
	})
	doc.Find("details").Each(func(_ int, sel *goquery.Selection) {
		decorateMarkdownCopyBlock(sel, "details", "Copy details")
	})
	doc.Find("table").Each(func(_ int, sel *goquery.Selection) {
		sel.WrapHtml(`<div class="markdown-table-scroll"></div>`)
	})

	html, err := doc.Find("#share-markdown-root").Html()
	if err != nil {
		return "", err
	}
	return html, nil
}

func decorateMarkdownMermaidBlock(sel *goquery.Selection) bool {
	if sel == nil {
		return false
	}
	code := sel.ChildrenFiltered("code.language-mermaid").First()
	if code.Length() == 0 {
		return false
	}

	sel.WrapHtml(`<div class="mermaid-block copyable-block copyable-block-code"></div>`)
	addMarkdownHTMLClass(sel, "mermaid-source")
	wrapper := sel.Parent()
	wrapper.PrependHtml(renderMarkdownCopyButtonHTML("code", "Copy diagram source"))
	wrapper.AppendHtml(`<div class="mermaid-diagram" role="img" aria-label="Mermaid diagram"></div>`)
	return true
}

func decorateMarkdownAlert(sel *goquery.Selection) {
	if sel == nil {
		return
	}
	firstParagraph := sel.ChildrenFiltered("p").First()
	kind, ok := removeMarkdownAlertMarker(firstParagraph)
	if !ok {
		return
	}

	addMarkdownHTMLClass(sel, "markdown-alert")
	addMarkdownHTMLClass(sel, "markdown-alert-"+kind)
	title := `<p class="markdown-alert-title">` + html.EscapeString(markdownAlertTitle(kind)) + `</p>`
	sel.PrependHtml(title)

	if strings.TrimSpace(firstParagraph.Text()) == "" && firstParagraph.Children().Length() == 0 {
		firstParagraph.Remove()
	}
}

func removeMarkdownAlertMarker(sel *goquery.Selection) (string, bool) {
	if sel == nil || sel.Length() == 0 {
		return "", false
	}
	for _, node := range sel.Contents().Nodes {
		if node.Type != nethtml.TextNode {
			if strings.TrimSpace(node.Data) == "" {
				continue
			}
			return "", false
		}
		match := markdownAlertMarkerPattern.FindStringSubmatchIndex(node.Data)
		if match == nil {
			if strings.TrimSpace(node.Data) == "" {
				continue
			}
			return "", false
		}
		kind := strings.ToLower(node.Data[match[2]:match[3]])
		node.Data = node.Data[match[1]:]
		return kind, true
	}
	return "", false
}

func markdownAlertTitle(kind string) string {
	switch kind {
	case "note":
		return "Note"
	case "tip":
		return "Tip"
	case "important":
		return "Important"
	case "warning":
		return "Warning"
	case "caution":
		return "Caution"
	default:
		return kind
	}
}

func mediaKindForSrc(src string) PreviewKind {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil || u.Path == "" {
		return ""
	}
	return ClassifyPreviewKind(path.Base(u.Path))
}

func retagImgAsMediaElement(sel *goquery.Selection, tag string) {
	node := sel.Get(0)
	if node == nil {
		return
	}
	node.Data = tag
	sel.RemoveAttr("alt")
	sel.SetAttr("controls", "")
	sel.SetAttr("preload", "metadata")
}

func decorateMarkdownCopyBlock(sel *goquery.Selection, kind string, label string) {
	if sel == nil {
		return
	}
	addMarkdownHTMLClass(sel, "copyable-block")
	addMarkdownHTMLClass(sel, "copyable-block-"+kind)
	sel.PrependHtml(renderMarkdownCopyButtonHTML(kind, label))
}

func addMarkdownHTMLClass(sel *goquery.Selection, className string) {
	if sel == nil || className == "" {
		return
	}
	current, _ := sel.Attr("class")
	parts := strings.Fields(current)
	for _, part := range parts {
		if part == className {
			return
		}
	}
	if current == "" {
		sel.SetAttr("class", className)
		return
	}
	sel.SetAttr("class", current+" "+className)
}

func renderMarkdownCopyButtonHTML(kind string, label string) string {
	escapedKind := html.EscapeString(kind)
	escapedLabel := html.EscapeString(label)
	return `<button class="action block-copy-button" type="button" data-copy-kind="` + escapedKind +
		`" data-copy-label="` + escapedLabel + `" title="` + escapedLabel + `" aria-label="` +
		escapedLabel + `">` + renderCopyButtonIconsHTML() + `</button>`
}

func rewriteMarkdownTarget(target string, currentDir string, isImage bool, resolver markdownLinkResolver) (string, bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return "", false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" || strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	if parsed.Path == "" {
		return "", false
	}

	resolvedPath := cleanMarkdownPath(currentDir, parsed.Path)
	rewritten := resolver(resolvedPath, isImage)
	if rewritten == "" {
		return "", false
	}

	rewrittenURL, err := url.Parse(rewritten)
	if err != nil {
		return rewritten, true
	}
	if parsed.RawQuery != "" {
		query := rewrittenURL.Query()
		for key, values := range parsed.Query() {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		rewrittenURL.RawQuery = query.Encode()
	}
	if parsed.Fragment != "" {
		rewrittenURL.Fragment = parsed.Fragment
	}
	return rewrittenURL.String(), true
}

func rewriteServePreviewImageSource(target string, baseURL *url.URL) (string, bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return "", false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.Path == "" {
		return "", false
	}

	if parsed.IsAbs() {
		if baseURL == nil || !sameHostURL(parsed, baseURL) {
			return "", false
		}
	}

	rewrittenPath, ok := rewriteServePreviewPath(parsed.Path, baseURL)
	if !ok {
		return "", false
	}
	parsed.Path = rewrittenPath
	return parsed.String(), true
}

func rewriteServePreviewPath(targetPath string, baseURL *url.URL) (string, bool) {
	basePath := ""
	if baseURL != nil {
		basePath = strings.TrimRight(baseURL.EscapedPath(), "/")
	}

	if basePath != "" {
		prefix := basePath + "/s/"
		if strings.HasPrefix(targetPath, prefix) {
			return basePath + "/r/" + strings.TrimPrefix(targetPath, prefix), true
		}
	}

	if strings.HasPrefix(targetPath, "/s/") {
		return "/r/" + strings.TrimPrefix(targetPath, "/s/"), true
	}

	return "", false
}

func sameHostURL(left *url.URL, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}

	return strings.EqualFold(left.Hostname(), right.Hostname()) && left.Port() == right.Port()
}

func cleanMarkdownPath(currentDir string, target string) string {
	joined := target
	if currentDir != "" {
		joined = path.Join(currentDir, target)
	}

	cleaned := path.Clean("/" + joined)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func analyzeMarkdownStandaloneTarget(target string) (isLocal bool, escapesRoot bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return false, false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false, false
	}
	if parsed.Scheme != "" || strings.HasPrefix(parsed.Path, "/") || parsed.Path == "" {
		return false, false
	}

	cleaned := path.Clean(parsed.Path)
	if cleaned == "." || cleaned == "" {
		return false, false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return true, true
	}
	return true, false
}

func wrapMarkdownFragment(rendered string) string {
	return `<div id="share-markdown-root">` + rendered + `</div>`
}

func preserveMarkdownDirectiveTags(source []byte) []byte {
	if len(source) == 0 {
		return source
	}

	original := string(source)
	lines := strings.Split(original, "\n")
	output := make([]string, 0, len(lines)+8)
	inFence := false
	fenceMarker := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if marker, ok := markdownFenceMarker(trimmed); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if len(marker) >= len(fenceMarker) && marker[0] == fenceMarker[0] {
				inFence = false
				fenceMarker = ""
			}
			output = append(output, line)
			continue
		}

		if !inFence {
			if rewritten, ok := rewriteMarkdownDirectiveTagLine(line); ok {
				if len(output) > 0 && strings.TrimSpace(output[len(output)-1]) != "" {
					output = append(output, "")
				}
				output = append(output, rewritten)
				output = append(output, "")
				continue
			}
		}

		output = append(output, line)
	}

	result := strings.Join(output, "\n")
	if strings.HasSuffix(original, "\n") {
		result += "\n"
	}
	return []byte(result)
}

func markdownFenceMarker(line string) (string, bool) {
	if line == "" {
		return "", false
	}

	fenceChar := line[0]
	if fenceChar != '`' && fenceChar != '~' {
		return "", false
	}

	runLength := 0
	for runLength < len(line) && line[runLength] == fenceChar {
		runLength++
	}
	if runLength < 3 {
		return "", false
	}
	return line[:runLength], true
}

func rewriteMarkdownDirectiveTagLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if trimmed == "" {
		return "", false
	}

	matches := markdownDirectiveTagPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return "", false
	}
	if _, ok := markdownDirectiveTags[strings.ToLower(matches[1])]; !ok {
		return "", false
	}

	indentWidth := len(line) - len(strings.TrimLeft(line, " \t"))
	if indentWidth < 0 || indentWidth > len(line) {
		indentWidth = 0
	}
	return line[:indentWidth] + "`" + trimmed + "`", true
}
