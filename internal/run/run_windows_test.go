//go:build windows

package run

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The POSIX fixtures in run_test.go skip on Windows, which left the entire
// Runner surface untested there: the process-group flag, the exit-status
// mapping, the stream collection, the output cap and the taskkill tree walk are
// all code that only exists on Windows and only ran on Unix-derived fixtures.
// These tests drive the same paths with cmd.exe only, so no POSIX shell is
// needed and nothing outside the test's own processes is touched.

// requireCmd returns the command interpreter, or skips the test.
func requireCmd(t *testing.T) string {
	t.Helper()
	command, err := LookPath("cmd")
	if err != nil {
		t.Skip("cmd.exe is unavailable")
	}
	return command
}

// TestApplyProcessGroupGivesTheChildItsOwnGroup pins the creation flag that
// keeps a console Ctrl-C from reaching the child.
//
// The value is a Windows API constant; a typo would silently put the child in
// the console's group, where an operator's Ctrl-C kills the server with the
// tool.
func TestApplyProcessGroupGivesTheChildItsOwnGroup(t *testing.T) {
	command := exec.Command(requireCmd(t), "/c", "exit 0")
	applyProcessGroup(command)
	if command.SysProcAttr == nil {
		t.Fatal("applyProcessGroup left the process attributes untouched")
	}
	if command.SysProcAttr.CreationFlags&createNewProcessGroup == 0 {
		t.Fatalf("CreationFlags = %#x, want the %#x bit", command.SysProcAttr.CreationFlags, createNewProcessGroup)
	}
}

// TestRunOnWindowsReportsTheExitStatus pins that a tool's own status survives
// the Runner, which is the contract every caller's IsExit check rests on.
func TestRunOnWindowsReportsTheExitStatus(t *testing.T) {
	command := requireCmd(t)
	err := NewRunner().Run(context.Background(), Command{Name: command, Args: []string{"/c", "exit 3"}})
	var status *ExitError
	if !errors.As(err, &status) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if status.Code != 3 {
		t.Fatalf("code = %d, want 3", status.Code)
	}
	if !IsExit(err, 3) || IsExit(err, 1) {
		t.Fatalf("IsExit must match the recorded status only: %v", err)
	}
	if !strings.Contains(err.Error(), command) {
		t.Fatalf("the error should name the command: %v", err)
	}
}

// TestRunOnWindowsRejectsAnAlreadyCancelledContext pins that no process is
// started for a request whose context is already done.
func TestRunOnWindowsRejectsAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewRunner().Run(ctx, Command{Name: requireCmd(t), Args: []string{"/c", "exit 0"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestRunOnWindowsCollectsBothStreams pins the collector, including the tee to
// the caller's writer.
func TestRunOnWindowsCollectsBothStreams(t *testing.T) {
	command := requireCmd(t)
	result := NewRunner().Capture(context.Background(), Command{
		Name: command,
		Args: []string{"/c", "echo out & echo err 1>&2"},
	})
	if result.Err != nil {
		t.Fatalf("Capture: %v", result.Err)
	}
	if result.Stdout != "out" || result.Stderr != "err" {
		t.Fatalf("result = %+v, want both streams collected", result)
	}
	if result.Truncated {
		t.Fatal("a small output must not be reported as truncated")
	}
}

// TestRunOnWindowsBoundsTheOutput pins that a runaway command cannot exhaust
// memory, with a small cap so the fixture stays cheap.
func TestRunOnWindowsBoundsTheOutput(t *testing.T) {
	command := requireCmd(t)
	runner := &Runner{OutputCap: 1024}
	// for /l %i in (1,1,200) do echo ... prints 200 lines of 44 characters.
	loop := "for /l %i in (1,1,200) do echo 0123456789012345678901234567890123456789"
	result := runner.Capture(context.Background(), Command{
		Name: command,
		Args: []string{"/c", loop},
	})
	if result.Err != nil {
		t.Fatalf("Capture: %v", result.Err)
	}
	if !result.Truncated {
		t.Fatal("output beyond the cap must be reported as truncated")
	}
	if len(result.Stdout) > 1024 {
		t.Fatalf("kept %d bytes, want at most the cap", len(result.Stdout))
	}
}

// TestRunOnWindowsCarriesStderrOnFailure pins that the tool's own diagnostic
// reaches the caller instead of a bare exit status.
func TestRunOnWindowsCarriesStderrOnFailure(t *testing.T) {
	command := requireCmd(t)
	_, err := NewRunner().Output(context.Background(), Command{
		Name: command,
		Args: []string{"/c", "echo fatal: nope 1>&2 & exit 128"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "fatal: nope") {
		t.Fatalf("error = %v, want the tool's diagnostic", err)
	}
	if !IsExit(err, 128) {
		t.Fatalf("error = %v, want the exit status preserved", err)
	}
}

// TestKillTreeEndsTheCommandTree pins the Windows equivalent of the group kill:
// taskkill /T ends the child and everything it started, which is what keeps
// pnpm's compilers from surviving a cancelled build.
//
// The child is a cmd.exe running ping for half a minute, so a kill that stops
// only the shell would leave the ping behind and the tree would look gone while
// it is not.
func TestKillTreeEndsTheCommandTree(t *testing.T) {
	command := requireCmd(t)
	child := exec.Command(command, "/c", "ping -n 30 127.0.0.1")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	// Discard rather than inherit: a console child would otherwise write its
	// output into the terminal the test suite is running in.
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		t.Fatalf("start the child: %v", err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() {
		// The tree is this test's own; a cleanup kill guards against a failed
		// assertion leaving the ping running on the machine.
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	// Give cmd.exe a moment to spawn ping, so the tree walk has a tree to walk.
	time.Sleep(200 * time.Millisecond)

	killTree(child)

	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("taskkill did not end pid %d", pid)
	}
	if processExists(pid) {
		t.Fatalf("pid %d still exists after the tree kill", pid)
	}
}

// TestKillTreeToleratesANeverStartedChild pins that the helper is safe to call
// on a command whose process was never started, which is the shape every
// deferred cleanup runs into.
func TestKillTreeToleratesANeverStartedChild(t *testing.T) {
	command := exec.Command(requireCmd(t), "/c", "exit 0")
	if command.Process != nil {
		t.Fatal("the fixture must not have started")
	}
	killTree(command)
}
