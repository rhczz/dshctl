// Package exitcode defines the process exit codes dshctl returns and the typed
// error that carries one through the call stack.
package exitcode

import (
	"errors"
	"fmt"
)

// Process exit codes. Scripts branch on these instead of parsing output.
const (
	// OK means the requested operation succeeded, or the probed service runs.
	OK = 0
	// Failure is any unclassified error.
	Failure = 1
	// Usage is a malformed command line or an invalid configured value.
	Usage = 2
	// NotRunning reports that the probed service is not running.
	NotRunning = 3
	// Preflight reports an unmet precondition for the requested operation.
	Preflight = 4
	// LockTimeout reports that another dshctl operation holds the lock.
	LockTimeout = 5
	// Interrupted is the conventional 128+SIGINT code for a cancelled command.
	Interrupted = 130
)

// Error attaches a process exit code to a failure.
type Error struct {
	// Code is the process exit code the CLI must return.
	Code int
	// Err is the underlying failure.
	Err error
}

// Error implements error.
func (e *Error) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying failure to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// New builds a coded error from a format string.
func New(code int, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Wrap attaches a code to err unless err already carries one, so the innermost
// classification wins.
func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	var coded *Error
	if errors.As(err, &coded) {
		return err
	}
	return &Error{Code: code, Err: err}
}

// Silent carries an exit code for a command that already reported its result on
// its own output, so the CLI must not print a second error line for it.
type Silent struct {
	// Code is the process exit code the CLI must return.
	Code int
}

// Error implements error with an empty message.
func (s *Silent) Error() string { return "" }

// SilentExit builds a coded result whose message is already printed.
func SilentExit(code int) error { return &Silent{Code: code} }

// Of reports the exit code carried by err, or Failure for an unclassified one.
func Of(err error) int {
	if err == nil {
		return OK
	}
	var silent *Silent
	if errors.As(err, &silent) {
		return silent.Code
	}
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return Failure
}
