// Package compat_test replays every known caller invocation of ferry against
// a real in-process daemon and compares the result with golden output
// recorded from the pre-toolkit binary (origin/main 6267d4d,
// v2.1.1-31-g6267d4d).
//
// For each case it compares the exit code, the admin API requests the daemon
// answered (method and path), the stdout JSON (or text) and the stderr error
// envelope (or text). Share IDs, tokens, times and the sandbox path are
// normalised.
//
// Re-record with internal/compat/record.sh, which builds the old client and
// runs `go test ./internal/compat -run TestCallers -args -record <old-binary>`.
package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/cli"

	"github.com/0xble/ferry/internal/ferrytest"
	"github.com/0xble/ferry/ops"
	"github.com/0xble/ferry/share"
)

var record = flag.String("record", "", "path to the pre-toolkit ferry binary; rewrite the goldens from it")

// TestMain pins local time, which the human share text prints, and points
// HOME and PATH at a scratch directory before any test runs.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	dir, err := os.MkdirTemp("", "ferry-compat-test-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME": dir, "XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "XDG_DATA_HOME": dir, "XDG_CACHE_HOME": dir,
		"PATH": "/usr/bin:/bin", "FERRY_ADMIN_ADDR": "", "FERRY_LAUNCH_AGENT_LABEL": "",
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// scenario is one case's sandbox: a home with files to publish under work/,
// a fake tailscale, and a daemon.
type scenario struct {
	t      *testing.T
	home   string
	work   string
	daemon *ferrytest.Daemon
	ids    []string
}

func (s *scenario) path(rel string) string { return filepath.Join(s.work, rel) }

// share seeds an active share for setup, with a fixed ID that cannot be
// mistaken for a flag, and remembers it for {id}.
func (s *scenario) share(rel, mode string) {
	id := fmt.Sprintf("fixture%04d", len(s.ids)+1)
	s.daemon.Seed(s.t, id, s.path(rel), mode, false, 24*time.Hour)
	s.ids = append(s.ids, id)
}

type tcase struct {
	name string
	// caller names who runs this invocation; see docs/compatibility.md.
	caller string
	// args may hold {work} (the files to publish) and {id} (the first share
	// setup created).
	args  []string
	setup func(*scenario)
	// down makes the daemon unreachable: the admin address is the default
	// socket in the sandbox home, which does not exist.
	down bool
	// noTailscale leaves tailscale off PATH.
	noTailscale bool
	// change, when set, is a documented intentional difference: stderr is
	// not compared, while the exit code, the requests and stdout still are.
	change string
	// stdoutChanged also leaves stdout uncompared, for parse errors, where
	// the old binary printed kong's usage text.
	stdoutChanged bool
	// exit, when set, is the documented new exit code.
	exit int
	// fewerRequests marks a documented change where the new binary refuses
	// before a daemon request the old one made.
	fewerRequests bool
}

