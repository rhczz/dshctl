//go:build unix

package history

import (
	"os"
	"testing"
)

// TestSaveUsesOwnerOnlyPermissions pins the mode an operator's umask must not
// widen: the file names checkouts and positions, and the state directory is the
// documented home for it.
func TestSaveUsesOwnerOnlyPermissions(t *testing.T) {
	box := store(t)
	if err := box.Save(File{Repos: []Group{{Repo: "/a", Records: []Record{record("c1", "", 1)}}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(box.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}
