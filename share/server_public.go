package share

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (d *Daemon) handlePublicHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Daemon) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	id, rel, ok := splitSharePath(r.URL.Path, "/s/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	share, token, ok := d.authorizeShare(w, r, id)
	if !ok {
		return
	}

	targetPath, info, err := d.resolveTarget(share, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "path_error", err.Error())
		return
	}

	breadcrumbs := d.buildBreadcrumbs(share, rel, token)

	if share.IsDir && info.IsDir() {
		html, err := d.renderDirectoryListing(share, targetPath, rel, token, breadcrumbs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "render_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
		_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
		return
	}

	baseName := filepath.Base(targetPath)
	kind := ClassifyPreviewKind(baseName)
	if kind == PreviewMarkdown {
		html, err := d.renderMarkdownPreview(share, targetPath, rel, token, breadcrumbs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "render_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
		_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
		return
	}
	if kind == PreviewHTML {
		rawURL := d.buildRawPath(share.ID, rel, token)
		html := RenderPreviewPage(baseName, kind, rawURL, breadcrumbs)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
		_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
		return
	}

	rawURL := d.buildRawPath(share.ID, rel, token)
	html := RenderPreviewPage(baseName, kind, rawURL, breadcrumbs)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
	_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
}

func (d *Daemon) handleRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	id, rel, ok := splitSharePath(r.URL.Path, "/r/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	share, _, ok := d.authorizeShare(w, r, id)
	if !ok {
		return
	}

	targetPath, info, err := d.resolveTarget(share, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "path_error", err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "invalid_target", "raw route requires a file")
		return
	}

	if ClassifyPreviewKind(filepath.Base(targetPath)) == PreviewHTML {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/octet-stream")
		if disposition := mime.FormatMediaType("attachment", map[string]string{
			"filename": filepath.Base(targetPath),
		}); disposition != "" {
			w.Header().Set("Content-Disposition", disposition)
		}
	}
	http.ServeFile(w, r, targetPath)
	_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
}

func (d *Daemon) authorizeShare(w http.ResponseWriter, r *http.Request, shareID string) (Share, string, bool) {
	share, err := d.store.GetShare(shareID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "share not found")
			return Share{}, "", false
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return Share{}, "", false
	}

	now := time.Now().UTC()
	if !share.IsActive(now) {
		writeError(w, http.StatusGone, "expired", "share is expired or revoked")
		return Share{}, "", false
	}

	token := strings.TrimSpace(r.URL.Query().Get("t"))
	if token == "" || !ValidateShareToken(d.secret, share.ID, token, d.cfg.TokenBytes) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
		return Share{}, "", false
	}

	return share, token, true
}

func (d *Daemon) resolveTarget(share Share, rel string) (string, os.FileInfo, error) {
	if !share.IsDir {
		if strings.TrimSpace(rel) != "" {
			return "", nil, errors.New("file share does not support nested paths")
		}
		path := share.SourcePath
		if share.Mode == ModeSnapshot && share.SnapshotRoot != "" {
			path = share.SnapshotRoot
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, err
		}
		return path, info, nil
	}

	base := share.SourcePath
	if share.Mode == ModeSnapshot && share.SnapshotRoot != "" {
		base = share.SnapshotRoot
	}
	resolved, err := ResolveScopedPath(base, rel)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, err
	}
	return resolved, info, nil
}

func (d *Daemon) renderDirectoryListing(share Share, dirPath string, rel string, token string, breadcrumbs []Breadcrumb) (string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("read dir: %w", err)
	}

	items := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		relChild := path.Join(rel, name)
		if rel == "" {
			relChild = name
		}

		fullPath := filepath.Join(dirPath, name)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		item := DirEntry{
			Name:       name,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			PreviewURL: d.buildPreviewPath(share.ID, relChild, token),
			RawURL:     d.buildRawPath(share.ID, relChild, token),
			CanCopy:    !info.IsDir() && canCopyContents(ClassifyPreviewKind(name)),
		}
		items = append(items, item)
	}

	title := filepath.Base(dirPath)
	if rel == "" {
		title = filepath.Base(share.SourcePath)
	}
	if title == "" || title == "." || title == "/" {
		title = "Directory"
	}
	return RenderDirectoryPage(title, items, breadcrumbs)
}

func (d *Daemon) renderMarkdownPreview(share Share, targetPath string, rel string, token string, breadcrumbs []Breadcrumb) (string, error) {
	source, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("read markdown: %w", err)
	}

	rendered, meta, err := RenderMarkdownDocument(source)
	if err != nil {
		return "", err
	}

	if share.IsDir {
		rendered, err = RewriteMarkdownLinks(rendered, rel, func(target string, isImage bool) string {
			if isImage {
				return d.buildRawPath(share.ID, target, token)
			}
			return d.buildPreviewPath(share.ID, target, token)
		})
		if err != nil {
			return "", err
		}
	}

	rendered, err = RewriteServePreviewImageSources(rendered, d.ExternalBaseURL())
	if err != nil {
		return "", err
	}

	rawURL := d.buildRawPath(share.ID, rel, token)
	return RenderMarkdownPreviewPage(filepath.Base(targetPath), rendered, rawURL, breadcrumbs, meta)
}

func (d *Daemon) buildPreviewPath(shareID string, rel string, token string) string {
	escapedRel := escapeRel(rel)
	baseURL := strings.TrimRight(d.ExternalBaseURL(), "/")
	if escapedRel == "" {
		path := fmt.Sprintf("/s/%s/?t=%s", shareID, url.QueryEscape(token))
		if baseURL == "" {
			return path
		}
		return baseURL + path
	}
	path := fmt.Sprintf("/s/%s/%s?t=%s", shareID, escapedRel, url.QueryEscape(token))
	if baseURL == "" {
		return path
	}
	return baseURL + path
}

func (d *Daemon) buildBreadcrumbs(share Share, rel string, token string) []Breadcrumb {
	if !share.IsDir || rel == "" {
		return nil
	}

	rootName := filepath.Base(share.SourcePath)
	if rootName == "" || rootName == "." || rootName == "/" {
		rootName = "Root"
	}

	parts := strings.Split(rel, "/")
	crumbs := make([]Breadcrumb, 0, len(parts))
	crumbs = append(crumbs, Breadcrumb{
		Name: rootName,
		URL:  d.buildPreviewPath(share.ID, "", token),
	})

	for i := 0; i < len(parts)-1; i++ {
		crumbs = append(crumbs, Breadcrumb{
			Name: parts[i],
			URL:  d.buildPreviewPath(share.ID, strings.Join(parts[:i+1], "/"), token),
		})
	}

	return crumbs
}

func (d *Daemon) buildRawPath(shareID string, rel string, token string) string {
	escapedRel := escapeRel(rel)
	baseURL := strings.TrimRight(d.ExternalBaseURL(), "/")
	if escapedRel == "" {
		path := fmt.Sprintf("/r/%s?t=%s", shareID, url.QueryEscape(token))
		if baseURL == "" {
			return path
		}
		return baseURL + path
	}
	path := fmt.Sprintf("/r/%s/%s?t=%s", shareID, escapedRel, url.QueryEscape(token))
	if baseURL == "" {
		return path
	}
	return baseURL + path
}
