//go:build unix

package detach

import "syscall"

// detachment names the Unix mechanism.
const detachment = "setsid"

// sysProcAttr makes the child a session leader with no controlling terminal.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
