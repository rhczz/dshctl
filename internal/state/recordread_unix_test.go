//go:build !windows

package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestRecordBeingReplacedExcuseseNothing pins that on this platform a failed
// read is the answer, not something to wait out.
func TestRecordBeingReplacedExcuseseNothing(t *testing.T) {
	for _, err := range []error{nil, fs.ErrNotExist, fs.ErrPermission, errors.New("boom")} {
		if recordBeingReplaced(err) {
			t.Fatalf("recordBeingReplaced(%v) = true, want every failure reported as it is", err)
		}
	}
	// The retry loop must therefore read exactly once and return the error.
	if _, err := readRecordFile(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readRecordFile = %v, want the missing-file error", err)
	}
}
