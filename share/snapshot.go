package share

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func CreateSnapshot(paths StatePaths, shareID string, sourcePath string, isDir bool) (string, error) {
	dstRoot := filepath.Join(paths.SnapshotsDir, shareID)
	if err := os.RemoveAll(dstRoot); err != nil {
		return "", fmt.Errorf("clear snapshot root: %w", err)
	}

	if isDir {
		dst := filepath.Join(dstRoot, "root")
		if err := copyTree(sourcePath, dst); err != nil {
			return "", err
		}
		return dst, nil
	}

	dst := filepath.Join(dstRoot, "file", filepath.Base(sourcePath))
	if err := copyFile(sourcePath, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func CleanupSnapshot(snapshotRoot string) error {
	if snapshotRoot == "" {
		return nil
	}
	return os.RemoveAll(filepath.Dir(filepath.Dir(snapshotRoot)))
}

func copyTree(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not directory: %s", src)
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create dst dir: %w", err)
	}

	return filepath.Walk(src, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		target := filepath.Join(dst, rel)
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if fileInfo.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := out.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod destination file: %w", err)
	}
	return nil
}