var (
	two     = func(s *scenario) { s.share("notes.txt", share.ModeLive); s.share("report.md", share.ModeSnapshot) }
	one     = func(s *scenario) { s.share("notes.txt", share.ModeLive) }
	twoSame = func(s *scenario) { s.share("notes.txt", share.ModeLive); s.share("notes.txt", share.ModeSnapshot) }
	cases   = []tcase{
		// The show-me skill (references/ferry.md).
		{name: "skill-doctor", caller: "show-me skill", args: []string{"doctor"}},
		{name: "skill-doctor-json", caller: "show-me skill", args: []string{"doctor", "--json"}},
		{name: "skill-doctor-down", caller: "show-me skill", args: []string{"doctor"}, down: true, noTailscale: true,
			change: "C2: the error line is `error: ferry doctor detected issues` text instead of a JSON envelope on a pipe; stdout and exit 1 are unchanged"},
		{name: "skill-doctor-json-down", caller: "show-me skill", args: []string{"doctor", "--json"}, down: true, noTailscale: true, exit: 1,
			change: "C3: doctor --json exits 1 with health_check_failed on stderr when a check fails; the report on stdout is unchanged"},
		{name: "skill-publish", caller: "show-me skill", args: []string{"publish", "--json", "{work}/report.md"}},
		{name: "skill-publish-snapshot", caller: "show-me skill", args: []string{"publish", "--snapshot", "--json", "{work}/notes.txt"}},
		{name: "skill-publish-expires", caller: "show-me skill", args: []string{"publish", "--expires-in=24h", "--json", "{work}/notes.txt"}},
		{name: "skill-publish-reuses-live", caller: "show-me skill", args: []string{"publish", "--json", "{work}/notes.txt"}, setup: one},
		{name: "skill-publish-bundle", caller: "show-me skill", args: []string{"publish", "--json", "{work}/bundle/plan.md"}},
		{name: "skill-publish-directory", caller: "show-me skill", args: []string{"publish", "--json", "{work}/docs"}},
		{name: "skill-list", caller: "show-me skill", args: []string{"list", "--json"}, setup: two},
		{name: "skill-list-empty", caller: "show-me skill", args: []string{"list", "--json"}},
		{name: "skill-get", caller: "show-me skill", args: []string{"get", "--json", "--", "{id}"}, setup: one},
		{name: "skill-renew", caller: "show-me skill", args: []string{"renew", "--for=168h", "--json", "--", "{id}"}, setup: one},
		{name: "skill-unshare-id", caller: "show-me skill", args: []string{"unshare", "--json", "--", "{id}"}, setup: one},
		{name: "skill-unshare-path", caller: "show-me skill", args: []string{"unshare", "--json", "--", "{work}/notes.txt"}, setup: twoSame},

		// Hermes cron LPG Weekly Business Review: flags after the path.
		{name: "wbr-meeting-doc", caller: "LPG Weekly Business Review", args: []string{"publish", "{work}/wbr/meeting-prep.md", "--json"}},
		{name: "wbr-html", caller: "LPG Weekly Business Review", args: []string{"publish", "{work}/wbr/index.html", "--json"}},
		{name: "wbr-pdf", caller: "LPG Weekly Business Review", args: []string{"publish", "{work}/wbr/weekly-business-review-2026-W40.pdf", "--json"}},

		// The README's human forms.
		{name: "readme-publish", caller: "README", args: []string{"publish", "{work}/report.md"}},
		{name: "readme-publish-bundle", caller: "README", args: []string{"publish", "{work}/bundle/plan.md"}},
		{name: "readme-implicit-publish", caller: "README", args: []string{"{work}/report.md", "--json"}},
		{name: "readme-list", caller: "README", args: []string{"list"}, setup: two},
		{name: "readme-list-empty", caller: "README", args: []string{"list"}},
		{name: "readme-get", caller: "README", args: []string{"get", "{id}"}, setup: one},
		{name: "readme-renew", caller: "README", args: []string{"renew", "{id}"}, setup: one},
		{name: "readme-unshare-id", caller: "README", args: []string{"unshare", "{id}"}, setup: one},
		{name: "readme-unshare-path", caller: "README", args: []string{"unshare", "{work}/notes.txt"}, setup: twoSame},
		{name: "readme-version", caller: "README", args: []string{"--version"}},
		{name: "readme-version-short", caller: "README", args: []string{"-V"}},

		// Error paths.
		{name: "err-expires-zero", caller: "publish", args: []string{"publish", "--expires-in=0", "--json", "{work}/notes.txt"}},
		{name: "err-for-zero", caller: "renew", args: []string{"renew", "--for=0", "--json", "{id}"}, setup: one, fewerRequests: true},
		{name: "err-markdown-escape", caller: "publish", args: []string{"publish", "--json", "{work}/escape/plan.md"}, fewerRequests: true},
		{name: "err-unshare-unmatched", caller: "unshare", args: []string{"unshare", "--json", "--", "nope"}},
		{name: "err-get-missing", caller: "get", args: []string{"get", "--json", "--", "nope"}, exit: 3,
			change: "C2, C5: a daemon 404 is the not_found envelope, exit 3, instead of `not_found: share not found` text, exit 1"},
		{name: "err-renew-missing", caller: "renew", args: []string{"renew", "--json", "--", "nope"}, exit: 3,
			change: "C2, C5: a daemon 404 is the not_found envelope, exit 3, instead of `not_found: share not found` text, exit 1"},
		{name: "err-publish-missing", caller: "publish", args: []string{"publish", "--json", "{work}/missing.md"}, exit: 3, fewerRequests: true,
			change: "C2, C5: a missing path is the not_found envelope, exit 3, before the daemon is checked, instead of the stat error text, exit 1"},
		{name: "err-daemon-down", caller: "every daemon command", args: []string{"list", "--json"}, down: true,
			change: "C2: the daemon_unavailable envelope instead of the raw error text; exit 1 is unchanged"},
		{name: "err-unknown-command", stdoutChanged: true, caller: "contracts", args: []string{"definitely-not-a-real-subcommand-xyz"}, exit: 2, fewerRequests: true,
			change: "C6: an unknown word that is not an existing path is a usage error, instead of publishing it and failing to stat it"},
		{name: "err-unknown-flag", stdoutChanged: true, caller: "contracts", args: []string{"--definitely-not-a-real-flag-xyz"}, exit: 2,
			change: "C4: a parse error exits 2 with one error line instead of the usage text and exit 80"},
		{name: "err-bare", stdoutChanged: true, caller: "contracts", args: []string{}, exit: 2,
			change: "C4: a parse error exits 2 with one error line instead of the usage text and exit 80"},
		{name: "err-missing-arg", stdoutChanged: true, caller: "get", args: []string{"get", "--json"}, exit: 2,
			change: "C4: a parse error exits 2 with the usage envelope instead of the usage text and exit 80"},
		{name: "err-bad-duration", stdoutChanged: true, caller: "publish", args: []string{"publish", "--expires-in=abc", "--json", "{work}/notes.txt"}, exit: 2,
			change: "C4: an invalid duration is the invalid_args envelope, exit 2, instead of kong's usage text and exit 80"},
	}
)

