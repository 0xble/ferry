package share

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const DefaultSnapshotMaxBytes int64 = 1 << 30

type SnapshotLimitError struct {
	Size  int64
	Limit int64
}

func (e *SnapshotLimitError) Error() string {
	return fmt.Sprintf("snapshot size %s exceeds configured limit %s; use live mode instead (omit --snapshot)", formatBytes(e.Size), formatBytes(e.Limit))
}

func CreateSnapshot(paths StatePaths, shareID string, sourcePath string, isDir bool, maxBytesOpt ...int64) (string, error) {
	maxBytes := DefaultSnapshotMaxBytes
	if len(maxBytesOpt) > 0 && maxBytesOpt[0] > 0 {
		maxBytes = maxBytesOpt[0]
	}

	size, err := snapshotSize(sourcePath, isDir, maxBytes)
	if err != nil {
		return "", err
	}
	if size > maxBytes {
		return "", snapshotLimitError(size, maxBytes)
	}

	dstRoot := filepath.Join(paths.SnapshotsDir, shareID)
	if err := os.RemoveAll(dstRoot); err != nil {
		return "", fmt.Errorf("clear snapshot root: %w", err)
	}

	budget := &snapshotBudget{remaining: maxBytes}
	var dst string
	if isDir {
		dst = filepath.Join(dstRoot, "root")
		err = copyTree(sourcePath, dst, budget)
	} else {
		dst = filepath.Join(dstRoot, "file", filepath.Base(sourcePath))
		err = copyFile(sourcePath, dst, budget)
	}
	if err != nil {
		_ = os.RemoveAll(dstRoot)
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

func snapshotSize(sourcePath string, isDir bool, maxBytes int64) (int64, error) {
	if !isDir {
		info, err := os.Stat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("stat source file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("source is not a regular file: %s", sourcePath)
		}
		return info.Size(), nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return 0, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("source is not directory: %s", sourcePath)
	}

	var total int64
	err = filepath.Walk(sourcePath, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
			return nil
		}
		if fileInfo.Size() > maxBytes-total {
			total = maxBytes + 1
			return filepath.SkipAll
		}
		total += fileInfo.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure source directory: %w", err)
	}
	return total, nil
}

func snapshotLimitError(size int64, maxBytes int64) error {
	return &SnapshotLimitError{Size: size, Limit: maxBytes}
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

type snapshotBudget struct {
	remaining int64
}

func copyTree(src string, dst string, budget *snapshotBudget) error {
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
		return copyFile(path, target, budget)
	})
}

func copyFile(src string, dst string, budget *snapshotBudget) error {
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

	copied, err := io.Copy(out, io.LimitReader(in, budget.remaining+1))
	if err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if copied > budget.remaining {
		return snapshotLimitError(copied, budget.remaining)
	}
	budget.remaining -= copied
	if err := out.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod destination file: %w", err)
	}
	return nil
}
