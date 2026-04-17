package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/0xble/ferry/internal/cli"
	"github.com/0xble/ferry/share"
)

const launchAgentLabelEnv = "FERRY_LAUNCH_AGENT_LABEL"

var version = "dev"

var cliFlags struct {
	JSON    bool             `short:"j" help:"Output as JSON"`
	Version kong.VersionFlag `short:"V" help:"Show version and exit"`

	Publish publishCmd `cmd:"" help:"Publish a file or directory"`
	List    listCmd    `cmd:"" help:"List active shares"`
	Get     getCmd     `cmd:"" help:"Get a share by id"`
	Unshare unshareCmd `cmd:"" help:"Revoke a share by id or exact path"`
	Renew   renewCmd   `cmd:"" help:"Extend share expiry"`
	Doctor  doctorCmd  `cmd:"" help:"Check daemon and Tailscale health"`
}

var execCommand = exec.Command

type publishCmd struct {
	Path      string        `arg:"" help:"File or directory path to publish"`
	Snapshot  bool          `help:"Snapshot content instead of live mode"`
	ExpiresIn time.Duration `name:"expires-in" default:"168h" help:"Share lifetime (default: 7d)"`
	Open      string        `help:"Open URL on a remote machine via SSH (e.g. laptop)"`
}

type publishPlan struct {
	RequestedPath string
	SharePath     string
	EntryRel      string
}

type listCmd struct{}

type getCmd struct {
	ID string `arg:"" help:"Share ID"`
}

type unshareCmd struct {
	Target string `arg:"" help:"Share ID or exact source path"`
}

type renewCmd struct {
	ID  string        `arg:"" help:"Share ID"`
	For time.Duration `name:"for" default:"168h" help:"Additional lifetime from now"`
}

type doctorCmd struct{}

func main() {
	os.Args = rewriteArgsForPublish(os.Args)

	ctx := kong.Parse(&cliFlags,
		kong.Name("ferry"),
		kong.Description("Tailnet-only file and directory serving"),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)

	client := share.NewClient(share.DefaultAdminAddr)

	var err error
	switch ctx.Command() {
	case "ferry publish <path>", "publish <path>":
		err = runPublish(client)
	case "ferry list", "list":
		err = runList(client)
	case "ferry get <id>", "get <id>":
		err = runGet(client)
	case "ferry unshare <target>", "unshare <target>":
		err = runUnshare(client)
	case "ferry renew <id>", "renew <id>":
		err = runRenew(client)
	case "ferry doctor", "doctor":
		err = runDoctor(client)
	default:
		err = fmt.Errorf("unsupported command: %s", ctx.Command())
	}

	if err != nil {
		format := cli.ResolveFormat("", cliFlags.JSON)
		if cliErr, ok := err.(*cli.CLIError); ok {
			cli.WriteError(os.Stderr, format, cliErr)
			os.Exit(cliErr.ExitCode)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitError)
	}
}

func rewriteArgsForPublish(args []string) []string {
	if len(args) < 2 {
		return args
	}

	known := map[string]bool{
		"publish": true,
		"list":    true,
		"get":     true,
		"unshare": true,
		"renew":   true,
		"doctor":  true,
		"help":    true,
	}

	idx := 1
	for idx < len(args) {
		token := strings.TrimSpace(args[idx])
		switch token {
		case "", "--":
			return args
		case "-h", "--help":
			return args
		case "-j", "--json":
			idx++
			continue
		}
		if strings.HasPrefix(token, "-") {
			idx++
			continue
		}
		if known[token] {
			return args
		}

		out := make([]string, 0, len(args)+1)
		out = append(out, args[:idx]...)
		out = append(out, "publish")
		out = append(out, args[idx:]...)
		return out
	}

	return args
}