// golden is what one case produced.
type golden struct {
	Caller     string              `json:"caller"`
	Args       []string            `json:"args"`
	Exit       int                 `json:"exit"`
	StdoutJSON any                 `json:"stdout_json,omitempty"`
	StdoutText string              `json:"stdout_text,omitempty"`
	StderrJSON any                 `json:"stderr_error,omitempty"`
	StderrText string              `json:"stderr_text,omitempty"`
	Requests   []ferrytest.Request `json:"requests"`
	Change     string              `json:"change,omitempty"`
}

func TestCallers(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] {
			t.Fatalf("duplicate case %s", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join("testdata", c.name+".json")
			if *record != "" {
				b, _ := json.MarshalIndent(run(t, c, *record), "", "  ")
				if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no golden for %s; record it with internal/compat/record.sh: %v", c.name, err)
			}
			var want golden
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			compare(t, c, want, run(t, c, ""))
		})
	}
}

func compare(t *testing.T, c tcase, want, got golden) {
	t.Helper()
	wantExit := want.Exit
	if c.exit != 0 {
		wantExit = c.exit
	}
	if got.Exit != wantExit {
		t.Errorf("exit %d, want %d (old binary %d)", got.Exit, wantExit, want.Exit)
	}
	refusedEarlier := c.fewerRequests && len(got.Requests) < len(want.Requests)
	if !refusedEarlier && !reflect.DeepEqual(got.Requests, want.Requests) {
		t.Errorf("daemon requests differ:\n new %v\n old %v", got.Requests, want.Requests)
	}
	if !c.stdoutChanged {
		if !reflect.DeepEqual(roundTrip(got.StdoutJSON), roundTrip(want.StdoutJSON)) {
			g, _ := json.Marshal(got.StdoutJSON)
			w, _ := json.Marshal(want.StdoutJSON)
			t.Errorf("stdout JSON differs:\n new %s\n old %s", g, w)
		}
		if got.StdoutText != want.StdoutText {
			t.Errorf("stdout text differs:\n new %q\n old %q", got.StdoutText, want.StdoutText)
		}
	}
	if c.change != "" {
		return
	}
	if !reflect.DeepEqual(roundTrip(got.StderrJSON), roundTrip(want.StderrJSON)) {
		g, _ := json.Marshal(got.StderrJSON)
		w, _ := json.Marshal(want.StderrJSON)
		t.Errorf("stderr error differs:\n new %s\n old %s", g, w)
	}
	if got.StderrText != want.StderrText {
		t.Errorf("stderr text differs:\n new %q\n old %q", got.StderrText, want.StderrText)
	}
}

