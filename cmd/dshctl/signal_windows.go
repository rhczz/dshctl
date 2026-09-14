//go:build windows

package main

import (
	"os"
	"syscall"
)

// terminationSignals are the signals that end the process gracefully.
//
// Windows delivers SIGTERM from services and taskkill; os.Interrupt is already
// handled at the call site.
func terminationSignals() []os.Signal { return []os.Signal{syscall.SIGTERM} }
