package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRunReportsTheExitStatus pins that a tool's own status survives.
func TestRunReportsTheExitStatus(t *testing.T) {
	shell := requireShell(t)
	err := NewRunner().Run(context.Background(), Command{Name: shell, Args: []string{"-c", "exit 3"}})
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
	if !strings.Contains(err.Error(), shell) {
		t.Fatalf("error should name the command: %v", err)
	}
}

// TestRunReportsAMissingExecutable pins the not-found classification.
func TestRunReportsAMissingExecutable(t *testing.T) {
	err := NewRunner().Run(context.Background(), Command{Name: "dshctl-no-such-tool"})
	if !IsNotFound(err) {
		t.Fatalf("a missing executable must be recognizable: %v", err)
	}
}

// TestRunRejectsAnAlreadyCancelledContext pins that no process is started for a
// cancelled request.
func TestRunRejectsAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shell := requireShell(t)
	err := NewRunner().Run(ctx, Command{Name: shell, Args: []string{"-c", "exit 0"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestRunCancelReachesTheWholeProcessTree pins the behaviour pnpm needs: a tool
// that started helpers must not leave them behind holding the build directory.
//
// The grandchild sleeps far longer than this test's patience, so it cannot
// expire on its own while a broken cancellation waits: what the test measures is
// the kill, not the clock. The shell's `wait` must not be what ends the run
// either — the grandchild's disappearance is asserted on its own, before the
// call is allowed to report anything.
func TestRunCancelReachesTheWholeProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fixture uses a POSIX shell")
	}
	shell := requireShell(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	finished := make(chan error, 1)
	go func() {
		finished <- NewRunner().Run(ctx, Command{
			Name:   shell,
			Args:   []string{"-c", "sleep 600 & echo $! > " + pidFile + "; wait"},
			Stdout: io.Discard,
			Stderr: io.Discard,
		})
	}()

	grandchild := waitForPID(t, pidFile)
	// The cleanup is the test's own, not the code under test: a cancellation
	// that failed must not leave a ten-minute sleeper behind for the rest of
	// the run.
	t.Cleanup(func() { killProcess(t, shell, grandchild) })
	cancel()

	if !waitForGone(grandchild, 5*time.Second) {
		t.Fatalf("grandchild %d survived the cancellation, want the whole process tree ended", grandchild)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled run must report cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the cancellation, want it to report context.Canceled")
	}
}

// TestCaptureCollectsBothStreams pins the collector, including that it tees to
// the caller's writers instead of ignoring them.
func TestCaptureCollectsBothStreams(t *testing.T) {
	shell := requireShell(t)
	var tee bytes.Buffer
	result := NewRunner().Capture(context.Background(), Command{
		Name:   shell,
		Args:   []string{"-c", "echo out; echo err 1>&2"},
		Stdout: &tee,
	})
	if result.Err != nil {
		t.Fatalf("Capture: %v", result.Err)
	}
	if result.Stdout != "out" || result.Stderr != "err" {
		t.Fatalf("result = %+v, want both streams collected", result)
	}
	if !strings.Contains(tee.String(), "out") {
		t.Fatalf("the caller's writer received %q, want the teed output", tee.String())
	}
	if result.Truncated {
		t.Fatal("a small output must not be reported as truncated")
	}
}

// TestCaptureBoundsTheOutput pins that a runaway command cannot exhaust memory.
func TestCaptureBoundsTheOutput(t *testing.T) {
	shell := requireShell(t)
	runner := &Runner{OutputCap: 1024}
	result := runner.Capture(context.Background(), Command{
		Name: shell,
		Args: []string{"-c", "i=0; while [ $i -lt 200 ]; do echo 0123456789012345678901234567890123456789; i=$((i+1)); done"},
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

// TestOutputCarriesStderrOnFailure pins that the tool's own diagnostic reaches
// the caller instead of a bare exit status.
func TestOutputCarriesStderrOnFailure(t *testing.T) {
	shell := requireShell(t)
	_, err := NewRunner().Output(context.Background(), Command{
		Name: shell,
		Args: []string{"-c", "echo 'fatal: nope' 1>&2; exit 128"},
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

// TestOutputReturnsStdoutOnSuccess pins the ordinary path.
func TestOutputReturnsStdoutOnSuccess(t *testing.T) {
	shell := requireShell(t)
	out, err := NewRunner().Output(context.Background(), Command{Name: shell, Args: []string{"-c", "echo '  value  '"}})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if out != "value" {
		t.Fatalf("output = %q, want it trimmed", out)
	}
}

// TestWithPathPrefix pins the environment helper.
func TestWithPathPrefix(t *testing.T) {
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/bin")
	env := WithPathPrefix("/opt/node/bin")
	want := "/opt/node/bin" + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"
	if got := pathOf(env); got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
	if got := pathOf(WithPathPrefix("")); got != "/usr/bin"+string(os.PathListSeparator)+"/bin" {
		t.Fatalf("an empty prefix changed PATH to %q", got)
	}
}

// TestWithPathPrefixWithoutAnExistingPath pins that the result stays honest when
// the environment has no PATH at all.
func TestWithPathPrefixWithoutAnExistingPath(t *testing.T) {
	env := withoutPath(os.Environ())
	env = append(env, "DSHCTL_TEST=1")
	prefixed := withPathPrefix(env, "/opt/node/bin")
	if got := pathOf(prefixed); got != "/opt/node/bin" {
		t.Fatalf("PATH = %q, want only the prefix", got)
	}
	if !contains(prefixed, "DSHCTL_TEST=1") {
		t.Fatal("the other environment entries must survive")
	}
}

// TestWithPathPrefixMatchesTheKeyCaseInsensitively pins the Windows spelling:
// an environment whose PATH entry is spelled "Path" must be prefixed in place,
// not shadowed by a second entry that would win the case-insensitive dedup and
// replace the whole PATH with dir alone.
func TestWithPathPrefixMatchesTheKeyCaseInsensitively(t *testing.T) {
	env := []string{"Path=C:\\Windows", "SystemRoot=C:\\Windows"}
	prefixed := withPathPrefix(env, "C:\node")
	if len(prefixed) != 2 {
		t.Fatalf("env = %v, want the same two entries", prefixed)
	}
	want := "Path=C:\node" + string(os.PathListSeparator) + "C:\\Windows"
	if prefixed[0] != want {
		t.Fatalf("PATH entry = %q, want %q", prefixed[0], want)
	}
	if prefixed[1] != "SystemRoot=C:\\Windows" {
		t.Fatalf("unrelated entry changed: %q", prefixed[1])
	}
}

// TestCommandString pins the diagnostic rendering.
func TestCommandString(t *testing.T) {
	cmd := Command{Name: "git", Args: []string{"-C", "/repo", "pull", "--ff-only"}}
	if cmd.String() != "git -C /repo pull --ff-only" {
		t.Fatalf("String = %q", cmd.String())
	}
}

// TestCollectorPrefersTheToolItself pins that a Runner is used as-is and a
// Capturer is adapted, so a test double is never bypassed.
func TestCollectorPrefersTheToolItself(t *testing.T) {
	runner := NewRunner()
	if got := Collector(runner); got != Outputer(runner) {
		t.Fatalf("Collector(*Runner) = %T, want the runner itself", got)
	}
	fake := &fakeCapturer{result: Result{Stdout: "from the fake"}}
	out, err := NewCollector(fake).Output(context.Background(), Command{Name: "git"})
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if out != "from the fake" {
		t.Fatalf("output = %q, want the fake's answer", out)
	}
	if !fake.called {
		t.Fatal("the fake was bypassed: a test double must never fall through to the real tool")
	}
}

// fakeCapturer records that it was asked and answers with a canned result.
type fakeCapturer struct {
	result Result
	called bool
}

// Capture implements Capturer.
func (f *fakeCapturer) Capture(context.Context, Command) Result {
	f.called = true
	return f.result
}

// TestBoundedBuffer pins the cap arithmetic directly, including the boundary.
func TestBoundedBuffer(t *testing.T) {
	buffer := &boundedBuffer{limit: 8}
	if n, err := buffer.Write([]byte("12345")); err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v)", n, err)
	}
	if _, err := buffer.Write([]byte("67890")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buffer.String() != "12345678" {
		t.Fatalf("kept %q, want the first 8 bytes", buffer.String())
	}
	if !buffer.truncated {
		t.Fatal("dropping bytes must set the truncated flag")
	}
	exact := &boundedBuffer{limit: 5}
	if _, err := exact.Write([]byte("12345")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if exact.truncated {
		t.Fatal("writing exactly the cap must not be reported as truncated")
	}
}

// requireShell returns a POSIX shell, or skips the test.
func requireShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these fixtures use a POSIX shell")
	}
	shell, err := LookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}
	return shell
}

// waitForGone polls until a pid no longer exists, or the budget runs out.
func waitForGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !processExists(pid)
}

// killProcess ends a pid the fixture started, through the shell the fixture
// already resolved. It is the test's own cleanup: production code must end the
// tree on its own, and this is what keeps a failed cancellation from leaving a
// child behind.
func killProcess(t *testing.T, shell string, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	_ = exec.Command(shell, "-c", "kill -9 "+strconv.Itoa(pid)).Run()
}

// waitForPID reads the pid a scripted shell wrote, or fails the test.
func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the helper process never reported its pid in %s", path)
	return 0
}

// pathEntry reports whether entry is a PATH entry, whatever the key's casing.
//
// The environment the tests build is matched the way the implementation matches
// it — case-insensitively — because Windows shells spell the key "Path" and
// t.Setenv preserves the ambient entry's original casing.
func pathEntry(entry string) bool {
	key, _, found := strings.Cut(entry, "=")
	return found && strings.EqualFold(key, "PATH")
}

// pathOf returns the PATH entry of an environment, or an empty string.
func pathOf(env []string) string {
	for _, entry := range env {
		if pathEntry(entry) {
			return entry[strings.IndexByte(entry, '=')+1:]
		}
	}
	return ""
}

// withoutPath removes every PATH entry, whatever the key's casing.
func withoutPath(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !pathEntry(entry) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// contains reports whether a slice holds a value.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestReapedGuardDoesNotKillAfterTheChildWasReaped pins the guard that keeps a
// cancellation from signalling a process group whose id may already belong to
// somebody else. It is deterministic on purpose: the race it protects against
// cannot be reproduced on demand, but the decision can be tested directly.
func TestReapedGuardDoesNotKillAfterTheChildWasReaped(t *testing.T) {
	guard := &reapedGuard{}
	killed := false
	guard.killIfLive(func() { killed = true })
	if !killed {
		t.Fatal("a child that has not been reaped must still be killed on cancellation")
	}

	guard.markReaped()
	killed = false
	guard.killIfLive(func() { killed = true })
	if killed {
		t.Fatal("a reaped child's process group must never be signalled")
	}
}
