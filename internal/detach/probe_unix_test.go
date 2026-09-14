//go:build unix

package detach

import (
	"os"
	"syscall"
)

// alive reports whether a pid exists.
func alive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// killProcess ends a pid.
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
