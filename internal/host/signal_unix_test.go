//go:build unix

package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// signalChildEnv marks the child process of the signal tests.
const signalChildEnv = "DSHCTL_TEST_SIGNAL_CHILD"

// TestSignalEndsARealProcess pins that a termination request reaches the kernel
// and ends a process — and that the request decides how it ends.
//
// The child is this test's own process, which is what makes the assertions safe
// on any machine: nothing outside the test is signalled, and the child blocks
// forever until it is. Both requests are checked against the wait status, so a
// "graceful" request that actually sends SIGKILL, or a "force" request that
// quietly sends SIGTERM, fails here instead of being discovered on a machine
// whose server only stops when it is killed.
func TestSignalEndsARealProcess(t *testing.T) {
	if os.Getenv(signalChildEnv) == helperMarker(os.Getppid()) {
		// The child: block until a signal ends it. Blocking in the test
		// function is deliberate — the go test framework installs no handler of
		// its own, so the default disposition applies to both signals. The loop
		// sleeps rather than parking on an empty select, so the runtime never
		// mistakes the process for deadlocked.
		for {
			time.Sleep(time.Hour)
		}
	}

	cases := []struct {
		name    string
		request Request
		signal  syscall.Signal
	}{
		{name: "graceful", request: Graceful, signal: syscall.SIGTERM},
		{name: "force", request: Force, signal: syscall.SIGKILL},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			command := startBlockingChild(t)
			pid := command.Process.Pid
			host := New()
			if !host.Alive(context.Background(), pid) {
				t.Fatalf("the child (pid %d) is not alive before the signal", pid)
			}
			if err := host.Signal(pid, testCase.request); err != nil {
				t.Fatalf("Signal(%d, %v): %v", pid, testCase.request, err)
			}
			status := waitForExit(t, command, testCase.request)
			if got := status.Signal(); got != testCase.signal {
				t.Fatalf("the child ended with %v, want %v", got, testCase.signal)
			}
			if host.Alive(context.Background(), pid) {
				t.Fatalf("the process is still alive after %v", testCase.request)
			}
		})
	}
}

// TestSignalReportsAProcessThatIsGone pins the failure mode a stale record runs
// into: the pid no longer names a process, and the caller must be told so rather
// than believe the request was delivered.
//
// The pid is above the largest pid the kernel can hand out (Linux's
// kernel.pid_max defaults to 4194304), so it can never name a process — not even
// one that appeared between the check and the signal. Signalling a pid that was
// just reaped would test the same branch while leaving a small window in which
// the kernel could have recycled the number onto a stranger.
func TestSignalReportsAProcessThatIsGone(t *testing.T) {
	// The failure must be the kernel's "no such process", not any error: the
	// caller distinguishes a recycled pid from a refused request by this
	// classification, and a stop that cannot tell them apart proceeds wrongly.
	// The two platforms report the same fact with different spellings: macOS
	// wraps ECHILD as os.ErrProcessDone, Linux answers ESRCH.
	for _, request := range []Request{Graceful, Force} {
		err := New().Signal(5_000_000, request)
		if !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("Signal(%v) error = %v, want the no-such-process classification", request, err)
		}
	}
}

// startBlockingChild starts a copy of this test binary that blocks until it is
// signalled, and registers its cleanup.
func startBlockingChild(t *testing.T) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	command := exec.Command(executable, "-test.run=TestSignalEndsARealProcess")
	command.Env = envForChild(signalChildEnv + "=" + helperMarker(os.Getpid()))
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		t.Fatalf("start the child: %v", err)
	}
	t.Cleanup(func() {
		// The child belongs to this test, so ending it is always safe. It is
		// only still running when an assertion above failed early.
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	return command
}

// waitForExit reaps the child and returns how it ended.
func waitForExit(t *testing.T, command *exec.Cmd, request Request) syscall.WaitStatus {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = command.Process.Kill()
		t.Fatalf("the child ignored %v", request)
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the child's state is %T, want a wait status", command.ProcessState.Sys())
	}
	return status
}