func runPublish(client *share.Client) error {
	target := strings.TrimSpace(cliFlags.Publish.Path)
	if target == "" {
		return cli.ErrWithExit("invalid_args", "file or directory path is required", cli.ExitUsage)
	}
	if cliFlags.Publish.ExpiresIn <= 0 {
		return cli.ErrWithExit("invalid_args", "--expires-in must be greater than zero", cli.ExitUsage)
	}

	if err := ensureDaemon(client); err != nil {
		return err
	}

	mode := share.ModeLive
	if cliFlags.Publish.Snapshot {
		mode = share.ModeSnapshot
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	plan, err := resolvePublishPlan(absPath)
	if err != nil {
		return err
	}

	var url string

	if mode == share.ModeLive {
		existing, ok, err := findExistingLiveShare(client, plan.SharePath)
		if err != nil {
			return err
		}
		if ok {
			existing, err = client.RenewShare(existing.ID, cliFlags.Publish.ExpiresIn)
			if err != nil {
				return err
			}
			existing.URL, err = resolvePublishURL(existing.URL, plan.EntryRel)
			if err != nil {
				return err
			}
			url = existing.URL
			if cliFlags.JSON {
				return cli.EncodeJSON(os.Stdout, existing)
			}
			fmt.Println(formatShareText(existing, shareTextOptions{
				Path:       plan.RequestedPath,
				BundleRoot: publishBundleRoot(plan, existing.Path),
			}))
			return openOnRemote(url)
		}
	}

	resp, err := client.CreateShare(share.CreateShareRequest{
		Path:             plan.SharePath,
		Mode:             mode,
		ExpiresInSeconds: int64(cliFlags.Publish.ExpiresIn / time.Second),
	})
	if err != nil {
		return err
	}
	resp.URL, err = resolvePublishURL(resp.URL, plan.EntryRel)
	if err != nil {
		return err
	}
	url = resp.URL

	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, resp)
	}

	fmt.Println(formatShareText(resp, shareTextOptions{
		Path:       plan.RequestedPath,
		BundleRoot: publishBundleRoot(plan, resp.Path),
	}))
	return openOnRemote(url)
}

type shareTextOptions struct {
	Path       string
	BundleRoot string
}

func formatShareText(shareResp share.ShareResponse, opts shareTextOptions) string {
	displayPath := shareResp.Path
	if strings.TrimSpace(opts.Path) != "" {
		displayPath = opts.Path
	}

	lines := []string{
		fmt.Sprintf("id: %s", shareResp.ID),
		fmt.Sprintf("kind: %s", shareKind(shareResp.IsDir)),
		fmt.Sprintf("mode: %s", shareResp.Mode),
		fmt.Sprintf("path: %s", displayPath),
	}
	if strings.TrimSpace(opts.BundleRoot) != "" {
		lines = append(lines, fmt.Sprintf("bundle_root: %s", opts.BundleRoot))
	}
	if !shareResp.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("created: %s", shareResp.CreatedAt.Local().Format(time.RFC3339)))
	}
	lines = append(lines,
		fmt.Sprintf("expires: %s", shareResp.ExpiresAt.Local().Format(time.RFC3339)),
		fmt.Sprintf("url: %s", shareResp.URL),
	)
	return strings.Join(lines, "\n")
}

func formatShareListText(shares []share.ShareResponse) string {
	entries := make([]string, 0, len(shares))
	for _, shareResp := range shares {
		entries = append(entries, formatShareText(shareResp, shareTextOptions{}))
	}
	return strings.Join(entries, "\n\n")
}

func shareKind(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
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
		return publishPlan{}, err
	}
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
		return publishPlan{}, cli.ErrWithExit(
			"invalid_args",
			"markdown file references assets outside its directory; publish the parent directory explicitly",
			cli.ExitUsage,
		)
	}

	plan.SharePath = filepath.Dir(absPath)
	plan.EntryRel = filepath.Base(absPath)
	return plan, nil
}

