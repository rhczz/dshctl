//go:build windows

package detach

import (
	"os"
	"syscall"
	"unsafe"
)

// processQueryLimitedInformation is enough to test for a live process.
const processQueryLimitedInformation = 0x1000

// stillActive is the exit code Windows reports for a running process.
const stillActive = 259

var (
	probeKernel32     = syscall.NewLazyDLL("kernel32.dll")
	procProbeOpen     = probeKernel32.NewProc("OpenProcess")
	procProbeExitCode = probeKernel32.NewProc("GetExitCodeProcess")
	procProbeClose    = probeKernel32.NewProc("CloseHandle")
)

// alive reports whether a pid exists and has not exited.
func alive(pid int) bool {
	handle, _, _ := procProbeOpen.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procProbeClose.Call(handle)
	var code uint32
	ok, _, _ := procProbeExitCode.Call(handle, uintptr(unsafe.Pointer(&code)))
	if ok == 0 {
		return false
	}
	return code == stillActive
}

// killProcess ends a pid.
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
