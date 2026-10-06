package ops

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xble/toolkit/op"

	"github.com/0xble/ferry/share"
)

// PublishInput's path and open are CLI-only: a served caller must neither
// expose an arbitrary host path on the tailnet nor make the host run ssh.
type PublishInput struct {
	Path      string `json:"path,omitempty" arg:"" help:"File or directory path to publish" toolkit:"cli-only"`
	Snapshot  bool   `json:"snapshot,omitempty" help:"Snapshot content instead of live mode"`
	ExpiresIn string `json:"expires_in,omitempty" name:"expires-in" default:"168h" help:"Share lifetime (default: 7d)"`
	Open      string `json:"open,omitempty" help:"Open URL on a remote machine via SSH (e.g. laptop)" toolkit:"cli-only"`
}

// Published is the share publish created or renewed, with its URL pointing
// at the published file. A preview has no share, only preview.
type Published struct {
	*share.ShareResponse
	Preview *PublishPreview `json:"preview,omitempty"`

	// requestedPath and bundleRoot are for the human summary only.
	requestedPath string
	bundleRoot    string
}

// PublishPreview is what publish would do. It is empty when there is no
// path to plan, as on HTTP, where path is not accepted.
type PublishPreview struct {
	// Action is create, or renew for an active live share of the same path.
	Action    string `json:"action,omitempty"`
	Path      string `json:"path,omitempty"`
	SharePath string `json:"share_path,omitempty"`
	// Entry is the file a Markdown bundle opens, relative to SharePath.
	Entry            string `json:"entry,omitempty"`
	IsDir            bool   `json:"is_dir,omitempty"`
	Mode             string `json:"mode,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	// ShareID is the live share a renew would extend.
	ShareID  string `json:"share_id,omitempty"`
	DaemonOK bool   `json:"daemon_ok"`
	Note     string `json:"note,omitempty"`
}

type publishPlan struct {
	RequestedPath string
	SharePath     string
	EntryRel      string
	IsDir         bool
}

func registerPublish(reg *op.Registry, b *Backend) {
	op.Add(reg, op.Op[PublishInput, Published]{
		Name: "share.publish", CLI: "publish", Summary: "Publish a file or directory", Effect: op.Write, CLIImmediate: true,
		Handler: b.publish,
		Render:  renderPublished,
	})
}

func (b *Backend) publish(ctx context.Context, req op.Request, in PublishInput) (Published, error) {
	target := strings.TrimSpace(in.Path)
	if target == "" {
		if !req.Apply {
			return Published{Preview: &PublishPreview{Note: "no path to publish: path is accepted only on the command line"}}, nil
		}
		return Published{}, invalidArgs("file or directory path is required")
	}
	ttl, err := lifetime("--expires-in", in.ExpiresIn)
	if err != nil {
		return Published{}, err
	}
	host := strings.TrimSpace(in.Open)
	if strings.HasPrefix(host, "-") {
		return Published{}, invalidArgs("--open host must not start with '-' (ssh option injection)")
	}
	mode := share.ModeLive
	if in.Snapshot {
		mode = share.ModeSnapshot
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		return Published{}, err
	}
	plan, err := resolvePublishPlan(absPath)
	if err != nil {
		return Published{}, err
	}

	c := b.Client()
	if !req.Apply {
		return previewPublish(c, plan, mode, ttl)
	}
	if err := b.ensure(c); err != nil {
		return Published{}, err
	}

	var out share.ShareResponse
	existing, ok := share.ShareResponse{}, false
	if mode == share.ModeLive {
		if existing, ok, err = findExistingLiveShare(c, plan.SharePath); err != nil {
			return Published{}, daemonError(err)
		}
	}
	if ok {
		out, err = c.RenewShare(existing.ID, ttl)
	} else {
		out, err = c.CreateShare(share.CreateShareRequest{
			Path:             plan.SharePath,
			Mode:             mode,
			ExpiresInSeconds: int64(ttl / time.Second),
		})
	}
	if err != nil {
		return Published{}, daemonError(err)
	}
	if out.URL, err = resolvePublishURL(out.URL, plan.EntryRel); err != nil {
		return Published{}, err
	}

	res := Published{ShareResponse: &out, requestedPath: plan.RequestedPath, bundleRoot: publishBundleRoot(plan, out.Path)}
	if host != "" {
		if err := b.openOnRemote(host, out.URL); err != nil {
			return res, &op.Error{Kind: op.KindError, Code: "open_failed", Message: fmt.Sprintf("ssh %s open: %v", host, err), Result: res}
		}
	}
	return res, nil
}

func previewPublish(c *share.Client, plan publishPlan, mode string, ttl time.Duration) (Published, error) {
	p := &PublishPreview{Action: "create", Path: plan.RequestedPath, SharePath: plan.SharePath, Entry: plan.EntryRel,
		IsDir: plan.IsDir, Mode: mode, ExpiresInSeconds: int64(ttl / time.Second), DaemonOK: c.Health() == nil}
	switch {
	case mode != share.ModeLive:
	case !p.DaemonOK:
		p.Note = "ferryd is not running, so an existing live share was not checked; publish starts the daemon"
	default:
		existing, ok, err := findExistingLiveShare(c, plan.SharePath)
		if err != nil {
			return Published{}, daemonError(err)
		}
		if ok {
			p.Action, p.ShareID = "renew", existing.ID
		}
	}
	return Published{Preview: p, requestedPath: plan.RequestedPath, bundleRoot: publishBundleRoot(plan, plan.SharePath)}, nil
}

func renderPublished(w io.Writer, p Published) error {
	if p.Preview == nil {
		_, err := fmt.Fprintln(w, formatShareText(*p.ShareResponse, shareTextOptions{Path: p.requestedPath, BundleRoot: p.bundleRoot}))
		return err
	}
	v := p.Preview
	if v.Action == "" {
		_, err := fmt.Fprintf(w, "preview: %s\n", v.Note)
		return err
	}
	head := fmt.Sprintf("preview: publish would create a %s share (nothing changed)", v.Mode)
	if v.Action == "renew" {
		head = fmt.Sprintf("preview: publish would renew live share %s (nothing changed)", v.ShareID)
	}
	lines := []string{head, "kind: " + shareKind(v.IsDir), "mode: " + v.Mode, "path: " + v.Path}
	if p.bundleRoot != "" {
		lines = append(lines, "bundle_root: "+p.bundleRoot)
	}
	lines = append(lines, "expires_in: "+(time.Duration(v.ExpiresInSeconds)*time.Second).String())
	if v.Note != "" {
		lines = append(lines, "note: "+v.Note)
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func publishBundleRoot(plan publishPlan, sharePath string) string {
	if plan.EntryRel == "" {
		return ""
	}
	return sharePath
}

func resolvePublishPlan(absPath string) (publishPlan, error) {
	plan := publishPlan{
		RequestedPath: absPath,
		SharePath:     absPath,
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return publishPlan{}, &op.Error{Kind: op.KindNotFound, Code: "not_found", Message: err.Error()}
		}
		return publishPlan{}, err
	}
	plan.IsDir = info.IsDir()
	if info.IsDir() || !isMarkdownPath(absPath) {
		return plan, nil
	}

	source, err := os.ReadFile(absPath)
	if err != nil {
		return publishPlan{}, err
	}
	analysis, err := share.AnalyzeMarkdownForDirectoryShare(source)
	if err != nil {
		return publishPlan{}, err
	}
	if !analysis.NeedsDirectoryShare {
		return plan, nil
	}
	if analysis.HasEscapingTargets {
		return publishPlan{}, invalidArgs("markdown file references assets outside its directory; publish the parent directory explicitly")
	}

	plan.SharePath = filepath.Dir(absPath)
	plan.EntryRel = filepath.Base(absPath)
	plan.IsDir = true
	return plan, nil
}

func isMarkdownPath(target string) bool {
	return share.IsMarkdownPreviewName(filepath.Base(target))
}

func resolvePublishURL(baseURL string, entryRel string) (string, error) {
	if strings.TrimSpace(entryRel) == "" {
		return baseURL, nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	joinedPath := strings.TrimRight(parsed.Path, "/")
	for _, segment := range strings.Split(entryRel, "/") {
		if segment == "" {
			continue
		}
		joinedPath += "/" + url.PathEscape(segment)
	}
	parsed.Path = path.Clean(joinedPath)
	return parsed.String(), nil
}

// openOnRemote runs `ssh host open url`, passing the URL as one argument.
// The host was checked not to start with '-' before anything was published.
func (b *Backend) openOnRemote(host, url string) error {
	cmd := b.Command("ssh", host, "open", url)
	cmd.Stdout = b.CommandOutput
	cmd.Stderr = b.CommandOutput
	return cmd.Run()
}

func findExistingLiveShare(c *share.Client, absPath string) (share.ShareResponse, bool, error) {
	shares, err := c.ListShares()
	if err != nil {
		return share.ShareResponse{}, false, err
	}
	existing, ok := findExistingLiveShareIn(shares, absPath)
	return existing, ok, nil
}

func findExistingLiveShareIn(shares []share.ShareResponse, absPath string) (share.ShareResponse, bool) {
	for _, existing := range shares {
		if existing.Mode != share.ModeLive {
			continue
		}
		if existing.Path != absPath {
			continue
		}
		return existing, true
	}
	return share.ShareResponse{}, false
}
