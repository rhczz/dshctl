//go:build windows

package conformance

import (
	"syscall"
	"unsafe"
)

// shortPath answers the 8.3 form of a path, which is what a program under test
// sees when it prints its own working directory on Windows. The harness only
// knows the long form, so normalization needs both or the golden would record a
// path that changes with every run.
func shortPath(path string) string {
	if path == "" {
		return ""
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetShortPathNameW")
	long, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	size, _, _ := proc.Call(uintptr(unsafe.Pointer(long)), 0, 0)
	if size == 0 {
		return ""
	}
	buffer := make([]uint16, size)
	written, _, _ := proc.Call(uintptr(unsafe.Pointer(long)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if written == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}