func roundTrip(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// run sets up the case's sandbox and daemon and runs it, with the old binary
// at bin or, when bin is empty, the new CLI in-process.
func run(t *testing.T, c tcase, bin string) golden {
	home := t.TempDir()
	s := &scenario{t: t, home: home, work: filepath.Join(home, "work"), daemon: ferrytest.Start(t)}
	files := map[string]string{
		"report.md":              "# Q3 Roadmap\n\nPlain text.\n",
		"notes.txt":              "notes\n",
		"docs/index.md":          "# Docs\n",
		"bundle/plan.md":         "# Plan\n\n![Diagram](./img/diagram.png)\n",
		"bundle/img/diagram.png": "png",
		"escape/plan.md":         "# Plan\n\n![Diagram](../img/diagram.png)\n",
		"wbr/index.html":         "<title>Weekly Business Review</title>\n",
		"wbr/meeting-prep.md":    "# Topics\n",
		"wbr/weekly-business-review-2026-W40.pdf": "%PDF-1.4\n",
	}
	for rel, content := range files {
		p := s.path(rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fakebin := filepath.Join(home, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	if !c.noTailscale {
		ferrytest.WriteFakeTailscale(t, fakebin)
	}
	if c.setup != nil {
		c.setup(s)
	}
	s.daemon.ResetRequests()

	args := make([]string, len(c.args))
	for i, a := range c.args {
		a = strings.ReplaceAll(a, "{work}", s.work)
		if len(s.ids) > 0 {
			a = strings.ReplaceAll(a, "{id}", s.ids[0])
		}
		args[i] = a
	}
	admin := s.daemon.AdminAddr
	if c.down {
		admin = ""
	}
	env := map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_STATE_HOME": filepath.Join(home, ".local", "state"),
		"XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"PATH":             fakebin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"FERRY_ADMIN_ADDR": admin, "FERRY_LAUNCH_AGENT_LABEL": "", "TZ": "UTC",
	}

	var code int
	var stdout, stderr bytes.Buffer
	if bin != "" {
		cmd := exec.Command(bin, args...)
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Dir = s.work
		cmd.Stdout, cmd.Stderr, cmd.Stdin = &stdout, &stderr, strings.NewReader("")
		if err := cmd.Run(); err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = ee.ExitCode()
		}
	} else {
		for k, v := range env {
			t.Setenv(k, v)
		}
		t.Chdir(s.work)
		b := ops.NewBackend()
		b.CommandOutput = &stderr
		reg := ops.New("dev", b)
		o := toolkit.CLIOptions(ops.Options(b))
		o.Stdin, o.Stdout, o.Stderr = strings.NewReader(""), &stdout, &stderr
		code = cli.Run(context.Background(), reg, o, ops.ImplicitPublish(reg, args))
	}
	return result(t, c, s, code, stdout.String(), stderr.String())
}

var (
	rfc3339 = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	// dial errors on the sandbox socket depend on the OS and on the length
	// of the temporary path (macOS refuses long socket paths as invalid).
	dialErr = regexp.MustCompile(`(admin\.sock: connect): [a-z ]+`)
	token   = regexp.MustCompile(`t=[A-Za-z0-9_-]+`)
)

func result(t *testing.T, c tcase, s *scenario, code int, stdout, stderr string) golden {
	reqs := s.daemon.Requests()
	if reqs == nil {
		reqs = []ferrytest.Request{}
	}
	var ids []string
	for _, sh := range s.daemon.Shares(t) {
		ids = append(ids, sh.ID)
	}
	clean := func(v string) string {
		// macOS reports a working directory under /var through /private.
		v = strings.ReplaceAll(v, "/private"+s.home, "<home>")
		v = strings.ReplaceAll(v, s.home, "<home>")
		for i, id := range ids {
			v = strings.ReplaceAll(v, id, "<id"+string(rune('1'+i))+">")
		}
		v = token.ReplaceAllString(v, "t=<token>")
		v = dialErr.ReplaceAllString(v, "$1: <error>")
		return rfc3339.ReplaceAllString(v, "<time>")
	}
	for i := range reqs {
		reqs[i].Path = clean(reqs[i].Path)
	}
	g := golden{Caller: c.caller, Args: c.args, Exit: code, Requests: reqs, Change: c.change}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err == nil && strings.TrimSpace(stdout) != "" {
		g.StdoutJSON = normalise(v, clean)
	} else {
		g.StdoutText = clean(stdout)
	}
	if err := json.Unmarshal([]byte(stderr), &v); err == nil && strings.TrimSpace(stderr) != "" {
		g.StderrJSON = normalise(v, clean)
	} else {
		g.StderrText = clean(stderr)
	}
	return g
}

func normalise(v any, clean func(string) string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = normalise(val, clean)
		}
	case []any:
		for i := range x {
			x[i] = normalise(x[i], clean)
		}
	case string:
		return clean(x)
	}
	return v
}

// TestEveryCaseHasAGolden keeps the table and testdata in step.
func TestEveryCaseHasAGolden(t *testing.T) {
	if *record != "" {
		t.Skip("recording")
	}
	files, _ := filepath.Glob(filepath.Join("testdata", "*.json"))
	if len(files) != len(cases) {
		t.Errorf("%d goldens for %d cases", len(files), len(cases))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		found := false
		for _, c := range cases {
			found = found || c.name == name
		}
		if !found {
			t.Errorf("golden %s has no case", name)
		}
	}
}
