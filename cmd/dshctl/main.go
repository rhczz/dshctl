// Command dshctl manages the DeepSeek Harness Web server on the local machine:
// it starts and stops the background server, builds and updates the checkout,
// and reports what is running.
//
// The command line itself lives in internal/cli. This file is the process
// boundary: it installs the signal handler and turns a cancelled context into a
// process exit status, and nothing else.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/rhczz/dshctl/internal/cli"
)

func main() {
	// The signal handler and the exit are in one function so that the whole
	// process boundary is testable: a test can drive run with a context it
	// controls and observe the status the process would have exited with.
	signals := append([]os.Signal{os.Interrupt}, terminationSignals()...)
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// run executes the command line and returns the process exit status.
//
// Parameters:
//   - ctx: cancelled by a termination signal or by the caller.
//   - args: arguments after the program name.
//   - stdout, stderr: the process streams.
//   - getenv: environment lookup.
//
// Returns:
//   - the exit status the process must use.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return cli.Main(ctx, args, stdout, stderr, getenv)
}
