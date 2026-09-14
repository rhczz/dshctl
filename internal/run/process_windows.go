//go:build windows

package run

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNewProcessGroup gives the child its own group so a console Ctrl-C does
// not reach it.
const createNewProcessGroup = 0x00000200

// applyProcessGroup puts the child in a fresh process group.
func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// killTree ends the child and its descendants that taskkill can reach.
//
// Windows has no process-group signal. taskkill walks the child's tree, which is
// what keeps pnpm's compilers from surviving a cancelled build.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
