// Package atomically writes small state files so a reader never observes a
// half-written document and a crash never leaves a truncated one.
//
// dshctl's state directory is disposable, but it is also read by concurrent
// operations: a config file that is renamed into place cannot be seen empty, and
// a runtime record that is replaced in one step cannot be read while it is
// being rewritten.
package atomically

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// WriteFile writes data to path through a temporary file in the same directory,
// then renames it over the destination.
//
// The temporary file is created with the same permissions the caller asked for,
// so a state file never exists briefly as world-readable.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("无法创建目录 %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("无法在 %s 创建临时文件: %w", dir, err)
	}
	tempName := temp.Name()
	// Best effort: a failed cleanup must not hide the real error.
	defer func() { _ = os.Remove(tempName) }()

	if err := temp.Chmod(perm); err != nil {
		temp.Close()
		return fmt.Errorf("无法设置 %s 的权限: %w", tempName, err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("无法写入 %s: %w", tempName, err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("无法同步 %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("无法关闭 %s: %w", tempName, err)
	}
	if err := replace(tempName, path); err != nil {
		return fmt.Errorf("无法替换 %s: %w", path, err)
	}
	// The rename is atomic, but it is only durable once the directory entry
	// itself has reached the disk. A machine that loses power in between comes
	// back with the old file, which for a record or a settings document is the
	// safe outcome — so a failure here is reported, not hidden.
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("无法同步目录 %s: %w", dir, err)
	}
	return nil
}

// tempNamePattern is the exact shape WriteFile creates: ".<base>.tmp<random
// digits>". Sweep removes only files matching it, so an operator's own hidden
// file that merely contains ".tmp" is left alone.
var tempNamePattern = regexp.MustCompile(`^\..+\.tmp[0-9]+$`)

// Sweep removes temporary files this package left behind.
//
// A process killed between creating its temporary file and renaming it leaves
// that file in place, and nothing else ever cleans it up: for a state directory
// that is written repeatedly, the leftovers accumulate. Called by whoever
// provisions the directory.
//
// Parameters:
//   - dir: directory to sweep.
//
// Returns:
//   - an error only when the directory cannot be read.
func Sweep(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("无法读取目录 %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !tempNamePattern.MatchString(entry.Name()) {
			continue
		}
		// Best effort: a leftover that cannot be removed must not stop the caller.
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
	return nil
}
