// Package lock serializes dshctl's mutating operations with an advisory lock on
// a file in the state directory.
//
// The kernel owns the lock, so a process that dies mid-operation releases it
// immediately: there is no stale-lock bookkeeping to get wrong and no window in
// which two operations both believe they hold it. The pid written inside the
// file is a diagnostic record for `dshctl status`, never the lock itself.
//
// Deleting the lock file while an operation runs defeats the guarantee, because
// a lock lives on the inode. Acquire therefore verifies after locking that the
// path still names the inode it locked and starts over when it does not, so a
// `rm -rf` of the state directory cannot silently produce two holders.
package lock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TimeoutError reports that another operation held the lock for too long.
type TimeoutError struct {
	// Holder is the pid recorded by the holder, or 0 when unknown.
	Holder int
	// Waited is how long Acquire waited before giving up, which is the timeout
	// it was asked for: the wait only reports a TimeoutError when that whole
	// budget has elapsed. A budget that asked for no patience at all is reported
	// as zero, because nothing was waited.
	Waited time.Duration
}

// pollInterval is how often a waiter retries the lock.
const pollInterval = 200 * time.Millisecond

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// recordBytes bounds the pid record read from the lock file.
const recordBytes = 32

// maxTakeovers bounds how often Acquire will start over after the lock path was
// replaced underneath it.
const maxTakeovers = 100

// Error implements error.
func (e *TimeoutError) Error() string {
	if e.Holder != 0 {
		return fmt.Sprintf("another dshctl operation is running (pid=%d); waited %s", e.Holder, e.Waited)
	}
	return fmt.Sprintf("another dshctl operation is running; waited %s", e.Waited)
}

// Lock is a held advisory lock.
type Lock struct {
	file *os.File
}

