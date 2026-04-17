package share

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type StatePaths struct {
	BaseDir      string
	DBPath       string
	SecretPath   string
	SnapshotsDir string
	LogsDir      string
}

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
)

func DefaultStatePaths() (StatePaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return StatePaths{}, fmt.Errorf("resolve home: %w", err)
	}

	base := filepath.Join(home, ".local", "state", "ferry")
	return StatePaths{
		BaseDir:      base,
		DBPath:       filepath.Join(base, "shares.db"),
		SecretPath:   filepath.Join(base, "secret"),
		SnapshotsDir: filepath.Join(base, "snapshots"),
		LogsDir:      filepath.Join(base, "logs"),
	}, nil
}

func (p StatePaths) Ensure() error {
	if err := ensureDirMode(p.BaseDir, privateDirMode); err != nil {
		return fmt.Errorf("create base dir: %w", err)
	}
	if err := ensureDirMode(p.SnapshotsDir, privateDirMode); err != nil {
		return fmt.Errorf("create snapshots dir: %w", err)
	}
	if err := ensureDirMode(p.LogsDir, privateDirMode); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}
	return nil
}

func EnsurePrivateFile(path string) error {
	return ensureFileMode(path, privateFileMode)
}

func ensureDirMode(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func ensureFileMode(path string, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("expected file, got directory: %s", path)
	}
	return os.Chmod(path, mode)
}

func ValidateMode(mode string) string {
	m := strings.TrimSpace(strings.ToLower(mode))
	if m == ModeSnapshot {
		return ModeSnapshot
	}
	return ModeLive
}

func ResolveScopedPath(root string, rel string) (string, error) {
	rootClean := filepath.Clean(root)
	if rel == "" {
		return rootClean, nil
	}

	for _, segment := range strings.Split(rel, "/") {
		if segment == ".." {
			return "", errors.New("path escapes shared root")
		}
	}

	cleanURLPath := path.Clean("/" + rel)
	cleanRel := strings.TrimPrefix(cleanURLPath, "/")
	if cleanRel == "." {
		cleanRel = ""
	}

	target := filepath.Join(rootClean, filepath.FromSlash(cleanRel))
	targetClean := filepath.Clean(target)
	if targetClean != rootClean && !strings.HasPrefix(targetClean, rootClean+string(os.PathSeparator)) {
		return "", errors.New("path escapes shared root")
	}

	rootReal, err := filepath.EvalSymlinks(rootClean)
	if err != nil {
		rootReal = rootClean
	}

	targetReal, err := filepath.EvalSymlinks(targetClean)
	if err == nil {
		if targetReal != rootReal && !strings.HasPrefix(targetReal, rootReal+string(os.PathSeparator)) {
			return "", errors.New("symlink target escapes shared root")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}

	return targetClean, nil
}
