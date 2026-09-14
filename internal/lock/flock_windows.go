//go:build windows

package lock

import (
	"os"
	"syscall"
	"unsafe"
)

// Lock byte range. One byte is enough and keeps the record readable.
const (
	lockBytesLow  = 1
	lockBytesHigh = 0
	// lockfileFailImmediately makes the call non-blocking.
	lockfileFailImmediately = 0x00000001
	// lockfileExclusiveLock asks for an exclusive range.
	lockfileExclusiveLock = 0x00000002
	// errorLockViolation is what a lost race reports.
	errorLockViolation = 33
)

var (
	modKernel32Lock = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = modKernel32Lock.NewProc("LockFileEx")
	procUnlockFile  = modKernel32Lock.NewProc("UnlockFileEx")
)

// tryLock attempts to take the lock without blocking.
func tryLock(file *os.File) (bool, error) {
	var overlapped syscall.Overlapped
	ok, _, callErr := procLockFileEx.Call(
		file.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		lockBytesLow,
		lockBytesHigh,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if ok != 0 {
		return true, nil
	}
	if callErr == syscall.Errno(errorLockViolation) {
		return false, nil
	}
	if errno, isErrno := callErr.(syscall.Errno); isErrno && errno == 0 {
		return false, nil
	}
	return false, callErr
}

// unlock releases the lock held on file.
func unlock(file *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, callErr := procUnlockFile.Call(
		file.Fd(),
		0,
		lockBytesLow,
		lockBytesHigh,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if ok == 0 {
		return callErr
	}
	return nil
}
