//go:build windows

package state

import (
	"errors"
	"syscall"
)

// errorSharingViolation is what Windows answers when a file is open without the
// sharing mode the new operation needs. The syscall package exports the sibling
// ERROR_ACCESS_DENIED but not this one, so it is declared as the operating
// system defines it.
const errorSharingViolation = syscall.Errno(32)

// recordBeingReplaced reports whether a failed read only means "the file was
// being replaced just then": Windows refuses to read a file another handle is
// replacing, and that answer is worth retrying rather than reporting.
func documentBeingReplaced(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errorSharingViolation)
}
