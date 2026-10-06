// Package ops declares the ferry client's operations once each, for the CLI,
// the HTTP API and MCP. The daemon they talk to, ferryd, is the share
// package; internal/daemon finds and starts it.
package ops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/op"
	"github.com/alecthomas/kong"

	"github.com/0xble/ferry/internal/daemon"
	"github.com/0xble/ferry/share"
)

// Backend is everything the operations reach outside the process. Tests
// replace each part.
type Backend struct {
	// Client returns a client of the ferryd admin API.
	Client func() *share.Client
	// EnsureDaemon starts a down daemon. Only applied calls use it: a
	// preview reads from a daemon that is already up, or not at all.
	EnsureDaemon func(c *share.Client) error
	// TailscaleIPv4 and TailscaleDNS are doctor's Tailscale checks.
	TailscaleIPv4 func() (string, error)
	TailscaleDNS  func() (string, error)
	// Command builds the ssh command of publish --open.
	Command func(name string, args ...string) *exec.Cmd
	// CommandOutput receives the ssh command's output. Stdout carries the
	// result, so it is never the command's stdout.
	CommandOutput io.Writer
	// Now is the clock for previews' projected expiry.
	Now func() time.Time
}

// NewBackend is the production backend: the admin address from
// FERRY_ADMIN_ADDR or the state directory, ferryd started on demand, the
// local tailscale and ssh.
func NewBackend() *Backend {
	return &Backend{
		Client: func() *share.Client { return share.NewClient(daemon.AdminAddr()) },
		EnsureDaemon: func(c *share.Client) error {
			return daemon.Ensure(c)
		},
		TailscaleIPv4: share.LocalTailscaleIPv4,
		TailscaleDNS:  share.LocalTailscaleMagicDNS,
		Command:       exec.Command,
		CommandOutput: os.Stderr,
		Now:           time.Now,
	}
}

// Globals are the root flags ferry keeps from before the toolkit. -V was
// kong's short form of --version.
type Globals struct {
	ShowVersion kong.VersionFlag `name:"show-version" short:"V" hidden:"" help:"Show version and exit"`
}

// New returns the registry of every ferry operation.
func New(version string, b *Backend) *op.Registry {
	reg := op.New("ferry", version)
	registerShares(reg, b)
	registerPublish(reg, b)
	registerDoctor(reg, b)
	return reg
}

// Options are the toolkit options for the ferry binary.
func Options(*Backend) toolkit.Options {
	return toolkit.Options{Description: "Tailnet-only file and directory serving", Globals: &Globals{}}
}

// ensure starts the daemon for an applied call.
func (b *Backend) ensure(c *share.Client) error {
	if err := b.EnsureDaemon(c); err != nil {
		return &op.Error{Kind: op.KindError, Code: "daemon_unavailable", Message: err.Error()}
	}
	return nil
}

// up checks that the daemon is already healthy, for a preview, which never
// starts it.
func up(c *share.Client) error {
	if err := c.Health(); err != nil {
		return &op.Error{Kind: op.KindError, Code: "daemon_unavailable",
			Message:     fmt.Sprintf("ferryd is not running (%v)", err),
			Suggestions: []string{"a preview never starts the daemon: run the command without --dry-run, or ferry doctor"}}
	}
	return nil
}

// daemonError classifies an admin API failure by its HTTP status: 404 is
// not_found (exit 3), 400 a usage error (exit 2), anything else exit 1.
func daemonError(err error) error {
	var apiErr *share.APIError
	if !errors.As(err, &apiErr) {
		return &op.Error{Kind: op.KindError, Code: "daemon_error", Message: err.Error()}
	}
	kind := op.KindError
	switch apiErr.StatusCode {
	case 400:
		kind = op.KindUsage
	case 404:
		kind = op.KindNotFound
	}
	code := apiErr.Code
	if code == "" {
		code = "daemon_error"
	}
	msg := apiErr.Message
	if msg == "" {
		msg = apiErr.Error()
	}
	return &op.Error{Kind: kind, Code: code, Message: msg}
}

func invalidArgs(format string, args ...any) error {
	return op.Errorf(op.KindUsage, "invalid_args", format, args...)
}

// lifetime parses a duration flag that must be above zero. The flags were
// kong durations, so their spelling is Go's (168h, 30m).
func lifetime(flag, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, invalidArgs("%s: expected duration but got %q: %v", flag, value, err)
	}
	if d <= 0 {
		return 0, invalidArgs("%s must be greater than zero", flag)
	}
	return d, nil
}