// Held reports who holds the lock at path, without taking it.
//
// Returns:
//   - the pid recorded by the holder, or 0 when it cannot be read.
//   - whether some process holds the lock.
//   - an error when the lock file exists but could not be inspected, so callers
//     never report "free" for a lock they could not look at.
func Held(path string) (int, bool, error) {
	// A symlink here is residue that Acquire removes, so whatever it points at is
	// not this path's lock: reporting it as held would describe a lock that is
	// available the moment anybody tries to take it. A directory or other residue
	// is a different matter — it cannot be inspected at all, which must not be
	// confused with "free".
	info, statErr := os.Lstat(path)
	switch {
	case errors.Is(statErr, fs.ErrNotExist):
		return 0, false, nil
	case statErr != nil:
		return 0, false, fmt.Errorf("the lock file %s could not be checked: %w", path, statErr)
	case info.Mode()&os.ModeSymlink != 0:
		return 0, false, nil
	case !info.Mode().IsRegular():
		return 0, false, fmt.Errorf("the lock path %s is not a regular file, so it cannot be checked", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("the lock file %s could not be opened: %w", path, err)
	}
	defer file.Close()

	acquired, err := tryLock(file)
	switch {
	case err != nil:
		return 0, false, fmt.Errorf("the lock file %s could not be checked: %w", path, err)
	case acquired:
		// Nobody held it; undo the probe so a real Acquire can still win.
		_ = unlock(file)
		return 0, false, nil
	default:
		return readRecord(file), true, nil
	}
}

// Acquire takes the lock at path, waiting up to timeout for its holder.
//
// Parameters:
//   - ctx: cancellation aborts the wait.
//   - path: lock file; created when missing.
//   - timeout: maximum wait for the current holder.
//
// Returns:
//   - the held lock.
//   - a TimeoutError when the holder kept the lock, or the context error.
func Acquire(ctx context.Context, path string, timeout time.Duration) (*Lock, error) {
	// A non-positive budget asks for no patience at all, and the wait that is
	// reported has to say that: echoing "-1s" describes a wait that never
	// happened, and an operator reading it cannot tell whether dshctl waited,
	// how long, or whether the timeout was understood.
	if timeout < 0 {
		timeout = 0
	}
	deadline := time.Now().Add(timeout)
	for attempt := 0; attempt < maxTakeovers; attempt++ {
		lock, takenOver, err := tryAcquire(ctx, path, deadline, timeout)
		if err != nil {
			return nil, err
		}
		if !takenOver {
			return lock, nil
		}
	}
	return nil, fmt.Errorf("the lock file %s keeps being replaced; giving up", path)
}

// tryAcquire attempts one acquisition round.
//
// It returns takenOver when the lock path was replaced between opening and
// locking, which means the lock that was taken no longer guards anything.
func tryAcquire(ctx context.Context, path string, deadline time.Time, timeout time.Duration) (*Lock, bool, error) {
	// The state directory is disposable by design, so it may be removed between
	// two attempts; recreate it rather than failing with a bare ENOENT.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("the lock's parent directory %s could not be created: %w", filepath.Dir(path), err)
	}
	if err := discardNonFile(path); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("the lock file %s could not be opened: %w", path, err)
	}

	for {
		acquired, lockErr := tryLock(file)
		switch {
		case lockErr != nil:
			file.Close()
			return nil, false, fmt.Errorf("%s could not be locked: %w", path, lockErr)
		case acquired:
			changed, err := fileChanged(file, path)
			if err != nil {
				_ = unlock(file)
				file.Close()
				return nil, false, err
			}
			if changed {
				_ = unlock(file)
				file.Close()
				return nil, true, nil
			}
			if err := writeRecord(file); err != nil {
				_ = unlock(file)
				file.Close()
				return nil, false, fmt.Errorf("the lock record %s could not be written: %w", path, err)
			}
			return &Lock{file: file}, false, nil
		case time.Now().After(deadline):
			holder := readRecord(file)
			file.Close()
			return nil, false, &TimeoutError{Holder: holder, Waited: timeout}
		default:
			// Never sleep past the deadline: a caller that asked for 100ms must
			// not be made to wait 200ms, and the reported wait must be the wait.
			remaining := time.Until(deadline)
			if remaining <= 0 {
				holder := readRecord(file)
				file.Close()
				return nil, false, &TimeoutError{Holder: holder, Waited: timeout}
			}
			select {
			case <-ctx.Done():
				file.Close()
				return nil, false, ctx.Err()
			case <-time.After(minDuration(pollInterval, remaining)):
			}
		}
	}
}

// fileChanged reports whether path no longer names the inode the lock is held
// on, which happens when the lock file is deleted while an operation runs.
func fileChanged(file *os.File, path string) (bool, error) {
	openInfo, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("the lock file %s could not be checked: %w", path, err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("the lock file %s could not be checked: %w", path, err)
	}
	return !os.SameFile(openInfo, pathInfo), nil
}

// Release drops the lock. Calling it more than once is safe.
func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlock(l.file)
	_ = l.file.Close()
	l.file = nil
}

// writeRecord stores the holder pid inside the lock file.
func writeRecord(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return err
}

// readRecord reads the pid stored inside the lock file, or 0 when it is missing,
// malformed, or not a process id.
//
// The record is a diagnostic that can hold anything — a truncated write from a
// killed holder, a foreign file that happens to sit at the path. A number that
// cannot name a process is reported as unknown rather than repeated: "pid=-1"
// sends the operator looking for an operation that does not exist.
func readRecord(file *os.File) int {
	buffer := make([]byte, recordBytes)
	read, err := file.ReadAt(buffer, 0)
	if read == 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buffer[:read])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// discardNonFile removes whatever occupies path when it is not a regular file.
//
// The state directory is disposable by design, and a directory or a device node
// here can only be residue: the lock itself is always a regular file. A symlink
// is removed rather than followed, so nothing outside the state directory is
// ever touched.
func discardNonFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the lock file %s could not be checked: %w", path, err)
	}
	if info.Mode().IsRegular() {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("the residue at the lock path %s could not be cleared: %w", path, err)
	}
	return nil
}
