//go:build unix

package run

import (
	"os"
	"syscall"
)

// processExists reports whether a pid names a live process.
//
// Unix answers through signal 0, which never delivers a signal and is refused
// for a pid that does not exist. This is the test-side probe only; production
// code never guesses liveness this way.
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
