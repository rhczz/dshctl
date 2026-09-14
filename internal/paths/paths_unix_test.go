//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureDirIsOwnerOnly pins the permissions of created state directories.
//
// The mode bits are a Unix concept: Windows reports 0777/0555 for a directory
// whatever mode was asked for, so the assertion is meaningless there and would
// only fail. The directory's existence is covered by the portable test.
func TestEnsureDirIsOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", info.Mode().Perm())
	}
}
