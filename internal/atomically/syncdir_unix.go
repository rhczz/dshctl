//go:build unix

package atomically

import "os"

// syncDir flushes a directory entry so a rename survives a crash.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		// Some filesystems refuse to sync a directory; the rename itself already
		// happened, so this is reported rather than treated as a failed write.
		return err
	}
	return nil
}
