package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/pelletier/go-toml"

	"github.com/0xble/ferry/share"
)

const configFileName = "config.toml"

var version = "dev"

type cli struct {
	Serve   serveCmd         `cmd:"" default:"1" help:"Run file-serving daemon"`
	Version kong.VersionFlag `short:"V" help:"Show version and exit"`
}

type serveCmd struct {
	AdminAddr   string `name:"admin-addr" default:"127.0.0.1:39125" help:"Admin API listen address"`
	PublicPort  int    `name:"public-port" default:"39124" help:"Public file-serving port (tailnet-only bind)"`
	TokenBytes  int    `name:"token-bytes" default:"8" help:"HMAC token truncation length in bytes (8 = 11 chars, 16 = 22 chars)"`
	ExternalURL string `name:"external-url" help:"Override external base URL for generated share links (e.g. https://share.example.com)"`
	StateDir    string `name:"state-dir" help:"Override state directory"`
}

func (c *serveCmd) Run() error {
	paths, err := share.DefaultStatePaths()
	if err != nil {
		return err
	}
	if c.StateDir != "" {
		paths = share.StatePaths{
			BaseDir:      c.StateDir,
			DBPath:       c.StateDir + "/shares.db",
			SecretPath:   c.StateDir + "/secret",
			SnapshotsDir: c.StateDir + "/snapshots",
			LogsDir:      c.StateDir + "/logs",
		}
	}

	daemon, err := share.NewDaemon(share.DaemonConfig{
		Paths:       paths,
		AdminAddr:   c.AdminAddr,
		PublicPort:  c.PublicPort,
		TokenBytes:  c.TokenBytes,
		ExternalURL: c.ExternalURL,
	})
	if err != nil {
		return err
	}
	defer func() { _ = daemon.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return daemon.Run(ctx)
}

func main() {
	var c cli
	configPath, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := kong.Parse(&c,
		kong.Name("ferryd"),
		kong.Description("Daemon for tailnet-only file serving"),
		kong.UsageOnError(),
		kong.Configuration(tomlConfigLoader, configPath),
		kong.Vars{"version": version},
	)
	if hasRegularFile(configPath) {
		log.Printf("loaded config file: %s", configPath)
	}
	if err := ctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultConfigPath() (string, error) {
	if configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configHome != "" {
		return filepath.Join(configHome, "ferry", configFileName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".config", "ferry", configFileName), nil
}

func hasRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func tomlConfigLoader(r io.Reader) (kong.Resolver, error) {
	tree, err := toml.LoadReader(r)
	if err != nil {
		return nil, err
	}

	return kong.ResolverFunc(func(_ *kong.Context, parent *kong.Path, flag *kong.Flag) (any, error) {
		for _, keyPath := range configKeyPaths(parent, flag.Name) {
			value := tree.GetPath(keyPath)
			if value != nil {
				return value, nil
			}
		}
		return nil, nil
	}), nil
}

func configKeyPaths(parent *kong.Path, flagName string) [][]string {
	keys := []string{flagName, strings.ReplaceAll(flagName, "-", "_")}
	paths := make([][]string, 0, len(keys)*2)
	for _, key := range keys {
		paths = append(paths, []string{key})
	}

	commandPath := configCommandPath(parent)
	if len(commandPath) == 0 {
		return paths
	}
	for _, key := range keys {
		path := append(append([]string(nil), commandPath...), key)
		paths = append(paths, path)
	}
	return paths
}

func configCommandPath(parent *kong.Path) []string {
	if parent == nil {
		return nil
	}

	var parts []string
	for node := parent.Node(); node != nil; node = node.Parent {
		if node.Type != kong.CommandNode {
			continue
		}
		parts = append(parts, node.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}
