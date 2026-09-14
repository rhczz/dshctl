//go:build windows

package lock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHeldReportsAWronglyPlacedLockAsFreeOnWindows pins the platform's own
// answer for a lock whose directory slot is a regular file.
//
// Windows reports this shape as ERROR_PATH_NOT_FOUND, which Go maps to the
// "not exist" family (syscall.Errno.Is(errors.ErrNotExist) in the standard
// library), so Held answers "free" rather than "uninspectable" — unlike Unix,
// where the same shape is an error (lock_unix_test.go). The answer is still
// safe: no lock can exist at that path, and Acquire refuses on every platform,
// so nothing can ever run while believing a real lock was taken.
func TestHeldReportsAWronglyPlacedLockAsFreeOnWindows(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	holder, locked, err := Held(filepath.Join(parent, "dshctl.lock"))
	if err != nil {
		t.Fatalf("Held = (%d, %v, %v), want the platform's not-exist answer", holder, locked, err)
	}
	if locked || holder != 0 {
		t.Fatalf("Held = (%d, %v), want no holder for a lock that cannot exist", holder, locked)
	}

	// The refusal half is shared with Unix: taking such a lock must fail.
	if held, err := Acquire(context.Background(), filepath.Join(parent, "dshctl.lock"), time.Second); err == nil {
		held.Release()
		t.Fatal("Acquire must not report a lock it could not create")
	}
	if data, readErr := os.ReadFile(parent); readErr != nil || string(data) != "not a directory" {
		t.Fatalf("the occupying file changed: %q (%v)", data, readErr)
	}
	if strings.ContainsAny(parent, "\\") == false {
		// A Windows test whose fixture never exercises a backslash-separated
		// path would not be testing the platform it claims to.
		t.Fatal("the fixture path has no backslash separators")
	}
}
