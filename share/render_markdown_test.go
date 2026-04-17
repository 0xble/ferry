package share

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderMarkdownDocumentSupportsGFMAndSanitizes(t *testing.T) {
	t.Parallel()

	rendered, meta, err := RenderMarkdownDocument([]byte(`# Hello

- [x] done

<details open><summary>More</summary>Body</details>

<script>alert("xss")</script>
`))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil metadata without frontmatter, got %#v", meta)
	}

	if !strings.Contains(rendered, `href="#hello"`) {
		t.Fatalf("expected heading anchor in rendered markdown, got %q", rendered)
	}
	if !strings.Contains(rendered, `<input checked="" disabled="" type="checkbox"/>`) {
		t.Fatalf("expected task list checkbox in rendered markdown, got %q", rendered)
	}
	if !strings.Contains(rendered, `<details open="" class="copyable-block copyable-block-details">`) {
		t.Fatalf("expected details block in rendered markdown, got %q", rendered)
	}
	if strings.Contains(strings.ToLower(rendered), "<script") {
		t.Fatalf("expected script tag to be removed, got %q", rendered)
	}
}

func TestRenderMarkdownDocumentPreservesDirectiveTagsAndNestedMarkdown(t *testing.T) {
	t.Parallel()

	rendered, meta, err := RenderMarkdownDocument([]byte(`<scope>
- Workspace: hostandhomecleaners
- Only handle direct messages.
</scope>

<reply_logic>
- Greeting only -> reply with greeting only.
- Greeting + routine FYI -> acknowledge the FYI.
</reply_logic>

<details open><summary>More</summary>Body</details>
`))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil metadata without frontmatter, got %#v", meta)
	}

	if !strings.Contains(rendered, `<code>&lt;scope&gt;</code>`) {
		t.Fatalf("expected opening directive tag to render visibly, got %q", rendered)
	}
	if !strings.Contains(rendered, `<code>&lt;/reply_logic&gt;</code>`) {
		t.Fatalf("expected closing directive tag to render visibly, got %q", rendered)
	}
	if !strings.Contains(rendered, `<li>Workspace: hostandhomecleaners</li>`) {
		t.Fatalf("expected markdown list inside directive block to render, got %q", rendered)
	}
	if !strings.Contains(rendered, `<li>Greeting + routine FYI -&gt; acknowledge the FYI.</li>`) {
		t.Fatalf("expected nested markdown content to survive directive tags, got %q", rendered)
	}
	if !strings.Contains(rendered, `<details open="" class="copyable-block copyable-block-details">`) {
		t.Fatalf("expected real HTML markdown features to keep working, got %q", rendered)
	}
}

func TestRenderMarkdownDocumentLeavesDirectiveTagsInsideLongFencesUntouched(t *testing.T) {
	t.Parallel()

	rendered, _, err := RenderMarkdownDocument([]byte("````md\n<scope>\n- alpha\n</scope>\n````\n"))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}

	if !strings.Contains(rendered, `<pre class="copyable-block copyable-block-code">`) {
		t.Fatalf("expected fenced code block to render as code, got %q", rendered)
	}
	if !strings.Contains(rendered, `<code>&lt;scope&gt;`) {
		t.Fatalf("expected fenced code block contents to remain visible, got %q", rendered)
	}
	if !strings.Contains(rendered, "&lt;scope&gt;") || !strings.Contains(rendered, "&lt;/scope&gt;") {
		t.Fatalf("expected directive tags inside fence to stay literal, got %q", rendered)
	}
	if strings.Contains(rendered, `<p><code>&lt;scope&gt;</code></p>`) {
		t.Fatalf("expected directive tag preprocessor to skip fenced code, got %q", rendered)
	}
	if strings.Contains(rendered, "<ul>") {
		t.Fatalf("expected fenced content not to be reinterpreted as markdown lists, got %q", rendered)
	}
}

func TestRenderMarkdownDocumentStripsAndParsesFrontmatter(t *testing.T) {
	t.Parallel()

	rendered, meta, err := RenderMarkdownDocument([]byte(`---
title: Hello
draft: true
---
# Hello
`))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}

	if meta["title"] != "Hello" {
		t.Fatalf("expected title metadata, got %#v", meta)
	}
	if meta["draft"] != true {
		t.Fatalf("expected draft metadata, got %#v", meta)
	}
	if !strings.Contains(rendered, `<h1 id="hello">`) {
		t.Fatalf("expected markdown body to render after frontmatter, got %q", rendered)
	}
	if strings.Contains(rendered, "<hr>") || strings.Contains(rendered, "title:") {
		t.Fatalf("expected frontmatter to be stripped from rendered body, got %q", rendered)
	}
}

