package share

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveScopedPathBlocksEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := filepath.Join(root, "a", "b.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveScopedPath(root, "a/b.txt")
	if err != nil {
		t.Fatalf("expected resolve success, got %v", err)
	}
	if resolved != inside {
		t.Fatalf("expected %s, got %s", inside, resolved)
	}

	if _, err := ResolveScopedPath(root, "../../etc/passwd"); err == nil {
		t.Fatal("expected escape path to fail")
	}
}

func TestStatePathsEnsureRepairsExistingDirectoryPermissions(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "state")
	logs := filepath.Join(base, "logs")
	snapshots := filepath.Join(base, "snapshots")
	for _, dir := range []string{base, logs, snapshots} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatalf("Chmod(%s): %v", dir, err)
		}
	}

	paths := StatePaths{
		BaseDir:      base,
		LogsDir:      logs,
		SnapshotsDir: snapshots,
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure(): %v", err)
	}

	for _, dir := range []string{base, logs, snapshots} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat(%s): %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s perms = %o, want 700", dir, got)
		}
	}
}

func TestEnsurePrivateFileRepairsExistingFilePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod(): %v", err)
	}

	if err := EnsurePrivateFile(path); err != nil {
		t.Fatalf("EnsurePrivateFile(): %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file perms = %o, want 600", got)
	}
}
