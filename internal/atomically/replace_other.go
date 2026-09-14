//go:build !unix && !windows

package atomically

import "os"

// replace renames temp over path.
//
// Go's os.Rename replaces an existing destination on Windows as well — it is
// MoveFileEx with MOVEFILE_REPLACE_EXISTING — so no extra step is needed for the
// ordinary case of replacing a file. An earlier version removed the destination
// first when the rename reported "file exists", on the theory that Windows
// refuses to rename onto an existing file. That premise is false for files, and
// the branch only ever fired when the destination was a *directory*, which it
// then deleted if empty. Failing loudly is the right answer there.
func replace(temp, path string) error {
	return os.Rename(temp, path)
}