func TestRenderMarkdownDocumentDecoratesSupportedCopyBlocks(t *testing.T) {
	t.Parallel()

	rendered, _, err := RenderMarkdownDocument([]byte("```ts\nconst value = 1\n```\n\n> quoted\n\n<details><summary>More</summary>Body</details>\n"))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}

	if !strings.Contains(rendered, `pre class="copyable-block copyable-block-code"`) {
		t.Fatalf("expected code block to be copy-decorated, got %q", rendered)
	}
	if !strings.Contains(rendered, `blockquote class="copyable-block copyable-block-quote"`) {
		t.Fatalf("expected blockquote to be copy-decorated, got %q", rendered)
	}
	if !strings.Contains(rendered, `details class="copyable-block copyable-block-details"`) {
		t.Fatalf("expected details block to be copy-decorated, got %q", rendered)
	}
	for _, needle := range []string{
		`data-copy-kind="code"`,
		`data-copy-kind="quote"`,
		`data-copy-kind="details"`,
		`data-copy-label="Copy code block"`,
		`data-copy-label="Copy quote"`,
		`data-copy-label="Copy details"`,
	} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("expected rendered markdown to contain %q, got %q", needle, rendered)
		}
	}
}

func TestRenderMarkdownDocumentOmitsCopyDecorationForTables(t *testing.T) {
	t.Parallel()

	rendered, _, err := RenderMarkdownDocument([]byte("| Name | Value |\n| --- | --- |\n| alpha | beta |\n"))
	if err != nil {
		t.Fatalf("RenderMarkdownDocument: %v", err)
	}

	if strings.Contains(rendered, `data-copy-kind="table"`) {
		t.Fatalf("expected tables to omit copy decoration, got %q", rendered)
	}
	if strings.Contains(rendered, `copyable-block-table`) {
		t.Fatalf("expected tables to omit copy wrapper classes, got %q", rendered)
	}
}

func TestRewriteMarkdownLinksRewritesRelativeTargets(t *testing.T) {
	t.Parallel()

	rendered := `<p><a href="./guide.md?view=full#intro">Guide</a><img src="../img/diagram.png" alt="Diagram"></p>`
	got, err := RewriteMarkdownLinks(rendered, "docs/readme.md", func(target string, isImage bool) string {
		if isImage {
			return "/r/share123/" + target + "?t=token123"
		}
		return "/s/share123/" + target + "?t=token123"
	})
	if err != nil {
		t.Fatalf("RewriteMarkdownLinks: %v", err)
	}

	if !strings.Contains(got, `/s/share123/docs/guide.md?t=token123&amp;view=full#intro`) {
		t.Fatalf("expected rewritten preview link, got %q", got)
	}
	if !strings.Contains(got, `/r/share123/img/diagram.png?t=token123`) {
		t.Fatalf("expected rewritten raw image link, got %q", got)
	}
}

func TestRewriteServePreviewImageSourcesRewritesSameHostPreviewURLs(t *testing.T) {
	t.Parallel()

	rendered := `<p><img src="https://share.example.com/share/s/abc123?t=token123" alt="Preview"></p>`
	got, err := RewriteServePreviewImageSources(rendered, "https://share.example.com/share")
	if err != nil {
		t.Fatalf("RewriteServePreviewImageSources: %v", err)
	}

	if !strings.Contains(got, `src="https://share.example.com/share/r/abc123?t=token123"`) {
		t.Fatalf("expected rewritten raw image source, got %q", got)
	}
}

func TestRewriteServePreviewImageSourcesSkipsDifferentHosts(t *testing.T) {
	t.Parallel()

	rendered := `<p><img src="https://other.example.com/share/s/abc123?t=token123" alt="Preview"></p>`
	got, err := RewriteServePreviewImageSources(rendered, "https://share.example.com/share")
	if err != nil {
		t.Fatalf("RewriteServePreviewImageSources: %v", err)
	}

	if strings.Contains(got, `/share/r/abc123`) {
		t.Fatalf("expected different-host image source to remain unchanged, got %q", got)
	}
	if !strings.Contains(got, `src="https://other.example.com/share/s/abc123?t=token123"`) {
		t.Fatalf("expected original different-host image source, got %q", got)
	}
}

func TestAnalyzeMarkdownForDirectoryShareDetectsLocalTargets(t *testing.T) {
	t.Parallel()

	analysis, err := AnalyzeMarkdownForDirectoryShare([]byte(`# Hello

See [Guide](./guide.md)

![Diagram](./img/diagram.png)
`))
	if err != nil {
		t.Fatalf("AnalyzeMarkdownForDirectoryShare: %v", err)
	}
	if !analysis.NeedsDirectoryShare {
		t.Fatal("expected local markdown targets to require a directory share")
	}
	if analysis.HasEscapingTargets {
		t.Fatal("expected same-directory targets not to escape the markdown root")
	}
}

