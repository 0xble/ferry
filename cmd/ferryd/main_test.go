package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
)

func TestDefaultConfigPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/ferry-config")
	t.Setenv("HOME", "/tmp/ignored-home")

	got, err := defaultConfigPath()
	if err != nil {
		t.Fatalf("defaultConfigPath returned error: %v", err)
	}

	want := filepath.Join("/tmp/ferry-config", "ferry", configFileName)
	if got != want {
		t.Fatalf("expected config path %q, got %q", want, got)
	}
}

func TestDefaultConfigPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/ferry-home")

	got, err := defaultConfigPath()
	if err != nil {
		t.Fatalf("defaultConfigPath returned error: %v", err)
	}

	want := filepath.Join("/tmp/ferry-home", ".config", "ferry", configFileName)
	if got != want {
		t.Fatalf("expected config path %q, got %q", want, got)
	}
}

func TestTOMLConfigProvidesServeDefaults(t *testing.T) {
	t.Parallel()

	configPath := writeConfigFile(t, `
admin-addr = "127.0.0.1:40125"
public-port = 40124
token-bytes = 12
external-url = "https://cfg.example"
state-dir = "/tmp/ferry-state"
`)

	var c cli
	parser, err := kong.New(&c, kong.Configuration(tomlConfigLoader, configPath))
	if err != nil {
		t.Fatalf("kong.New returned error: %v", err)
	}
	if _, err := parser.Parse(nil); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if c.Serve.AdminAddr != "127.0.0.1:40125" {
		t.Fatalf("expected admin addr from config, got %q", c.Serve.AdminAddr)
	}
	if c.Serve.PublicPort != 40124 {
		t.Fatalf("expected public port from config, got %d", c.Serve.PublicPort)
	}
	if c.Serve.TokenBytes != 12 {
		t.Fatalf("expected token bytes from config, got %d", c.Serve.TokenBytes)
	}
	if c.Serve.ExternalURL != "https://cfg.example" {
		t.Fatalf("expected external URL from config, got %q", c.Serve.ExternalURL)
	}
	if c.Serve.StateDir != "/tmp/ferry-state" {
		t.Fatalf("expected state dir from config, got %q", c.Serve.StateDir)
	}
}

func TestCLIFlagsOverrideTOMLConfig(t *testing.T) {
	t.Parallel()

	configPath := writeConfigFile(t, `
admin-addr = "127.0.0.1:40125"
public-port = 40124
token-bytes = 12
external-url = "https://cfg.example"
state-dir = "/tmp/ferry-state"
`)

	var c cli
	parser, err := kong.New(&c, kong.Configuration(tomlConfigLoader, configPath))
	if err != nil {
		t.Fatalf("kong.New returned error: %v", err)
	}
	if _, err := parser.Parse([]string{"serve", "--public-port=49124", "--external-url=https://cli.example"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if c.Serve.PublicPort != 49124 {
		t.Fatalf("expected CLI public port override, got %d", c.Serve.PublicPort)
	}
	if c.Serve.ExternalURL != "https://cli.example" {
		t.Fatalf("expected CLI external URL override, got %q", c.Serve.ExternalURL)
	}
	if c.Serve.AdminAddr != "127.0.0.1:40125" {
		t.Fatalf("expected admin addr from config, got %q", c.Serve.AdminAddr)
	}
	if c.Serve.TokenBytes != 12 {
		t.Fatalf("expected token bytes from config, got %d", c.Serve.TokenBytes)
	}
	if c.Serve.StateDir != "/tmp/ferry-state" {
		t.Fatalf("expected state dir from config, got %q", c.Serve.StateDir)
	}
}

func TestTOMLConfigSupportsServeTable(t *testing.T) {
	t.Parallel()

	configPath := writeConfigFile(t, `
[serve]
public-port = 47124
external-url = "https://nested.example"
`)

	var c cli
	parser, err := kong.New(&c, kong.Configuration(tomlConfigLoader, configPath))
	if err != nil {
		t.Fatalf("kong.New returned error: %v", err)
	}
	if _, err := parser.Parse(nil); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if c.Serve.PublicPort != 47124 {
		t.Fatalf("expected nested public port from config, got %d", c.Serve.PublicPort)
	}
	if c.Serve.ExternalURL != "https://nested.example" {
		t.Fatalf("expected nested external URL from config, got %q", c.Serve.ExternalURL)
	}
}

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), configFileName)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}
