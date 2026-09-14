//go:build unix

package lock

import (
	"errors"
	"os"
	"syscall"
)

// tryLock attempts to take the lock without blocking.
//
// Returns true when the lock is now held by this process. A held-by-someone-else
// answer is not an error.
func tryLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}

// unlock releases the lock held on file.
func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
