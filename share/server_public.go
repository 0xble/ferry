package share

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
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
		html := RenderHTMLPreviewPage(baseName, rawURL, breadcrumbs)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
		_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
		return
	}
	if kind == PreviewPDF {
		http.Redirect(w, r, d.buildRawPath(share.ID, rel, token), http.StatusFound)
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

func (d *Daemon) handleHTMLArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	id, rel, ok := splitSharePath(r.URL.Path, "/h/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	token := strings.TrimSpace(r.URL.Query().Get("t"))
	if token != "" {
		share, ok := d.authorizeShareToken(w, id, token)
		if !ok {
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     htmlArtifactCookieName(share.ID),
			Value:    token,
			Path:     "/",
			Expires:  share.ExpiresAt,
			HttpOnly: true,
			Secure:   requestIsHTTPS(r),
			SameSite: http.SameSiteStrictMode,
		})
		// A relative Location preserves any reverse-proxy mount prefix that the
		// daemon cannot see after path stripping.
		w.Header().Set("Location", path.Base(r.URL.Path))
		w.WriteHeader(http.StatusSeeOther)
		return
	}

	cookie, err := r.Cookie(htmlArtifactCookieName(id))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
		return
	}
	share, ok := d.authorizeShareToken(w, id, cookie.Value)
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
	if info.IsDir() || ClassifyPreviewKind(filepath.Base(targetPath)) != PreviewHTML {
		writeError(w, http.StatusBadRequest, "invalid_target", "HTML artifact route requires an HTML file")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	file, err := os.Open(targetPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_error", err.Error())
		return
	}
	defer func() { _ = file.Close() }()
	currentInfo, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_error", err.Error())
		return
	}
	artifact, err := newHTMLArtifactReadSeeker(file, currentInfo.Size())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_error", err.Error())
		return
	}
	http.ServeContent(w, r, filepath.Base(targetPath), currentInfo.ModTime(), artifact)
	_ = d.store.TouchLastServed(share.ID, time.Now().UTC())
}

const htmlViewportContainmentStyle = `<style id="ferry-viewport-containment">html,body{width:100%!important;max-width:100%!important;overflow-x:hidden!important;overflow-x:clip!important;overscroll-behavior-x:none!important;touch-action:pan-y pinch-zoom!important}@layer ferry-viewport{html,body{width:100%!important;max-width:100%!important;overflow-x:hidden!important;overflow-x:clip!important;overscroll-behavior-x:none!important;touch-action:pan-y pinch-zoom!important}}</style>`

const htmlViewportContainmentScript = `<script id="ferry-viewport-containment-script">(()=>{const declarations={width:"100%",maxWidth:"100%",overflowX:CSS.supports("overflow-x","clip")?"clip":"hidden",overscrollBehaviorX:"none",touchAction:"pan-y pinch-zoom"};const apply=()=>{for(const node of [document.documentElement,document.body]){if(!node)continue;for(const [property,value] of Object.entries(declarations)){const cssProperty=property.replace(/[A-Z]/g,letter=>"-"+letter.toLowerCase());if(node.style.getPropertyValue(cssProperty)!==value||node.style.getPropertyPriority(cssProperty)!=="important")node.style.setProperty(cssProperty,value,"important")}}};new MutationObserver(apply).observe(document,{subtree:true,childList:true,attributes:true,attributeFilter:["style"]});apply();addEventListener("DOMContentLoaded",apply,{once:true})})()</script>`

const htmlViewportContainmentMarkup = htmlViewportContainmentStyle + htmlViewportContainmentScript

func newHTMLArtifactReadSeeker(source io.ReaderAt, sourceSize int64) (*io.SectionReader, error) {
	insertAt, err := htmlViewportGuardOffsetReader(source, sourceSize)
	if err != nil {
		return nil, err
	}
	reader := &injectedReaderAt{
		source:     source,
		sourceSize: sourceSize,
		insertAt:   insertAt,
		injection:  []byte(htmlViewportContainmentMarkup),
	}
	return io.NewSectionReader(reader, 0, sourceSize+int64(len(reader.injection))), nil
}

type injectedReaderAt struct {
	source     io.ReaderAt
	sourceSize int64
	insertAt   int64
	injection  []byte
}

