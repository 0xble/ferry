package ops

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"

	"github.com/0xble/ferry/internal/ferrytest"
	"github.com/0xble/ferry/share"
)

// TestMain points HOME and PATH at a scratch directory before any test runs,
// so even a test that forgets ferrytest.Sandbox cannot reach the user's
// state, the live daemon's socket or a real ferryd.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ferry-ops-test-")
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

// fixedNow is the clock of every test backend, so previews are stable.
var fixedNow = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// world is a test daemon, a scratch directory of files to publish, and a
// backend that reaches only them.
type world struct {
	t      *testing.T
	daemon *ferrytest.Daemon
	dir    string
	b      *Backend
	// ssh records the --open commands instead of running them.
	ssh [][]string
	// tailscaleErr makes doctor's Tailscale checks fail.
	tailscaleErr error
	// down makes the daemon unreachable.
	down bool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return newWorldIn(t, t.TempDir())
}

// newWorldIn is newWorld over an existing directory of files.
func newWorldIn(t *testing.T, dir string) *world {
	t.Helper()
	w := &world{t: t, daemon: ferrytest.Start(t), dir: dir}
	w.b = &Backend{
		Client: func() *share.Client {
			if w.down {
				return share.NewClient("127.0.0.1:1")
			}
			return w.daemon.Client()
		},
		EnsureDaemon: func(c *share.Client) error {
			if err := c.Health(); err != nil {
				return ferrytest.ErrDaemonDown
			}
			return nil
		},
		TailscaleIPv4: func() (string, error) { return "100.64.0.1", w.tailscaleErr },
		TailscaleDNS:  func() (string, error) { return "studio.example.ts.net", w.tailscaleErr },
		Command: func(name string, args ...string) *exec.Cmd {
			w.ssh = append(w.ssh, append([]string{name}, args...))
			return exec.Command("true")
		},
		CommandOutput: &bytes.Buffer{},
		Now:           func() time.Time { return fixedNow },
	}
	return w
}

// file writes a file under the world's directory and returns its path.
func (w *world) file(name, content string) string {
	w.t.Helper()
	p := filepath.Join(w.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) registry() *op.Registry { return New("test", w.b) }

// run runs the generated CLI in-process.
func (w *world) run(args ...string) (int, string, string) {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	o := toolkit.CLIOptions(Options(w.b))
	o.Stdin, o.Stdout, o.Stderr = strings.NewReader(""), &stdout, &stderr
	code := cli.Run(context.Background(), w.registry(), o, args)
	return code, stdout.String(), stderr.String()
}