func isMarkdownPath(target string) bool {
	switch strings.ToLower(filepath.Ext(target)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
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

func openOnRemote(url string) error {
	host := strings.TrimSpace(cliFlags.Publish.Open)
	if host == "" {
		return nil
	}
	cmd := execCommand("ssh", host, "open", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findExistingLiveShare(client *share.Client, absPath string) (share.ShareResponse, bool, error) {
	shares, err := client.ListShares()
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

func runList(client *share.Client) error {
	if err := ensureDaemon(client); err != nil {
		return err
	}
	shares, err := client.ListShares()
	if err != nil {
		return err
	}
	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, shares)
	}
	if len(shares) == 0 {
		fmt.Println("No active shares")
		return nil
	}
	fmt.Println(formatShareListText(shares))
	return nil
}

func runGet(client *share.Client) error {
	if err := ensureDaemon(client); err != nil {
		return err
	}
	share, err := client.GetShare(strings.TrimSpace(cliFlags.Get.ID))
	if err != nil {
		return err
	}
	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, share)
	}
	fmt.Println(formatShareText(share, shareTextOptions{}))
	return nil
}

func runUnshare(client *share.Client) error {
	if err := ensureDaemon(client); err != nil {
		return err
	}
	target := strings.TrimSpace(cliFlags.Unshare.Target)
	if target == "" {
		return cli.ErrWithExit("invalid_args", "share id or path is required", cli.ExitUsage)
	}

	if !strings.Contains(target, string(os.PathSeparator)) {
		if err := client.RevokeShare(target); err == nil {
			if cliFlags.JSON {
				return cli.EncodeJSON(os.Stdout, map[string]any{"ok": true, "id": target})
			}
			fmt.Printf("revoked share: %s\n", target)
			return nil
		}
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	shares, err := client.ListShares()
	if err != nil {
		return err
	}

	revoked := 0
	for _, share := range shares {
		if share.Path != abs {
			continue
		}
		if err := client.RevokeShare(share.ID); err == nil {
			revoked++
		}
	}
	if revoked == 0 {
		return cli.ErrWithExit("not_found", "no active share matched target", cli.ExitNotFound)
	}

	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, map[string]any{"ok": true, "path": abs, "revoked": revoked})
	}
	fmt.Printf("revoked %d share(s) for %s\n", revoked, abs)
	return nil
}

func runRenew(client *share.Client) error {
	if err := ensureDaemon(client); err != nil {
		return err
	}
	if cliFlags.Renew.For <= 0 {
		return cli.ErrWithExit("invalid_args", "--for must be greater than zero", cli.ExitUsage)
	}
	share, err := client.RenewShare(strings.TrimSpace(cliFlags.Renew.ID), cliFlags.Renew.For)
	if err != nil {
		return err
	}
	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, share)
	}
	fmt.Println(formatShareText(share, shareTextOptions{}))
	return nil
}

func runDoctor(client *share.Client) error {
	var tsErr error
	if _, err := share.LocalTailscaleIPv4(); err != nil {
		tsErr = err
	}
	if _, err := share.LocalTailscaleMagicDNS(); err != nil {
		if tsErr == nil {
			tsErr = err
		} else {
			tsErr = fmt.Errorf("%v; %w", tsErr, err)
		}
	}
	daemonErr := client.Health()

	report := map[string]any{
		"tailscale_ok": tsErr == nil,
		"daemon_ok":    daemonErr == nil,
	}
	if tsErr != nil {
		report["tailscale_error"] = tsErr.Error()
	}
	if daemonErr != nil {
		report["daemon_error"] = daemonErr.Error()
	}

	if cliFlags.JSON {
		return cli.EncodeJSON(os.Stdout, report)
	}

	if tsErr == nil {
		fmt.Println("tailscale: ok")
	} else {
		fmt.Printf("tailscale: error (%v)\n", tsErr)
	}
	if daemonErr == nil {
		fmt.Println("daemon: ok")
	} else {
		fmt.Printf("daemon: error (%v)\n", daemonErr)
	}

	if tsErr != nil || daemonErr != nil {
		return cli.ErrWithExit("health_check_failed", "ferry doctor detected issues", cli.ExitError)
	}
	return nil
}

func ensureDaemon(client *share.Client) error {
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
	label := os.Getenv(launchAgentLabelEnv)
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

	return "", fmt.Errorf("ferryd daemon binary not found in PATH")
}