func (r *injectedReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if offset < 0 {
		return 0, errors.New("negative read offset")
	}
	totalSize := r.sourceSize + int64(len(r.injection))
	if offset >= totalSize {
		return 0, io.EOF
	}

	written := 0
	for len(p) > 0 && offset < totalSize {
		switch {
		case offset < r.insertAt:
			available := min(int64(len(p)), r.insertAt-offset)
			n, err := r.source.ReadAt(p[:available], offset)
			offset += int64(n)
			written += n
			p = p[n:]
			if err != nil {
				return written, err
			}
		case offset < r.insertAt+int64(len(r.injection)):
			injectionOffset := offset - r.insertAt
			n := copy(p, r.injection[injectionOffset:])
			offset += int64(n)
			written += n
			p = p[n:]
		default:
			sourcePosition := offset - int64(len(r.injection))
			available := min(int64(len(p)), r.sourceSize-sourcePosition)
			n, err := r.source.ReadAt(p[:available], sourcePosition)
			offset += int64(n)
			written += n
			p = p[n:]
			if err != nil {
				return written, err
			}
		}
	}
	if len(p) > 0 {
		return written, io.EOF
	}
	return written, nil
}

func htmlViewportGuardOffset(source []byte) int {
	offset, err := htmlViewportGuardOffsetReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return 0
	}
	return int(offset)
}

func htmlViewportGuardOffsetReader(source io.ReaderAt, sourceSize int64) (int64, error) {
	reader := bufio.NewReader(io.NewSectionReader(source, 0, sourceSize))
	offset := int64(0)
	if prefix, _ := reader.Peek(3); bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = reader.Discard(3)
		offset += 3
	}

	for {
		for {
			prefix, err := reader.Peek(1)
			if errors.Is(err, io.EOF) {
				return 0, nil
			}
			if err != nil {
				return 0, err
			}
			if !strings.ContainsRune(" 	\r\n\f", rune(prefix[0])) {
				break
			}
			_, _ = reader.Discard(1)
			offset++
		}

		prefix, _ := reader.Peek(4)
		if !bytes.Equal(prefix, []byte("<!--")) {
			break
		}
		_, _ = reader.Discard(4)
		offset += 4
		matched := 0
		for matched < 3 {
			value, err := reader.ReadByte()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return 0, nil
				}
				return 0, err
			}
			offset++
			if value == '-' {
				if matched < 2 {
					matched++
				}
				continue
			}
			if value == '>' && matched == 2 {
				matched = 3
			} else {
				matched = 0
			}
		}
	}

	prefix, _ := reader.Peek(len("<!doctype"))
	if !bytes.EqualFold(prefix, []byte("<!doctype")) {
		return 0, nil
	}
	for {
		value, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, nil
			}
			return 0, err
		}
		offset++
		if value == '>' {
			return offset, nil
		}
	}
}

func (d *Daemon) authorizeShare(w http.ResponseWriter, r *http.Request, shareID string) (Share, string, bool) {
	token := strings.TrimSpace(r.URL.Query().Get("t"))
	share, ok := d.authorizeShareToken(w, shareID, token)
	if !ok {
		return Share{}, "", false
	}
	return share, token, true
}

func (d *Daemon) authorizeShareToken(w http.ResponseWriter, shareID string, token string) (Share, bool) {
	share, err := d.store.GetShare(shareID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "share not found")
			return Share{}, false
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return Share{}, false
	}

	now := time.Now().UTC()
	if !share.IsActive(now) {
		writeError(w, http.StatusGone, "expired", "share is expired or revoked")
		return Share{}, false
	}

	if token == "" || !ValidateShareToken(d.secret, share.ID, token, d.cfg.TokenBytes) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
		return Share{}, false
	}

	return share, true
}

func htmlArtifactCookieName(shareID string) string {
	return "ferry_html_" + shareID
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
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
	query := previewQuery(token, rel != "" && ClassifyPreviewKind(path.Base(rel)) == PreviewPDF)
	if escapedRel == "" {
		path := fmt.Sprintf("/s/%s/?%s", shareID, query)
		if baseURL == "" {
			return path
		}
		return baseURL + path
	}
	path := fmt.Sprintf("/s/%s/%s?%s", shareID, escapedRel, query)
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
