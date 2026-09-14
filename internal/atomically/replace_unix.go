//go:build unix

package atomically

import "os"

// replace renames temp over path. On Unix the rename is atomic and replaces an
// existing destination.
func replace(temp, path string) error {
	return os.Rename(temp, path)
}
