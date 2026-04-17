package share

import (
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

type MarkdownPreviewField struct {
	Label string
	Value string
}

type MarkdownPreviewTag struct {
	Label string
}

func RenderMarkdownPreviewPage(
	title string,
	rendered string,
	rawURL string,
	breadcrumbs []Breadcrumb,
	meta map[string]any,
) (string, error) {
	nav := renderBreadcrumbHTML(breadcrumbs, title)
	actions := renderActionsHTML(rawURL, canCopyContents(PreviewMarkdown))
	documentTitle, displayTitle, summary, metadata, tags, fields := buildMarkdownPreviewSections(title, meta)

	data := struct {
		DocumentTitle string
		Title         string
		Summary       string
		Nav           template.HTML
		Actions       template.HTML
		ActionScript  template.HTML
		Meta          []MarkdownPreviewField
		Tags          []MarkdownPreviewTag
		Fields        []MarkdownPreviewField
		Content       template.HTML
	}{
		DocumentTitle: documentTitle,
		Title:         displayTitle,
		Summary:       summary,
		Nav:           template.HTML(nav),
		Actions:       template.HTML(actions),
		ActionScript:  template.HTML(previewActionScriptTag),
		Meta:          metadata,
		Tags:          tags,
		Fields:        fields,
		Content:       template.HTML(rendered),
	}

	var b strings.Builder
	if err := markdownPreviewTemplate.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render markdown page: %w", err)
	}
	return b.String(), nil
}

func buildMarkdownPreviewSections(
	fallbackTitle string,
	meta map[string]any,
) (string, string, string, []MarkdownPreviewField, []MarkdownPreviewTag, []MarkdownPreviewField) {
	documentTitle := trimMarkdownPreviewTitle(fallbackTitle)
	title := strings.TrimSpace(formatMarkdownPreviewValue(meta["title"]))
	if title != "" {
		documentTitle = title
	}
	summary := strings.TrimSpace(formatMarkdownPreviewValue(firstNonNil(meta["summary"], meta["description"])))
	if len(meta) == 0 {
		return documentTitle, "", summary, nil, nil, nil
	}

	fields := buildMarkdownFieldItems(meta["fields"])
	tags := buildMarkdownTags(meta["tags"])

	keys := make([]string, 0, len(meta))
	for key, value := range meta {
		if shouldSkipMarkdownMetaKey(key) || strings.TrimSpace(formatMarkdownPreviewValue(value)) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return metadataKeyRank(keys[i]) < metadataKeyRank(keys[j])
	})

	items := make([]MarkdownPreviewField, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(formatMarkdownPreviewValue(meta[key]))
		if value == "" {
			continue
		}
		items = append(items, MarkdownPreviewField{
			Label: humanizeMarkdownPreviewKey(key),
			Value: value,
		})
	}

	return documentTitle, title, summary, items, tags, fields
}

func trimMarkdownPreviewTitle(title string) string {
	title = strings.TrimSpace(title)
	for _, suffix := range []string{".markdown", ".mdown", ".mkdn", ".mkd", ".md"} {
		if strings.HasSuffix(strings.ToLower(title), suffix) {
			return strings.TrimSpace(title[:len(title)-len(suffix)])
		}
	}
	return title
}

func buildMarkdownFieldItems(raw any) []MarkdownPreviewField {
	if raw == nil {
		return nil
	}

	switch typed := raw.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key, value := range typed {
			if strings.TrimSpace(formatMarkdownPreviewValue(value)) == "" {
				continue
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return strings.ToLower(keys[i]) < strings.ToLower(keys[j])
		})
		items := make([]MarkdownPreviewField, 0, len(keys))
		for _, key := range keys {
			items = append(items, MarkdownPreviewField{
				Label: humanizeMarkdownPreviewKey(key),
				Value: formatMarkdownPreviewValue(typed[key]),
			})
		}
		return items
	default:
		value := strings.TrimSpace(formatMarkdownPreviewValue(raw))
		if value == "" {
			return nil
		}
		return []MarkdownPreviewField{{
			Label: "Value",
			Value: value,
		}}
	}
}

func buildMarkdownTags(raw any) []MarkdownPreviewTag {
	switch typed := raw.(type) {
	case nil:
		return nil
	case []any:
		tags := make([]MarkdownPreviewTag, 0, len(typed))
		for _, value := range typed {
			label := strings.TrimSpace(formatMarkdownPreviewValue(value))
			if label == "" {
				continue
			}
			tags = append(tags, MarkdownPreviewTag{Label: label})
		}
		return tags
	case string:
		label := strings.TrimSpace(typed)
		if label == "" {
			return nil
		}
		return []MarkdownPreviewTag{{Label: label}}
	default:
		label := strings.TrimSpace(formatMarkdownPreviewValue(raw))
		if label == "" {
			return nil
		}
		return []MarkdownPreviewTag{{Label: label}}
	}
}

