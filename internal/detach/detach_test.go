package detach

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestStartReturnsThePIDWithoutWaiting pins the core promise: a detached child
// outlives the call that created it.
func TestStartReturnsThePIDWithoutWaiting(t *testing.T) {
	shell, args := sleeper(t, 30*time.Second)
	cmd := Command(shell, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	process, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	if process.PID <= 0 {
		t.Fatalf("pid = %d", process.PID)
	}
	if process.Exited(context.Background()) {
		t.Fatal("the detached child is not running")
	}
	if !alive(process.PID) {
		t.Fatal("the detached child is not visible to the operating system")
	}
}

// TestStartReportsAMissingExecutable pins the failure path: when the executable
// is not there, Start must say so instead of handing back a handle that looks
// like a running server — and it must not have created anything the caller
// would then have to clean up.
func TestStartReportsAMissingExecutable(t *testing.T) {
	cmd := Command("dshctl-definitely-not-a-real-binary")
	process, err := Start(cmd)
	if err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	if process != nil {
		t.Fatalf("Start returned pid %d for a command that never ran", process.PID)
	}
	// A failed start creates no process at all, so there is nothing running and
	// nothing to signal: cmd.Process stays nil.
	if cmd.Process != nil {
		t.Fatalf("a failed Start left process %d behind", cmd.Process.Pid)
	}
	// The handle the caller got back must not be usable as if it were a server:
	// it reports as ended rather than blocking a wait loop.
	if !process.Exited(context.Background()) {
		t.Fatal("a handle from a failed Start must report as ended")
	}
	if !process.Wait(context.Background()) {
		t.Fatal("a handle from a failed Start must not block Wait")
	}
}

// TestStartReportsFailure pins the error path: a start that could not happen
// must be recognisable as "not found" by a caller, not just as "something went
// wrong".
func TestStartReportsFailure(t *testing.T) {
	_, err := Start(Command("dshctl-no-such-binary-at-all"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap exec.ErrNotFound", err)
	}
}

// TestExitedOnANilProcess pins that a start which never happened reads as ended,
// so a caller cannot wait forever on nothing.
func TestExitedOnANilProcess(t *testing.T) {
	var process *Process
	if !process.Exited(context.Background()) {
		t.Fatal("a nil process must report as ended")
	}
	if !process.Wait(context.Background()) {
		t.Fatal("a nil process must not block Wait")
	}
}

// TestCommandCarriesThePlatformsDetachment pins that the child gets the
// platform's detachment attributes and nothing else surprising.
func TestCommandCarriesThePlatformsDetachment(t *testing.T) {
	name, args := exitingCommand(t)
	cmd := Command(name, args...)
	if cmd.SysProcAttr == nil {
		t.Fatalf("no detachment attributes on %s", runtime.GOOS)
	}
	if Describe() == "" {
		t.Fatal("Describe must name the mechanism")
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("the command should still be runnable: %v", err)
	}
}

// TestDetachedChildDoesNotReceiveAConsoleInput pins that the server cannot hold
// the terminal dshctl was started from: Start never sets Stdin.
func TestDetachedChildDoesNotReceiveAConsoleInput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "echo.sh")
	if runtime.GOOS == "windows" {
		t.Skip("this fixture uses a POSIX shell")
	}
	// Resolved through PATH rather than hardcoded: a minimal container without
	// /bin/sh must skip this test for the machine's absence, not fail it.
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is unavailable: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntest -t 0 && echo tty || echo no-tty\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	output := filepath.Join(t.TempDir(), "out.txt")
	file, err := os.Create(output)
	if err != nil {
		t.Fatalf("create output: %v", err)
	}
	defer file.Close()

	cmd := Command(shell, script)
	cmd.Stdout = file
	cmd.Stderr = file
	process, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	waitGone(t, process.PID)
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	// The script writes exactly one word, so the file must hold it: accepting
	// "anything that is not tty" would let a child that never ran — or never
	// wrote — pass as a detached one.
	if got := strings.TrimSpace(string(data)); got != "no-tty" {
		t.Fatalf("child output = %q, want %q", got, "no-tty")
	}
}

