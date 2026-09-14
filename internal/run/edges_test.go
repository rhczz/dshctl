package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// These edges are platform-neutral shapes of the Runner surface: a working
// directory that cannot exist, and receivers that were never constructed. They
// live here rather than in the shell-based fixtures so they run on every
// platform, including Windows, where those fixtures skip.

// TestRunRejectsAnImpossibleWorkingDirectory pins the start-failure shape a
// build directory that vanished produces: the caller must be told the command
// never ran, and a start failure must not be mistaken for an exit status.
func TestRunRejectsAnImpossibleWorkingDirectory(t *testing.T) {
	// A real command, so the directory is the only thing that can fail: with a
	// missing executable the test would pass on the tool failure instead of
	// pinning the directory.
	missing := filepath.Join(t.TempDir(), "missing-directory")
	err := NewRunner().Run(context.Background(), Command{
		Name: os.Args[0],
		Args: []string{"-test.run=^$"},
		Dir:  missing,
	})
	if err == nil {
		t.Fatal("a run in a directory that cannot exist must fail")
	}
	// The failure is a start failure: neither an exit status nor the
	// "executable not found" classification, both of which would mislead the
	// caller about what went wrong.
	if IsExit(err, 0) {
		t.Fatalf("a start failure must not be reported as an exit status: %v", err)
	}
	if IsNotFound(err) {
		t.Fatalf("a directory failure was reported as a missing executable: %v", err)
	}
}

// TestRunnerNilReceiversStayUsable pins the nil-receiver paths the production
// call sites rely on: the Runner is documented to tolerate a nil receiver, and
// a panic there would take down an operation that only wanted to report a
// failure.
//
// The command does not exist, so the test asserts the answer shape — a
// classified not-found error, never a panic.
func TestRunnerNilReceiversStayUsable(t *testing.T) {
	missing := Command{Name: "dshctl-no-such-tool"}
	var runner *Runner

	if err := runner.Run(context.Background(), missing); !IsNotFound(err) {
		t.Fatalf("a nil *Runner must classify a missing tool, got %v", err)
	}
	if result := runner.Capture(context.Background(), missing); !IsNotFound(result.Err) {
		t.Fatalf("a nil *Runner must classify a missing tool in Capture, got %v", result.Err)
	}
	if _, err := runner.Output(context.Background(), missing); !IsNotFound(err) {
		t.Fatalf("a nil *Runner must classify a missing tool in Output, got %v", err)
	}

	// The zero-value runner behaves the same, with the documented default cap.
	zero := &Runner{}
	if zero.OutputCap != 0 {
		t.Fatalf("a zero Runner carries an output cap of %d, want none", zero.OutputCap)
	}
	if err := zero.Run(context.Background(), missing); !IsNotFound(err) {
		t.Fatalf("a zero Runner must classify a missing tool, got %v", err)
	}
}