func metadataKeyRank(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "status":
		return "00-status"
	case "draft":
		return "01-draft"
	case "author", "authors":
		return "02-author"
	case "date", "created", "created_at", "published", "published_at":
		return "03-date"
	case "updated", "updated_at", "last_updated":
		return "04-updated"
	case "category", "categories":
		return "05-category"
	default:
		return "10-" + normalized
	}
}

func shouldSkipMarkdownMetaKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "title", "summary", "description", "tags", "fields":
		return true
	default:
		return false
	}
}

func humanizeMarkdownPreviewKey(key string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(key), func(r rune) bool {
		return r == '_' || r == '-'
	})
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func formatMarkdownPreviewValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		if len(typed) == 0 {
			return ""
		}
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			switch item.(type) {
			case map[string]any, []any:
				return marshalMarkdownPreviewValue(value)
			default:
				formatted := strings.TrimSpace(formatMarkdownPreviewValue(item))
				if formatted != "" {
					parts = append(parts, formatted)
				}
			}
		}
		return strings.Join(parts, ", ")
	default:
		return marshalMarkdownPreviewValue(value)
	}
}

func marshalMarkdownPreviewValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return string(encoded)
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

var markdownPreviewTemplate = template.Must(template.New("markdown-preview").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="color-scheme" content="light dark" />
<title>{{ .DocumentTitle }}</title>
<style>
` + previewThemeCSS + `
.breadcrumb{display:flex;align-items:center;gap:4px;min-width:0;font-size:0.95rem}
.breadcrumb a{color:var(--preview-link);font-weight:400}
.breadcrumb .sep{color:var(--preview-text-muted);flex-shrink:0}
.breadcrumb .current{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
html {
  background: var(--preview-bg);
  color-scheme: light dark;
}
body {
  margin: 0;
  background:
    radial-gradient(circle at top, rgba(9, 105, 218, 0.08), transparent 34rem),
    var(--preview-bg);
  color: var(--preview-text);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif;
}

a {
  color: var(--preview-link);
  text-decoration: none;
}

a:hover { text-decoration: underline; }

.preview-shell {
  width: min(100%, 70rem);
  margin: 0 auto;
  padding: 1.5rem 1rem 3rem;
}

.preview-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.9rem 1rem;
  border-bottom: 1px solid var(--preview-border);
  background: var(--preview-canvas);
}

.preview-title {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
  flex: 1 1 auto;
}

.preview-title strong {
  font-size: 0.95rem;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.actions{display:flex;gap:6px;flex-shrink:0}
.action{display:inline-flex;align-items:center;justify-content:center;width:2rem;height:2rem;
  border:1px solid var(--preview-border);border-radius:6px;background:var(--preview-canvas);
  color:var(--preview-text);text-decoration:none}
.action:disabled{cursor:default;opacity:.8}
.action:hover{background:color-mix(in srgb, var(--preview-canvas) 88%, var(--preview-bg));text-decoration:none}
.action.is-error{color:#d1242f}
.action .icon-check{display:none}
.action.is-success .icon-copy{display:none}
.action.is-success .icon-check{display:block}
.action svg{width:1rem;height:1rem;stroke:currentColor;fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}

.preview-card {
  border: 1px solid var(--preview-border);
  border-radius: 6px;
  background: var(--preview-canvas);
  box-shadow: 0 18px 38px rgba(31, 35, 40, 0.06);
  overflow: hidden;
}

.markdown-header {
  display: grid;
  gap: 0.9rem;
  padding: 2rem 2rem 0;
}

.markdown-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.markdown-tag {
  display: inline-flex;
  align-items: center;
  padding: 0.2rem 0.65rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--preview-link) 12%, transparent);
  color: var(--preview-link);
  font-size: 0.78rem;
  font-weight: 600;
}

.markdown-title {
  margin: 0;
  font-size: clamp(1.8rem, 3vw, 2.6rem);
  line-height: 1.08;
  letter-spacing: -0.02em;
}

.markdown-summary {
  margin: 0;
  max-width: 58ch;
  color: var(--preview-text-muted);
  font-size: 1.05rem;
  line-height: 1.65;
}

.markdown-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.25rem;
  padding: 0;
  margin: 0;
  list-style: none;
}

.markdown-facts li {
  display: inline-flex;
  align-items: baseline;
  gap: 0.45rem;
  min-width: 0;
  font-size: 0.92rem;
}

.markdown-fact-label {
  margin: 0;
  color: var(--preview-text-muted);
  font-weight: 500;
}

.markdown-fact-value {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}

