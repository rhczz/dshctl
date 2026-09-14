//go:build windows

package atomically

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// replaceRetryWindow is how long the replacement keeps trying while somebody
// else holds the destination open.
const replaceRetryWindow = 500 * time.Millisecond

// replaceDelay is the pause between two attempts.
const replaceDelay = 10 * time.Millisecond

// errorSharingViolation is what Windows answers when a file is open without the
// sharing mode the new operation needs. The syscall package exports the sibling
// ERROR_ACCESS_DENIED but not this one, so it is declared as the operating
// system defines it.
const errorSharingViolation = syscall.Errno(32)

// replace renames temp over path, retrying while the destination is open
// elsewhere.
//
// On Windows a rename onto a destination that another handle has open fails with
// "Access is denied", because Go opens files without FILE_SHARE_DELETE. A reader
// is a normal thing to have here — `status` reads the runtime record while
// `start` rewrites it — and failing the write for that would make an atomic
// update a coin flip. The retry covers the reader's own read, not a lock: a
// destination that stays open beyond the window is reported as the failure it is.
func replace(temp, path string) error {
	deadline := time.Now().Add(replaceRetryWindow)
	for {
		err := os.Rename(temp, path)
		if err == nil {
			return nil
		}
		if !destinationBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(replaceDelay)
	}
}

// destinationBusy reports whether the rename failed because the destination is
// held open rather than for a reason a retry cannot fix.
func destinationBusy(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errorSharingViolation)
}
