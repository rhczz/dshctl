//go:build windows

package logfile

import (
	"os"
	"syscall"
)

// openForFollow opens the log for the follower.
//
// The handle deliberately shares delete access as well as read and write: a log
// somebody is following must still be rotatable and deletable by whoever else
// owns it — the operator, a rotation script, or a second `dshctl logs -f`. Go's
// os.Open asks for sharing *without* FILE_SHARE_DELETE, which makes every rename
// and unlink of the file fail with "being used by another process" for as long
// as the follower holds it, so the follow would pin the log in place.
func openForFollow(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
