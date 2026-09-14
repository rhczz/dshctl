package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// fakeExecutor is a test double that can only run commands, which is the
// smallest capability an injected executor may have.
type fakeExecutor struct {
	commands []Command
	err      error
}

// Run implements Executor.
func (f *fakeExecutor) Run(_ context.Context, cmd Command) error {
	f.commands = append(f.commands, cmd)
	return f.err
}

// TestCollectorHonoursAPlainExecutor pins the other half of "a test double is
// never bypassed".
//
// The existing test covers a double that implements Capturer. An executor that
// implements only Run is the smaller capability, and adapting it must still ask
// the double rather than reach past it: a caller that injected an executor has
// said where commands go, and a collection path that quietly runs a real process
// instead makes every test above it measure the machine rather than the code.
//
// The command name does not exist, so even the bypass this test is looking for
// cannot touch anything real.
func TestCollectorHonoursAPlainExecutor(t *testing.T) {
	fake := &fakeExecutor{}
	out, err := Collector(fake).Output(context.Background(), Command{Name: "dshctl-no-such-tool-4f2a"})
	if err != nil {
		t.Fatalf("Output through an injected executor: %v", err)
	}
	if len(fake.commands) != 1 {
		t.Fatalf("the injected executor saw %d commands, want 1: the call ran a real process instead",
			len(fake.commands))
	}
	if out != "" {
		t.Fatalf("output = %q, want the empty output a plain executor can offer", out)
	}
}

// TestCollectorReportsAPlainExecutorsFailure pins that the failure an injected
// executor reports is the failure the caller sees, rather than an error invented
// by a different command path.
func TestCollectorReportsAPlainExecutorsFailure(t *testing.T) {
	failure := errors.New("the fake refused")
	fake := &fakeExecutor{err: failure}
	if _, err := Collector(fake).Output(context.Background(), Command{Name: "dshctl-no-such-tool-4f2a"}); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the executor's own failure", err)
	}
}

// collectorHelperEnv marks the child process of the fallback test below. The
// value names the parent that set it, so an exported variable in the ambient
// environment cannot make the parent test process take the child branch.
const collectorHelperEnv = "DSHCTL_TEST_COLLECTOR_HELPER"

// TestNewCollectorWithoutACapturerStillAnswers pins the documented fallback: a
// caller that has no capturer gets a working runner, so the helper can never
// return nil.
//
// The command is this test binary re-executed as a helper that prints one known
// line, which is a real process that needs no tool installed on the machine.
func TestNewCollectorWithoutACapturerStillAnswers(t *testing.T) {
	if os.Getenv(collectorHelperEnv) == "child-of-"+strconv.Itoa(os.Getppid()) {
		fmt.Fprintln(os.Stdout, "collector-helper-ok")
		os.Exit(0)
	}

	out, err := NewCollector(nil).Output(context.Background(), Command{
		Name: os.Args[0],
		Args: []string{"-test.run=^TestNewCollectorWithoutACapturerStillAnswers$"},
		Env:  collectorChildEnv(collectorHelperEnv + "=child-of-" + strconv.Itoa(os.Getpid())),
	})
	if err != nil {
		t.Fatalf("Output through the fallback runner: %v", err)
	}
	if !strings.Contains(out, "collector-helper-ok") {
		t.Fatalf("output = %q, want the helper's line: the fallback runner did not run the command", out)
	}
}

// collectorChildEnv returns the parent environment with any ambient helper
// marker removed before the real one is appended. Unix resolves a duplicated
// environment key to its first occurrence, so an exported
// DSHCTL_TEST_COLLECTOR_HELPER would otherwise shadow the appended value and
// the child would re-run the test instead of taking the helper branch.
func collectorChildEnv(extra ...string) []string {
	environment := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, collectorHelperEnv+"=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, extra...)
}

// TestIsExitClassifiesOnlyRealExitStatuses pins the boundary the callers branch
// on: a nil error is not an exit status, and a wrapped one still is.
func TestIsExitClassifiesOnlyRealExitStatuses(t *testing.T) {
	if IsExit(nil, 0) {
		t.Fatal("IsExit(nil, 0) = true: no command ran, so there is no status")
	}
	if IsExit(nil, 1) {
		t.Fatal("IsExit(nil, 1) = true")
	}
	if IsExit(&ExitError{Command: "git", Code: 2}, 0) {
		t.Fatal("a non-zero status was reported as a successful exit")
	}
	if !IsExit(&ExitError{Command: "git", Code: 2}, 2) {
		t.Fatal("a matching exit status was not recognized")
	}
	wrapped := fmt.Errorf("git ls-files 失败: %w", &ExitError{Command: "git", Code: 3})
	if !IsExit(wrapped, 3) {
		t.Fatal("a wrapped exit status was not recognized")
	}
	other := errors.New("boom")
	if IsExit(other, 1) {
		t.Fatal("an unrelated error was reported as an exit status")
	}
}

// TestIsNotFoundClassifiesOnlyLookupFailures pins that "the tool is not
// installed" is recognized through wrapping and is never confused with a tool
// that ran and failed.
func TestIsNotFoundClassifiesOnlyLookupFailures(t *testing.T) {
	if IsNotFound(nil) {
		t.Fatal("IsNotFound(nil) = true")
	}
	if !IsNotFound(fmt.Errorf("run git: %w", exec.ErrNotFound)) {
		t.Fatal("a wrapped lookup failure was not recognized")
	}
	if IsNotFound(&ExitError{Command: "git", Code: 127}) {
		t.Fatal("a tool that ran and failed was reported as missing")
	}
}

// TestCaptureAdapterKeepsTheToolsDiagnostic pins how collected output becomes an
// error: the tool's stderr is carried inside it, and a silent failure stays the
// bare error rather than gaining an empty suffix.
func TestCaptureAdapterKeepsTheToolsDiagnostic(t *testing.T) {
	failure := errors.New("exit status 2")

	withStderr := captureAdapter{&fakeCapturer{result: Result{Stdout: "partial", Stderr: "fatal: not a git repository", Err: failure}}}
	out, err := withStderr.Output(context.Background(), Command{Name: "git"})
	if out != "partial" {
		t.Fatalf("output = %q, want what the tool printed before failing", out)
	}
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want the tool's own diagnostic inside it", err)
	}

	silent := captureAdapter{&fakeCapturer{result: Result{Err: failure}}}
	out, err = silent.Output(context.Background(), Command{Name: "git"})
	if out != "" {
		t.Fatalf("output = %q, want nothing", out)
	}
	if err == nil || err.Error() != failure.Error() {
		t.Fatalf("err = %v, want the bare failure with no invented detail", err)
	}

	success := captureAdapter{&fakeCapturer{result: Result{Stdout: "ok"}}}
	if _, err := success.Output(context.Background(), Command{Name: "git"}); err != nil {
		t.Fatalf("a successful capture must not produce an error: %v", err)
	}
}
