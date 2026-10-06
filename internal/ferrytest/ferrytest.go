// Package ferrytest hosts a real ferryd daemon in-process for tests: the
// share package's admin API and share routes on random 127.0.0.1 ports, over
// a state directory under t.TempDir(). It never runs ferryd, launchctl or
// Tailscale, and never touches the user's state.
package ferrytest

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/0xble/ferry/share"
)

// BaseURL is the base of the share links the test daemon generates.
const BaseURL = "https://ferry.invalid/share"

// Request is one admin API request the daemon answered.
type Request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// Daemon is a running test daemon.
type Daemon struct {
	// AdminAddr is the admin API's TCP address, for FERRY_ADMIN_ADDR or
	// share.NewClient.
	AdminAddr string
	// State is the daemon's state directory.
	State string

	mu       sync.Mutex
	requests []Request
}

// Start runs a daemon until the test ends.
func Start(t testing.TB) *Daemon {
	t.Helper()
	public, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = public.Close()
		t.Fatal(err)
	}
	state := t.TempDir()
	paths := share.StatePaths{
		BaseDir:      state,
		DBPath:       filepath.Join(state, "shares.db"),
		SecretPath:   filepath.Join(state, "secret"),
		SnapshotsDir: filepath.Join(state, "snapshots"),
		LogsDir:      filepath.Join(state, "logs"),
		AdminSocket:  filepath.Join(state, "admin.sock"),
	}
	d, err := share.NewDaemon(share.DaemonConfig{Paths: paths, AdminAddr: "tcp:" + admin.Addr().String(),
		PublicPort: public.Addr().(*net.TCPAddr).Port})
	if err != nil {
		_ = public.Close()
		_ = admin.Close()
		t.Fatal(err)
	}
	td := &Daemon{AdminAddr: admin.Addr().String(), State: state}
	adminHandler, publicHandler := d.Handlers(BaseURL)
	recorded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		td.mu.Lock()
		td.requests = append(td.requests, Request{Method: r.Method, Path: r.URL.Path})
		td.mu.Unlock()
		adminHandler.ServeHTTP(w, r)
	})
	servers := []*http.Server{
		{Handler: recorded, ReadHeaderTimeout: 10 * time.Second},
		{Handler: publicHandler, ReadHeaderTimeout: 10 * time.Second},
	}
	go func() { _ = servers[0].Serve(admin) }()
	go func() { _ = servers[1].Serve(public) }()
	t.Cleanup(func() {
		for _, s := range servers {
			_ = s.Close()
		}
		_ = d.Close()
	})
	return td
}

// Client is an admin client of the daemon.
func (d *Daemon) Client() *share.Client { return share.NewClient(d.AdminAddr) }

// Requests returns the admin requests answered so far.
func (d *Daemon) Requests() []Request {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Request{}, d.requests...)
}

// ResetRequests forgets the requests answered so far.
func (d *Daemon) ResetRequests() {
	d.mu.Lock()
	d.requests = nil
	d.mu.Unlock()
}

// Share is one row of the daemon's store.
type Share struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	ExpiresAt int64  `json:"expires_at"`
	Revoked   bool   `json:"revoked"`
}

// Shares reads every share in the store, revoked and expired ones included,
// in creation order. It is the state a write changes.
func (d *Daemon) Shares(t testing.TB) []Share {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(d.State, "shares.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT id, source_path, mode, expires_at, revoked_at IS NOT NULL FROM shares ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := []Share{}
	for rows.Next() {
		var s Share
		if err := rows.Scan(&s.ID, &s.Path, &s.Mode, &s.ExpiresAt, &s.Revoked); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Seed inserts an active share with a known ID straight into the store, for
// tests that must name a share before the daemon exists.
func (d *Daemon) Seed(t testing.TB, id, path, mode string, isDir bool, ttl time.Duration) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(d.State, "shares.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO shares (id, source_path, is_dir, mode, snapshot_root, created_at, expires_at) VALUES (?, ?, ?, ?, '', ?, ?)`,
		id, path, isDir, mode, now.Unix(), now.Add(ttl).Unix()); err != nil {
		t.Fatal(err)
	}
}

// Create publishes path directly through the admin API, for test setup.
func (d *Daemon) Create(t testing.TB, path, mode string, ttl time.Duration) share.ShareResponse {
	t.Helper()
	s, err := d.Client().CreateShare(share.CreateShareRequest{Path: path, Mode: mode, ExpiresInSeconds: int64(ttl / time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Sandbox points HOME, the XDG directories and PATH into t.TempDir() and
// clears every FERRY_ variable, so nothing a test runs can read or write the
// user's state, reach the live daemon, kickstart its launch agent or spawn a
// real ferryd. PATH holds only a fake tailscale (healthy unless broken) and
// /usr/bin:/bin. It returns the new HOME.
func Sandbox(t *testing.T, tailscaleHealthy bool) string {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, "fakebin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if tailscaleHealthy {
		WriteFakeTailscale(t, bin)
	}
	for k, v := range map[string]string{
		"HOME":                     home,
		"XDG_CONFIG_HOME":          filepath.Join(home, ".config"),
		"XDG_DATA_HOME":            filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":           filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME":           filepath.Join(home, ".cache"),
		"PATH":                     bin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"FERRY_ADMIN_ADDR":         "",
		"FERRY_LAUNCH_AGENT_LABEL": "",
		"TZ":                       "UTC",
	} {
		t.Setenv(k, v)
	}
	return home
}

// WriteFakeTailscale writes a tailscale script into dir that answers
// `ip -4` and `status --json` like a connected node, and fails otherwise.
func WriteFakeTailscale(t testing.TB, dir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"ip) echo 100.64.0.1 ;;\n" +
		"status) echo '{\"Self\":{\"DNSName\":\"studio.example.ts.net.\"}}' ;;\n" +
		"*) echo \"fake tailscale: unsupported $*\" >&2; exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// ErrDaemonDown is what a test's EnsureDaemon returns instead of starting a
// daemon.
var ErrDaemonDown = errors.New("test daemon is down and tests never start ferryd")
