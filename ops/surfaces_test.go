package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/ferry/share"
)

func httpCall(t *testing.T, w *world, auth op.Authorizer, name string, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	toolkit.Handler(w.registry(), auth).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ops/"+name, bytes.NewReader(b)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func errCode(v map[string]any) string {
	e, _ := v["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

// TestWritesNeedApplyOnEverySurface checks that publish, renew and unshare
// change nothing without apply on the CLI (--dry-run) and HTTP, that served
// HTTP refuses to apply them by default, and that MCP does not offer them.
func TestWritesNeedApplyOnEverySurface(t *testing.T) {
	w := newWorld(t)
	doc := w.file("doc.md", "# Doc\n")
	w.daemon.Seed(t, seededID, doc, share.ModeLive, false, time.Hour)
	before := w.daemon.Shares(t)

	for _, args := range [][]string{
		{"publish", doc, "--dry-run", "--json"},
		{"renew", seededID, "--dry-run", "--json"},
		{"unshare", seededID, "--dry-run", "--json"},
		{"unshare", doc, "--dry-run", "--json"},
	} {
		code, stdout, stderr := w.run(args...)
		if code != 0 || !strings.Contains(stdout, `"preview"`) {
			t.Errorf("%v: exit %d, stdout %s, stderr %s; want a preview", args, code, stdout, stderr)
		}
	}
	for name, in := range map[string]map[string]any{
		"share.publish": {},
		"share.renew":   {"id": seededID},
		"share.unshare": {"target": seededID},
	} {
		if status, body := httpCall(t, w, op.AllowAll, name, in); status != 200 || body["preview"] == nil {
			t.Errorf("HTTP %s without apply: %d %v; want a 200 preview", name, status, body)
		}
		applied := map[string]any{"apply": true}
		for k, v := range in {
			applied[k] = v
		}
		if status, body := httpCall(t, w, nil, name, applied); status != 403 || errCode(body) != "write_not_authorized" {
			t.Errorf("HTTP %s apply under the served default: %d %v; want 403 write_not_authorized", name, status, body)
		}
	}
	if status, body := httpCall(t, w, op.AllowAll, "share.publish", map[string]any{"path": doc, "apply": true}); status != 400 || errCode(body) != "cli_only" {
		t.Errorf("HTTP publish with a path: %d %v; want 400 cli_only", status, body)
	}
	if status, body := httpCall(t, w, op.AllowAll, "share.publish", map[string]any{"open": "laptop", "apply": true}); status != 400 || errCode(body) != "cli_only" {
		t.Errorf("HTTP publish with --open: %d %v; want 400 cli_only", status, body)
	}
	if status, body := httpCall(t, w, op.AllowAll, "share.publish", map[string]any{"apply": true}); status != 400 || errCode(body) != "invalid_args" {
		t.Errorf("HTTP publish applied without a path: %d %v; want 400 invalid_args", status, body)
	}

	cs := toolkittest.MCPClient(t, w.registry(), op.AllowAll)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"doctor", "share_get", "shares_list"}; !reflect.DeepEqual(names, want) {
		t.Errorf("MCP tools %v, want only the reads %v", names, want)
	}
	for _, name := range []string{"share_publish", "share_renew", "share_unshare"} {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{"apply": true, "target": seededID, "id": seededID}})
		if err == nil && !res.IsError {
			t.Errorf("MCP %s was callable", name)
		}
	}

	if after := w.daemon.Shares(t); !reflect.DeepEqual(after, before) {
		t.Errorf("state changed without an applied CLI call:\nbefore %v\nafter  %v", before, after)
	}
}

// TestPreviewNeverStartsTheDaemon checks that --dry-run reads only from a
// daemon that is already up: publish still plans offline, renew and unshare
// report the daemon as down, and nothing calls EnsureDaemon.
func TestPreviewNeverStartsTheDaemon(t *testing.T) {
	w := newWorld(t)
	doc := w.file("doc.md", "# Doc\n")
	w.down = true
	started := 0
	w.b.EnsureDaemon = func(*share.Client) error { started++; return fmt.Errorf("must not start") }

	code, stdout, _ := w.run("publish", doc, "--dry-run", "--json")
	var got Published
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil || got.Preview == nil {
		t.Fatalf("publish --dry-run with the daemon down: exit %d, %s", code, stdout)
	}
	want := PublishPreview{Action: "create", Path: doc, SharePath: doc, Mode: "live", ExpiresInSeconds: 604800,
		Note: "ferryd is not running, so an existing live share was not checked; publish starts the daemon"}
	if !reflect.DeepEqual(*got.Preview, want) || got.ShareResponse != nil {
		t.Errorf("preview %+v, want %+v", *got.Preview, want)
	}
	for _, args := range [][]string{{"renew", "x", "--dry-run", "--json"}, {"unshare", "x", "--dry-run", "--json"}} {
		code, _, stderr := w.run(args...)
		if code != 1 || !strings.Contains(stderr, `"daemon_unavailable"`) {
			t.Errorf("%v: exit %d, %s; want daemon_unavailable", args, code, stderr)
		}
	}
	if started != 0 {
		t.Errorf("a preview called EnsureDaemon %d times", started)
	}
	if code, _, stderr := w.run("list", "--json"); code != 1 || !strings.Contains(stderr, "must not start") || started != 1 {
		t.Errorf("list with the daemon down: exit %d, %s, started %d; want the EnsureDaemon error", code, stderr, started)
	}
}

