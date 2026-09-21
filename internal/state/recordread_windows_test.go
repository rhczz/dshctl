//go:build windows

package state

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestRecordBeingReplacedRecognisesTheTwoSharingErrors pins which failures are
// treated as "try again" and which are final.
func TestRecordBeingReplacedRecognisesTheTwoSharingErrors(t *testing.T) {
	for _, err := range []error{syscall.ERROR_ACCESS_DENIED, errorSharingViolation} {
		if !documentBeingReplaced(err) {
			t.Fatalf("documentBeingReplaced(%v) = false, want a retry", err)
		}
	}
	for _, err := range []error{nil, os.ErrNotExist, os.ErrPermission, errors.New("boom")} {
		if documentBeingReplaced(err) {
			t.Fatalf("documentBeingReplaced(%v) = true, want it reported as it is", err)
		}
	}
	if _, err := readDocumentFile(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readDocumentFile = %v, want the missing-file error", err)
	}
}
