package main

import (
	"context"
	"os"
	"os/signal"
)

// signalContext installs the same handler the real process uses, so the helper
// exercises the production wiring rather than a copy of it.
func signalContext() (context.Context, context.CancelFunc) {
	signals := append([]os.Signal{os.Interrupt}, terminationSignals()...)
	return signal.NotifyContext(context.Background(), signals...)
}
