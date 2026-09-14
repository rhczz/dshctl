//go:build windows

package run

import (
	"syscall"
	"unsafe"
)

// processExists reports whether a pid names a live process.
//
// os.Process.Signal cannot serve as the probe here: Windows refuses every
// non-Kill signal with syscall.EWINDOWS whether the process is alive or not,
// so a signal-based probe would be permanently false. The real answer is the
// exit code, which stays STILL_ACTIVE (259) until the process ends.
var (
	modKernel32Run            = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessRun        = modKernel32Run.NewProc("OpenProcess")
	procGetExitCodeProcessRun = modKernel32Run.NewProc("GetExitCodeProcess")
	procCloseHandleRun        = modKernel32Run.NewProc("CloseHandle")
)

const (
	processQueryLimitedInformationRun = 0x1000
	stillActiveRun                    = 259
)

func processExists(pid int) bool {
	handle, _, _ := procOpenProcessRun.Call(processQueryLimitedInformationRun, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procCloseHandleRun.Call(handle)

	var code uint32
	ok, _, _ := procGetExitCodeProcessRun.Call(handle, uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == stillActiveRun
}
