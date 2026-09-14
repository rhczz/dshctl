//go:build windows

package atomically

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestWriteFileOverAFileInTheWayIsReportedAsMissing pins the platform's own
// classification of the failure the portable test asserts.
//
// Windows answers "the system cannot find the path specified" when a component
// of the directory chain is a file, and Go maps that to fs.ErrNotExist — so an
// error that looks like "there is nothing here" is what the operating system
// actually said, not a claim this package invented. What must not be lost, and
// is asserted here as well, is that the message still names the directory that
// could not be created: that is the part the operator acts on.
func TestWriteFileOverAFileInTheWayIsReportedAsMissing(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := WriteFile(filepath.Join(parent, "file.json"), []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("a file where the directory belongs must fail")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want the platform's own classification (path not found)", err)
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("error = %v, want it to name %s", err, parent)
	}
}

// transientRead reports whether a failed read only means "the file was being
// replaced just then". Windows refuses a read of a file that another handle is
// replacing, which is a "try again" rather than a torn document — and the test
// that uses it asserts exactly that: every document a reader does see is whole.
func transientRead(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, syscall.Errno(32))
}