// TestExitedIsBoundedByTheSeededGracePeriod pins what the newTimer seam is for:
// Exited gives a just-ended child a bounded grace period to be reaped, and that
// period — not the caller's whole deadline — is how long a poll of a running
// child blocks.
//
// The behaviour matters because Exited is called in wait loops: a grace that is
// not honoured turns "the server is gone" into a full timeout instead of a fast
// failure, while a grace that is not bounded makes every poll stall.
func TestExitedIsBoundedByTheSeededGracePeriod(t *testing.T) {
	name, args := sleeper(t, 30*time.Second)
	process, err := Start(Command(name, args...))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	cases := []struct {
		name  string
		grace time.Duration // how long the seeded timer takes to fire
		ctx   time.Duration // the caller's deadline
		min   time.Duration // generous lower bound on the observed wait
		max   time.Duration // generous upper bound on the observed wait
	}{
		{
			name:  "the seeded grace ends the wait",
			grace: 400 * time.Millisecond,
			ctx:   20 * time.Second,
			min:   250 * time.Millisecond,
			max:   5 * time.Second,
		},
		{
			name:  "a shorter seed returns sooner",
			grace: 5 * time.Millisecond,
			ctx:   20 * time.Second,
			min:   0,
			max:   2 * time.Second,
		},
		{
			name:  "a context that ends first still ends the wait",
			grace: 20 * time.Second,
			ctx:   300 * time.Millisecond,
			min:   200 * time.Millisecond,
			max:   5 * time.Second,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The seam is a package-level variable, so the override is scoped to
			// this subtest and restored before the child is touched again.
			original := newTimer
			t.Cleanup(func() { newTimer = original })
			newTimer = func(time.Duration) <-chan time.Time { return time.After(tc.grace) }

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctx)
			defer cancel()

			start := time.Now()
			exited := process.Exited(ctx)
			elapsed := time.Since(start)

			if exited {
				t.Fatal("Exited reported a child that is still running as ended")
			}
			if elapsed < tc.min {
				t.Fatalf("Exited returned after %v with a %v seeded grace period, want at least %v", elapsed, tc.grace, tc.min)
			}
			if elapsed > tc.max {
				t.Fatalf("Exited returned after %v, want the wait to end at %v", elapsed, tc.max)
			}
		})
	}
}

