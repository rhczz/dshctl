//go:build unix

package run

import (
	"os/exec"
	"syscall"
)

// applyProcessGroup puts the child in a fresh process group so cancellation can
// reach everything it started.
func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree ends the child and its whole process group.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// The negative pid names the process group of a child this call started.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
