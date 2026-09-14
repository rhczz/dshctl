//go:build windows

package detach

import "syscall"

// detachment names the Windows mechanism.
const detachment = "new process group, no console"

// Creation flags from the Windows API.
const (
	// createNewProcessGroup gives the child its own group, so a Ctrl-C in
	// dshctl's console does not reach it.
	createNewProcessGroup = 0x00000200
	// detachedProcess starts the child without inheriting a console.
	detachedProcess = 0x00000008
)

// sysProcAttr detaches the child from dshctl's console.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