.markdown-details {
  display: grid;
  grid-template-columns: minmax(8rem, 12rem) minmax(0, 1fr);
  gap: 0.65rem 1rem;
  padding-top: 1rem;
  margin: 0;
  border-top: 1px solid var(--preview-border-muted);
}

.markdown-details dt {
  margin: 0;
  color: var(--preview-text-muted);
  font-size: 0.88rem;
  font-weight: 500;
}

.markdown-details dd {
  margin: 0;
  min-width: 0;
  font-size: 0.95rem;
  overflow-wrap: anywhere;
}

.markdown-meta {
  display: grid;
  gap: 1rem;
  padding: 1.25rem 1.25rem 0;
}

.markdown-meta-section {
  padding: 1rem;
  border: 1px solid var(--preview-border);
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--preview-canvas) 92%, var(--preview-bg));
}

.markdown-meta-section h2 {
  margin: 0 0 0.875rem;
  font-size: 0.95rem;
  font-weight: 600;
}

.markdown-meta-grid {
  display: grid;
  grid-template-columns: minmax(8rem, 12rem) minmax(0, 1fr);
  gap: 0.75rem 1rem;
  margin: 0;
}

.markdown-meta-grid dt {
  margin: 0;
  color: var(--preview-text-muted);
  font-size: 0.875rem;
  font-weight: 500;
}

.markdown-meta-grid dd {
  margin: 0;
  min-width: 0;
  font-size: 0.95rem;
  overflow-wrap: anywhere;
}

.markdown-body {
  min-width: 0;
  max-width: 100%;
  margin: 0 auto;
  padding: 2rem;
  font-size: 1rem;
  line-height: 1.7;
  color: var(--preview-text);
  overflow-wrap: break-word;
}

.markdown-body > *:first-child { margin-top: 0 !important; }
.markdown-body > *:last-child { margin-bottom: 0 !important; }

.markdown-body h1,
.markdown-body h2,
.markdown-body h3,
.markdown-body h4,
.markdown-body h5,
.markdown-body h6 {
  position: relative;
  margin-bottom: 1rem;
  font-weight: 600;
  line-height: 1.25;
}

.markdown-body h1,
.markdown-body h2 {
  padding-bottom: 0.3em;
  border-bottom: 1px solid var(--preview-border-muted);
}

.markdown-body h1 { margin-top: 2rem; font-size: 2rem; }
.markdown-body h2 { margin-top: 2rem; font-size: 1.5rem; }
.markdown-body h3 { margin-top: 1.75rem; font-size: 1.25rem; font-weight: 500; }
.markdown-body h4 { margin-top: 1.5rem; font-size: 1rem; font-weight: 500; }
.markdown-body h5 { margin-top: 1.5rem; font-size: 0.875rem; font-weight: 500; }
.markdown-body h6 { margin-top: 1.5rem; font-size: 0.85rem; font-weight: 500; color: var(--preview-text-muted); }

.markdown-body p,
.markdown-body blockquote,
.markdown-body ul,
.markdown-body ol,
.markdown-body dl,
.markdown-body table,
.markdown-body pre,
.markdown-body details {
  margin-top: 0;
  margin-bottom: 1rem;
}

.markdown-body ul,
.markdown-body ol { padding-left: 2em; }
.markdown-body li + li { margin-top: 0.25rem; }
.markdown-body li > p { margin-top: 0.4rem; }

.markdown-body .heading-anchor {
  position: absolute;
  top: 0;
  left: -1.1em;
  width: 1em;
  opacity: 0;
  color: var(--preview-text-muted);
}

.markdown-body .heading-anchor::before { content: "#"; }
.markdown-body h1:hover .heading-anchor,
.markdown-body h2:hover .heading-anchor,
.markdown-body h3:hover .heading-anchor,
.markdown-body h4:hover .heading-anchor,
.markdown-body h5:hover .heading-anchor,
.markdown-body h6:hover .heading-anchor,
.markdown-body .heading-anchor:focus { opacity: 1; }

.markdown-body code,
.markdown-body tt {
  padding: 0.2em 0.4em;
  margin: 0;
  border-radius: 0.375rem;
  background: var(--preview-code-bg);
  box-shadow: var(--preview-inline-shadow);
  font-size: 0.875em;
  font-family: ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, monospace;
}

.markdown-body pre {
  padding: 1rem;
  overflow: auto;
  border: 1px solid var(--preview-border);
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--preview-canvas) 84%, var(--preview-bg));
}

.markdown-body pre code {
  padding: 0;
  background: transparent;
  box-shadow: none;
  border-radius: 0;
  display: block;
  line-height: 1.55;
}

.markdown-body blockquote {
  margin-left: 0;
  padding: 0 1em;
  color: var(--preview-text-muted);
  border-left: 0.25em solid var(--preview-border-muted);
}

.markdown-body hr {
  height: 0.25rem;
  padding: 0;
  margin: 1.5rem 0;
  background: var(--preview-border-muted);
  border: 0;
}