func TestPublishReusesTheLiveShareOfAPath(t *testing.T) {
	w := newWorld(t)
	doc := w.file("report.txt", "report\n")

	var first, second, snap share.ShareResponse
	for _, c := range []struct {
		args []string
		out  *share.ShareResponse
	}{
		{[]string{"publish", doc, "--json", "--expires-in", "1h"}, &first},
		{[]string{"publish", doc, "--json"}, &second},
		{[]string{"publish", "--snapshot", doc, "--json"}, &snap},
	} {
		code, stdout, stderr := w.run(c.args...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", c.args, code, stderr)
		}
		if err := json.Unmarshal([]byte(stdout), c.out); err != nil {
			t.Fatal(err)
		}
	}
	if second.ID != first.ID || !second.ExpiresAt.After(first.ExpiresAt) {
		t.Errorf("second live publish %s expiring %s, want a renewal of %s past %s", second.ID, second.ExpiresAt, first.ID, first.ExpiresAt)
	}
	if snap.ID == first.ID || snap.Mode != share.ModeSnapshot {
		t.Errorf("snapshot publish %+v, want a new snapshot share", snap)
	}
	if n := len(w.daemon.Shares(t)); n != 2 {
		t.Errorf("%d shares, want 2", n)
	}
	code, stdout, _ := w.run("publish", doc, "--dry-run", "--json")
	var p Published
	if err := json.Unmarshal([]byte(stdout), &p); code != 0 || err != nil || p.Preview.Action != "renew" || p.Preview.ShareID != first.ID {
		t.Errorf("publish --dry-run of a live path: exit %d, %s; want renew of %s", code, stdout, first.ID)
	}
}

func TestPublishMarkdownBundle(t *testing.T) {
	w := newWorld(t)
	doc := w.file("plan.md", "# Plan\n\n![Diagram](./img/diagram.png)\n")
	w.file("img/diagram.png", "png")

	code, stdout, stderr := w.run("publish", doc)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"kind: directory\n", "path: " + doc + "\n", "bundle_root: " + w.dir + "\n", "/plan.md?t="} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	shares := w.daemon.Shares(t)
	if len(shares) != 1 || shares[0].Path != w.dir {
		t.Errorf("shares %v, want one share of %s", shares, w.dir)
	}
}

// TestUnsharePreservesRevokeFailureInsteadOfReportingNotFound is ported from
// cmd/ferry: a revoke that fails after the daemon mutated must keep the
// daemon's error, not become exit 3 not found.
func TestUnsharePreservesRevokeFailureInsteadOfReportingNotFound(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":%q}`, server.URL)
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/admin/shares/failing-id":
			if r.Method != http.MethodDelete {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":{"code":"store_error","message":"revoke response failed after mutation"}}`)
		case "/admin/shares":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	w := newWorld(t)
	w.b.Client = func() *share.Client { return share.NewClient(server.URL) }
	code, _, stderr := w.run("unshare", "failing-id", "--json")
	if code != 1 || !strings.Contains(stderr, `"code":"store_error"`) {
		t.Fatalf("exit %d, %s; want exit 1 store_error, not 3 not_found", code, stderr)
	}
}

func TestUnshareByPathAndNotFound(t *testing.T) {
	w := newWorld(t)
	doc := w.file("a.txt", "a\n")
	w.daemon.Create(t, doc, share.ModeLive, time.Hour)
	w.daemon.Create(t, doc, share.ModeSnapshot, time.Hour)

	code, stdout, _ := w.run("unshare", "--json", "--", doc)
	if code != 0 || strings.TrimSpace(stdout) != fmt.Sprintf("{\n  \"ok\": true,\n  \"path\": %q,\n  \"revoked\": 2\n}", doc) {
		t.Errorf("unshare by path: exit %d, %s", code, stdout)
	}
	code, _, stderr := w.run("unshare", "--json", "--", doc)
	if code != 3 || !strings.Contains(stderr, `"code":"not_found"`) {
		t.Errorf("unshare of a path with no active share: exit %d, %s; want 3 not_found", code, stderr)
	}
}

func TestPublishOpenPassesURLAsOneArgument(t *testing.T) {
	w := newWorld(t)
	doc := w.file("o'hare.txt", "x\n")
	code, stdout, stderr := w.run("publish", doc, "--open", "laptop", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var s share.ShareResponse
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"ssh", "laptop", "open", s.URL}}; !reflect.DeepEqual(w.ssh, want) {
		t.Errorf("ran %q, want %q", w.ssh, want)
	}
}