// TestExitedBlocksOnTheSeededTimerUntilTheChildEnds pins the exact contract of
// the newTimer seam: while the child runs, Exited waits on the seeded timer and
// reports "still running" the moment it fires; once the child has ended it
// answers true without consulting the timer at all.
//
// The test owns the timer channel instead of measuring wall-clock time, so a
// pass cannot be an artefact of how fast the machine is.
func TestExitedBlocksOnTheSeededTimerUntilTheChildEnds(t *testing.T) {
	name, args := sleeper(t, 30*time.Second)
	process, err := Start(Command(name, args...))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	// Cleanups run last-registered-first, so restoring the seam here means it
	// is back to the real timer before reapInCleanup looks at the child.
	original := newTimer
	t.Cleanup(func() { newTimer = original })

	gate := make(chan time.Time)
	requested := make(chan time.Duration, 1)
	newTimer = func(d time.Duration) <-chan time.Time {
		select {
		case requested <- d:
		default:
		}
		return gate
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reported := make(chan bool, 1)
	go func() { reported <- process.Exited(ctx) }()

	// Exited must ask for the package's own grace period and then block on it.
	select {
	case d := <-requested:
		if d != reapGrace {
			t.Fatalf("Exited asked newTimer for %v, want the package's reapGrace (%v)", d, reapGrace)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exited never consulted newTimer: the grace period is not the seam it documents")
	}
	select {
	case <-reported:
		t.Fatal("Exited stopped waiting before the seeded grace period fired")
	case <-time.After(200 * time.Millisecond):
	}

	// The grace fires while the child is still running: the answer is false.
	close(gate)
	select {
	case exited := <-reported:
		if exited {
			t.Fatal("Exited reported a still-running child as ended when the grace period fired")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exited ignored the seeded grace period when it fired")
	}

	// Once the child really ends the answer flips: the grace period exists to
	// catch a just-ended child, not to hold the answer back.
	if err := killProcess(process.PID); err != nil {
		t.Fatalf("kill pid %d: %v", process.PID, err)
	}
	if !process.Wait(ctx) {
		t.Fatal("the killed child was never reaped")
	}
	if !process.Exited(ctx) {
		t.Fatal("Exited reports a child that has ended as running")
	}
}

// TestAnExitOutranksAnAlreadyCancelledContext pins the order the lifecycle
// depends on: a child that has ended has ended, and a context cancelled after
// that must not make it look alive again.
//
// The bug this catches is a select over both the child's done channel and the
// context: once the context is done, Go picks uniformly at random between the
// two, so the same question about the same dead process answers "still running"
// roughly half the time. That answer is what dshctl start uses to turn a server
// that died on startup into a fast failure instead of a timeout
// (internal/app/start.go and observe.go poll the function spawnDetached
// hands them), and an answer that flips while nothing about the process has
// changed cannot be trusted for it.
//
// Each call is an independent draw, so repetition is what makes the coin flip
// impossible to miss: if the exit does not win, 200 calls all passing is a
// 2^-200 event. The wrong answers are counted rather than fatal at the first
// one, so the failure report shows the split and covers both methods.
func TestAnExitOutranksAnAlreadyCancelledContext(t *testing.T) {
	name, args := exitingCommand(t)
	process, err := Start(Command(name, args...))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	// End the child first, so the exit is a fact before the context is
	// cancelled. Waiting with a live context means only the exit can settle it.
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelWait()
	if !process.Wait(waitCtx) {
		t.Fatal("the short-lived child never ended")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // already done: only the exit is news below

	const attempts = 200
	calls := []struct {
		name   string
		report func(context.Context) bool
	}{
		{name: "Exited", report: process.Exited},
		{name: "Wait", report: process.Wait},
	}
	// Every call is counted rather than failing on the first one, so the report
	// shows the coin flip itself (roughly half the calls are wrong) and covers
	// both Exited and Wait.
	for _, call := range calls {
		wrong := 0
		for i := 0; i < attempts; i++ {
			if !call.report(cancelled) {
				wrong++
			}
		}
		if wrong > 0 {
			t.Errorf("%s reported the ended child (pid %d) as running in %d of %d calls because the context was already cancelled, want true every time",
				call.name, process.PID, wrong, attempts)
		}
	}
}

// sleeper returns a command that keeps running for about d and then ends on its
// own, so a test can hand the lifecycle a child whose lifetime it controls
// without having to signal it.
func sleeper(t *testing.T, d time.Duration) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// ping is the portable sleeper: `timeout` refuses to run without a
		// console, which is exactly what a detached child does not have. One
		// ping per second, and the first one is immediate.
		return requireCommand(t, "ping"), []string{"-n", strconv.Itoa(int(d/time.Second) + 1), "127.0.0.1"}
	}
	return requireCommand(t, "sleep"), []string{strconv.Itoa(int(d / time.Second))}
}

// exitingCommand returns a command that ends as soon as it is started: the
// fixture for tests that must look at a child which has already exited.
func exitingCommand(t *testing.T) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return requireCommand(t, "cmd"), []string{"/c", "exit 0"}
	}
	return requireCommand(t, "sleep"), []string{"0"}
}

// requireCommand finds a platform tool that the operating system is expected to
// ship, and skips only when it genuinely is not there.
func requireCommand(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is unavailable", name)
	}
	return path
}

// reapInCleanup makes a started child safe for the rest of the test run: it is
// killed if it is still running, then waited for, so a failing assertion cannot
// leave a stray server behind.
func reapInCleanup(t *testing.T, process *Process) {
	t.Helper()
	t.Cleanup(func() {
		// A handle without a usable pid must never be signalled: pid 0 is the
		// whole process group on Unix.
		if process.PID <= 0 {
			return
		}
		// done still open means the child has not been reaped, so its pid is
		// still owned by this process and can be signalled without hitting a
		// recycled pid.
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelCheck()
		if !process.Exited(checkCtx) {
			_ = killProcess(process.PID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if !process.Wait(ctx) {
			t.Errorf("the detached child (pid %d) was never reaped", process.PID)
		}
	})
}

// waitGone waits until a pid disappears, and fails the test if it does not: a
// helper that returns silently on a timeout lets a test pass because nothing
// happened.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	const timeout = 5 * time.Second
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d is still present %v after the child should have exited: it was never reaped", pid, timeout)
}