.markdown-body table {
  display: block;
  width: max-content;
  max-width: 100%;
  overflow: auto;
  border-spacing: 0;
}

.markdown-body table th,
.markdown-body table td {
  padding: 0.5rem 0.8rem;
  border: 1px solid var(--preview-border);
}

.markdown-body table tr {
  background: var(--preview-canvas);
  border-top: 1px solid var(--preview-border);
}

.markdown-body table tr:nth-child(2n) {
  background: color-mix(in srgb, var(--preview-canvas) 94%, var(--preview-bg));
}

.markdown-body table th {
  font-weight: 600;
  background: color-mix(in srgb, var(--preview-canvas) 88%, var(--preview-bg));
}

.markdown-body img {
  display: block;
  max-width: 100%;
  height: auto;
  border-radius: 0.75rem;
}

.markdown-body input[type="checkbox"] {
  margin: 0 0.5rem 0.2rem -1.5rem;
  vertical-align: middle;
  pointer-events: none;
}

.markdown-body li:has(> input[type="checkbox"]) {
  list-style: none;
}

.markdown-body details {
  padding: 0 0 0 1rem;
  border-left: 3px solid var(--preview-border-muted);
}

.markdown-body summary {
  cursor: pointer;
  font-weight: 600;
  color: var(--preview-text);
}

.markdown-body .copyable-block {
  position: relative;
}

.markdown-body .block-copy-button {
  position: absolute;
  top: 0.75rem;
  right: 0.75rem;
  z-index: 1;
  opacity: 0;
  pointer-events: none;
  transition: opacity 120ms ease;
}

.markdown-body .copyable-block:hover > .block-copy-button,
.markdown-body .copyable-block:focus-within > .block-copy-button {
  opacity: 1;
  pointer-events: auto;
}

.markdown-body pre.copyable-block {
  padding-right: 3.25rem;
}

.markdown-body blockquote.copyable-block {
  padding: 0.75rem 3rem 0.75rem 1em;
}

.markdown-body details.copyable-block {
  padding-right: 3rem;
}

.markdown-body details.copyable-block > summary {
  padding-right: 1.5rem;
}

@media (hover: none), (pointer: coarse) {
  .markdown-body .block-copy-button {
    opacity: 1;
    pointer-events: auto;
  }
}

@media (max-width: 760px) {
  .markdown-header {
    padding: 1.25rem 1.2rem 0;
  }

  .markdown-details {
    grid-template-columns: 1fr;
    gap: 0.375rem 0;
  }

  .markdown-details dd {
    margin-bottom: 0.75rem;
  }
  .markdown-body {
    padding: 1.2rem;
    font-size: 0.96rem;
  }

  .markdown-body .block-copy-button {
    opacity: 1;
    pointer-events: auto;
    top: 0.6rem;
    right: 0.6rem;
  }

  .markdown-body pre.copyable-block {
    padding-right: 3rem;
  }

  .markdown-body blockquote.copyable-block {
    padding-right: 2.75rem;
  }

  .markdown-body details.copyable-block {
    padding-right: 2.75rem;
  }

  .markdown-body .heading-anchor {
    display: none;
  }
}
</style>
</head>
<body>
<header class="preview-bar">
  <div class="preview-title">
    {{ if .Nav }}{{ .Nav }}{{ else }}<strong>{{ .Title }}</strong>{{ end }}
  </div>
  {{ .Actions }}
</header>
<main class="preview-shell">
  <div class="preview-card">
    {{ if or .Title .Summary .Meta .Tags .Fields }}
    <header class="markdown-header">
      {{ if .Tags }}
      <div class="markdown-tags">
        {{ range .Tags }}
        <span class="markdown-tag">{{ .Label }}</span>
        {{ end }}
      </div>
      {{ end }}
      {{ if .Title }}
      <h1 class="markdown-title">{{ .Title }}</h1>
      {{ end }}
      {{ if .Summary }}
      <p class="markdown-summary">{{ .Summary }}</p>
      {{ end }}
      {{ if .Meta }}
      <ul class="markdown-facts">
        {{ range .Meta }}
        <li>
          <span class="markdown-fact-label">{{ .Label }}</span>
          <span class="markdown-fact-value">{{ .Value }}</span>
        </li>
        {{ end }}
      </ul>
      {{ end }}
      {{ if .Fields }}
      <dl class="markdown-details">
        {{ range .Fields }}
        <dt>{{ .Label }}</dt>
        <dd>{{ .Value }}</dd>
        {{ end }}
      </dl>
      {{ end }}
    </header>
    {{ end }}
    <article class="markdown-body">{{ .Content }}</article>
  </div>
</main>
{{ .ActionScript }}
</body>
</html>`))