func TestAnalyzeMarkdownForDirectoryShareFlagsEscapingTargets(t *testing.T) {
	t.Parallel()

	analysis, err := AnalyzeMarkdownForDirectoryShare([]byte(`# Hello

![Diagram](../img/diagram.png)
`))
	if err != nil {
		t.Fatalf("AnalyzeMarkdownForDirectoryShare: %v", err)
	}
	if !analysis.NeedsDirectoryShare {
		t.Fatal("expected local relative target to require a directory share")
	}
	if !analysis.HasEscapingTargets {
		t.Fatal("expected parent-directory target to be flagged as escaping")
	}
}

func TestAnalyzeMarkdownForDirectoryShareIgnoresAbsoluteTargets(t *testing.T) {
	t.Parallel()

	analysis, err := AnalyzeMarkdownForDirectoryShare([]byte(`# Hello

[External](https://example.com/docs)

![Diagram](https://example.com/img/diagram.png)
`))
	if err != nil {
		t.Fatalf("AnalyzeMarkdownForDirectoryShare: %v", err)
	}
	if analysis.NeedsDirectoryShare {
		t.Fatal("expected absolute markdown targets not to require a directory share")
	}
	if analysis.HasEscapingTargets {
		t.Fatal("expected no escaping targets for absolute markdown links")
	}
}

func TestAnalyzeMarkdownForDirectoryShareIgnoresFrontmatter(t *testing.T) {
	t.Parallel()

	analysis, err := AnalyzeMarkdownForDirectoryShare([]byte(`---
summary: "[Guide](./guide.md)"
---
# Hello
`))
	if err != nil {
		t.Fatalf("AnalyzeMarkdownForDirectoryShare: %v", err)
	}
	if analysis.NeedsDirectoryShare {
		t.Fatal("expected frontmatter links to be ignored during directory-share analysis")
	}
	if analysis.HasEscapingTargets {
		t.Fatal("expected no escaping targets from stripped frontmatter")
	}
}

func TestHandlePreviewRendersLiveMarkdown(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docsDir, "img"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte(`# Hello

See [Guide](./guide.md#intro)

![Diagram](./img/diagram.png)
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/docs/readme.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if strings.Contains(body, "Loading markdown preview") {
		t.Fatalf("expected server-rendered markdown page, got %q", body)
	}
	if !strings.Contains(body, `class="markdown-body"`) {
		t.Fatalf("expected markdown body wrapper, got %q", body)
	}
	if !strings.Contains(body, `href="https://share.example.com/s/share-live/docs/guide.md?t=`) {
		t.Fatalf("expected rewritten markdown link, got %q", body)
	}
	if !strings.Contains(body, `src="https://share.example.com/r/share-live/docs/img/diagram.png?t=`) {
		t.Fatalf("expected rewritten markdown image, got %q", body)
	}
	if !strings.Contains(body, `class="breadcrumb"`) {
		t.Fatalf("expected breadcrumb navigation, got %q", body)
	}
	if !strings.Contains(body, `href="https://share.example.com/s/share-live/docs?t=`) {
		t.Fatalf("expected breadcrumb link to parent directory, got %q", body)
	}
}

func TestHandlePreviewRendersMarkdownMetadataAndFields(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	root := t.TempDir()
	source := `---
title: Ferry Metadata Test
summary: Shared over tailnet
status: draft
tags:
  - preview
  - docs
fields:
  owner: Alice
  team: infra
---
# Hello

` + "```go\nfmt.Println(\"hi\")\n```\n"
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live-meta",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/readme.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if strings.Contains(body, "title: Ferry Metadata Test") {
		t.Fatalf("unexpected raw frontmatter in response body: %q", body)
	}
	if !strings.Contains(body, `class="markdown-header"`) {
		t.Fatalf("expected markdown header, got %q", body)
	}
	if !strings.Contains(body, `class="action action-copy"`) ||
		!strings.Contains(body, `/r/share-live-meta/readme.md?t=`) {
		t.Fatalf("expected markdown preview to expose copy action, got %q", body)
	}
	for _, needle := range []string{
		`class="action block-copy-button"`,
		`data-copy-kind="code"`,
		`closest(".action-copy, .block-copy-button")`,
		`.markdown-body .block-copy-button`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected markdown preview to contain %q, got %q", needle, body)
		}
	}
	if !strings.Contains(body, `<h1 class="markdown-title">Ferry Metadata Test</h1>`) {
		t.Fatalf("expected Material-style title header, got %q", body)
	}
	if !strings.Contains(body, `<p class="markdown-summary">Shared over tailnet</p>`) {
		t.Fatalf("expected summary line, got %q", body)
	}
	if !strings.Contains(body, `class="markdown-tag">preview</span>`) ||
		!strings.Contains(body, `class="markdown-tag">docs</span>`) {
		t.Fatalf("expected tag pills, got %q", body)
	}
	if !strings.Contains(body, `class="markdown-fact-label">Status</span>`) ||
		!strings.Contains(body, `class="markdown-fact-value">draft</span>`) {
		t.Fatalf("expected compact metadata facts, got %q", body)
	}
	if !strings.Contains(body, `class="markdown-details"`) ||
		!strings.Contains(body, `>Owner</dt>`) ||
		!strings.Contains(body, `>Alice</dd>`) {
		t.Fatalf("expected compact details list, got %q", body)
	}
	if !strings.Contains(body, `<h1 id="hello"`) {
		t.Fatalf("expected markdown body heading, got %q", body)
	}
}

