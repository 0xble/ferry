// Package daemon finds the ferryd admin API and starts ferryd when it is
// down. It is the client side of the daemon: ferryd itself lives in
// cmd/ferryd and the share package.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/0xble/ferry/share"
)

// LaunchAgentLabelEnv names the launchd job to kickstart before spawning
// ferryd directly. Unset means spawn directly.
const LaunchAgentLabelEnv = "FERRY_LAUNCH_AGENT_LABEL"

// AdminAddrEnv overrides the admin API address.
const AdminAddrEnv = "FERRY_ADMIN_ADDR"

// ErrNotFound is returned when no ferryd binary can be found to spawn.
var ErrNotFound = errors.New("ferryd daemon binary not found in PATH")

// AdminAddr returns the admin socket path for the current user's state
// directory. Falls back to the legacy TCP loopback address only if the home
// directory cannot be resolved (extremely unusual, kept to avoid panics in
// degraded environments).
func AdminAddr() string {
	if env := strings.TrimSpace(os.Getenv(AdminAddrEnv)); env != "" {
		return env
	}
	paths, err := share.DefaultStatePaths()
	if err != nil || paths.AdminSocket == "" {
		return "tcp:127.0.0.1:39125"
	}
	return paths.AdminSocket
}

// Health is the part of share.Client that Ensure needs.
type Health interface {
	Health() error
}

// Ensure returns once the daemon behind client is healthy. A down daemon is
// first kickstarted through launchd (on macOS, when FERRY_LAUNCH_AGENT_LABEL
// is set), then spawned as a detached `ferryd serve`.
func Ensure(client Health) error {
	if err := client.Health(); err == nil {
		return nil
	}

	_ = kickstartLaunchAgent()
	for i := 0; i < 10; i++ {
		if err := client.Health(); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	if err := spawnDaemonProcess(); err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		if err := client.Health(); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return fmt.Errorf("daemon did not become healthy")
}

func kickstartLaunchAgent() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	label := os.Getenv(LaunchAgentLabelEnv)
	if label == "" {
		return nil
	}
	domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	return exec.Command("launchctl", "kickstart", "-k", domain).Run()
}

func spawnDaemonProcess() error {
	daemonPath, err := resolveDaemonPath()
	if err != nil {
		return err
	}

	paths, err := share.DefaultStatePaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	logPath := filepath.Join(paths.LogsDir, "ferryd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	if err := share.EnsurePrivateFile(logPath); err != nil {
		return fmt.Errorf("lock daemon log file: %w", err)
	}

	cmd := exec.Command(daemonPath, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin = devNull

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return cmd.Process.Release()
}

func resolveDaemonPath() (string, error) {
	if path, err := exec.LookPath("ferryd"); err == nil {
		return path, nil
	}

	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "ferryd")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	home, homeErr := os.UserHomeDir()
	if homeErr == nil && home != "" {
		candidate := filepath.Join(home, ".local", "bin", "ferryd")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", ErrNotFound
}
