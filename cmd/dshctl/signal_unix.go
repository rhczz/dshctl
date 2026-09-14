//go:build unix

package main

import (
	"os"
	"syscall"
)

// terminationSignals are the signals that end the process gracefully.
//
// The tool targets Unix and Windows only, so the two files that define this are
// the whole story: a catch-all for other platforms would be code that can never
// be compiled, let alone tested.
func terminationSignals() []os.Signal { return []os.Signal{syscall.SIGTERM} }