func TestHandlePreviewSkipsMarkdownTableCopyButtons(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "table.md"), []byte("| Name | Value |\n| --- | --- |\n| alpha | beta |\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live-table",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/table.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if strings.Contains(body, `data-copy-kind="table"`) {
		t.Fatalf("expected markdown table to omit block copy button, got %q", body)
	}
}

func TestHandlePreviewOmitsFilenameAsArticleTitleWithoutFrontmatterTitle(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plain.md"), []byte(`# Hello

No frontmatter here.
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live-plain",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/plain.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if !strings.Contains(body, "<title>plain</title>") {
		t.Fatalf("expected trimmed document title, got %q", body)
	}
	if strings.Contains(body, `<h1 class="markdown-title">plain.md</h1>`) {
		t.Fatalf("unexpected filename article title, got %q", body)
	}
	if !strings.Contains(body, `<h1 id="hello"`) {
		t.Fatalf("expected markdown body heading, got %q", body)
	}
}

func TestHandlePreviewUsesDescriptionWithoutFallbackFilenameTitle(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "desc.md"), []byte(`---
description: Description fallback works
tags: note
fields:
  owner: Alice
---
# Body Heading
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live-desc",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/desc.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if !strings.Contains(body, "<title>desc</title>") {
		t.Fatalf("expected trimmed document title, got %q", body)
	}
	if strings.Contains(body, `<h1 class="markdown-title">desc.md</h1>`) {
		t.Fatalf("unexpected filename article title, got %q", body)
	}
	if !strings.Contains(body, `<p class="markdown-summary">Description fallback works</p>`) {
		t.Fatalf("expected description summary, got %q", body)
	}
	if !strings.Contains(body, `class="markdown-tag">note</span>`) {
		t.Fatalf("expected tag pill, got %q", body)
	}
	if !strings.Contains(body, `>Owner</dt>`) || !strings.Contains(body, `>Alice</dd>`) {
		t.Fatalf("expected compact details list, got %q", body)
	}
	if !strings.Contains(body, `<h1 id="body-heading"`) {
		t.Fatalf("expected markdown body heading, got %q", body)
	}
}

func TestHandlePreviewRewritesEmbeddedServePreviewImages(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com/share"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte(`# Hello

![Before](https://share.example.com/share/s/embedded123?t=embedtoken)
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:         "share-live",
		SourcePath: root,
		IsDir:      true,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/readme.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}

	body := res.Body.String()
	if !strings.Contains(body, `src="https://share.example.com/share/r/embedded123?t=embedtoken"`) {
		t.Fatalf("expected embedded serve preview image to use raw route, got %q", body)
	}
}

func TestHandlePreviewRendersSnapshotMarkdown(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.externalBase = "https://share.example.com"

	sourceRoot := t.TempDir()
	snapshotRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(snapshotRoot, "readme.md"), []byte("# Snapshot copy"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Now().UTC()
	share := Share{
		ID:           "share-snapshot",
		SourcePath:   sourceRoot,
		IsDir:        true,
		Mode:         ModeSnapshot,
		SnapshotRoot: snapshotRoot,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	req := httptest.NewRequest("GET", "/s/"+share.ID+"/readme.md?t="+token, nil)
	res := httptest.NewRecorder()

	d.handlePreview(res, req)

	if res.Code != 200 {
		t.Fatalf("handlePreview status = %d, want 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), "Snapshot copy") {
		t.Fatalf("expected snapshot markdown content, got %q", res.Body.String())
	}
}

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()

	baseDir := t.TempDir()
	paths := StatePaths{
		BaseDir:      baseDir,
		DBPath:       filepath.Join(baseDir, "shares.db"),
		SecretPath:   filepath.Join(baseDir, "secret"),
		SnapshotsDir: filepath.Join(baseDir, "snapshots"),
		LogsDir:      filepath.Join(baseDir, "logs"),
	}

	d, err := NewDaemon(DaemonConfig{Paths: paths, AdminAddr: "127.0.0.1:0", PublicPort: 39124})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})
	return d
}