func TestPublishOpenRejectsHostStartingWithDashBeforePublishing(t *testing.T) {
	w := newWorld(t)
	doc := w.file("a.txt", "x\n")
	code, _, stderr := w.run("publish", doc, "--open=-oProxyCommand=evil", "--json")
	if code != 2 || !strings.Contains(stderr, "ssh option injection") {
		t.Errorf("exit %d, %s; want exit 2 invalid_args", code, stderr)
	}
	if len(w.ssh) != 0 || len(w.daemon.Shares(t)) != 0 {
		t.Errorf("ssh ran %v and %d shares exist; want neither", w.ssh, len(w.daemon.Shares(t)))
	}
}

func TestPublishOpenFailureStillPrintsTheShare(t *testing.T) {
	w := newWorld(t)
	doc := w.file("a.txt", "x\n")
	w.b.Command = func(string, ...string) *exec.Cmd { return exec.Command("false") }
	code, stdout, stderr := w.run("publish", doc, "--open", "laptop", "--json")
	if code != 1 || !strings.Contains(stdout, `"url"`) || !strings.Contains(stderr, `"open_failed"`) {
		t.Errorf("exit %d, stdout %s, stderr %s; want the share and open_failed", code, stdout, stderr)
	}
}

func TestDoctor(t *testing.T) {
	w := newWorld(t)
	code, stdout, _ := w.run("doctor")
	if code != 0 || stdout != "tailscale: ok\ndaemon: ok\n" {
		t.Errorf("healthy doctor: exit %d, %q", code, stdout)
	}
	w.tailscaleErr = fmt.Errorf("tailscale ip -4: not running")
	w.down = true
	code, stdout, stderr := w.run("doctor", "--json")
	var r DoctorReport
	if err := json.Unmarshal([]byte(stdout), &r); err != nil || code != 1 || r.TailscaleOK || r.DaemonOK ||
		r.TailscaleError != "tailscale ip -4: not running; tailscale ip -4: not running" || !strings.Contains(stderr, `"health_check_failed"`) {
		t.Errorf("failing doctor --json: exit %d, %s, %s", code, stdout, stderr)
	}
}

func TestDurationsAreValidated(t *testing.T) {
	w := newWorld(t)
	doc := w.file("a.txt", "x\n")
	for args, want := range map[string]string{
		"publish|" + doc + "|--expires-in=0|--json":   `"message":"--expires-in must be greater than zero"`,
		"publish|" + doc + "|--expires-in=abc|--json": `--expires-in: expected duration but got \"abc\": time: invalid duration \"abc\"`,
		"renew|x|--for=-1h|--json":                    `"message":"--for must be greater than zero"`,
	} {
		code, _, stderr := w.run(strings.Split(args, "|")...)
		if code != 2 || !strings.Contains(stderr, want) {
			t.Errorf("%s: exit %d, %s; want exit 2 and %s", args, code, stderr, want)
		}
	}
}

func TestImplicitPublish(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := newWorld(t).registry()
	for _, c := range []struct{ in, want []string }{
		{[]string{doc, "--json"}, []string{"publish", doc, "--json"}},
		{[]string{"--json", doc}, []string{"--json", "publish", doc}},
		{[]string{"-j", "--snapshot", doc}, []string{"-j", "--snapshot", "publish", doc}},
		{[]string{"--fields", "url", doc}, []string{"--fields", "url", "publish", doc}},
		{[]string{"list", "--json"}, []string{"list", "--json"}},
		{[]string{"get", "--json", "--", "-abc"}, []string{"get", "--json", "--", "-abc"}},
		{[]string{"metadata", "--json"}, []string{"metadata", "--json"}},
		{[]string{"serve", "--socket", "/tmp/x"}, []string{"serve", "--socket", "/tmp/x"}},
		{[]string{"no-such-file-or-command"}, []string{"no-such-file-or-command"}},
		{[]string{"--help"}, []string{"--help"}},
		{nil, nil},
	} {
		if got := ImplicitPublish(reg, c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ImplicitPublish(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortVersionFlag(t *testing.T) {
	w := newWorld(t)
	for _, flag := range []string{"-V", "--version"} {
		if code, stdout, _ := w.run(flag); code != 0 || stdout != "test\n" {
			t.Errorf("%s: exit %d, %q", flag, code, stdout)
		}
	}
}
