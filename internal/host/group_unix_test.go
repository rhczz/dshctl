//go:build unix

package host

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDescendsFromUsesTheProcessGroup pins the Unix evidence: a child started
// in its own group belongs to that group — and to nothing else.
func TestDescendsFromUsesTheProcessGroup(t *testing.T) {
	command := exec.Command("sleep", "5")
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep is unavailable")
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()

	child := command.Process.Pid
	host := New()
	// The child leads its own group, which is the shape dshctl creates for the
	// wrapper, so it descends from itself.
	if !host.DescendsFrom(child, child) {
		t.Fatal("a group leader must descend from its own group")
	}
	if host.DescendsFrom(os.Getpid(), child) {
		t.Fatal("a child in its own group was reported as belonging to this process's group")
	}
	if host.DescendsFrom(0, os.Getpid()) || host.DescendsFrom(os.Getpid(), 0) {
		t.Fatal("impossible pids must never be reported as related")
	}
	// A group is only inherited: a process that is not a leader descends from
	// whoever leads its group, not from itself.
	ownGroup, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if host.DescendsFrom(os.Getpid(), os.Getpid()) != (ownGroup == os.Getpid()) {
		t.Fatal("self-descent does not agree with the process group")
	}
}

// groupChildEnv marks the child process of the group-signal tests.
const groupChildEnv = "DSHCTL_TEST_GROUP_CHILD"

// TestSignalGroupEndsARealProcessGroup pins that a group request reaches the
// kernel and how the process dies, not merely that the call returned nil.
//
// The whole stop path rests on this: dshctl starts its server in a group of its
// own precisely so that one signal reaches the wrapper and everything the wrapper
// spawned, and a "graceful" request that actually sends SIGKILL would skip the
// server's chance to flush its state without anyone noticing.
func TestSignalGroupEndsARealProcessGroup(t *testing.T) {
	if os.Getenv(groupChildEnv) == helperMarker(os.Getppid()) {
		// The child: hold the group open until it is signalled. The loop sleeps
		// rather than parking on an empty select so the runtime never mistakes the
		// process for deadlocked, and the default disposition applies to both
		// signals because the test framework installs no handler of its own.
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
			command := startBlockingGroupChild(t, "TestSignalGroupEndsARealProcessGroup")
			pid := command.Process.Pid
			host := New()
			// The group id the call will address is only meaningful if the child
			// really leads that group.
			if !host.DescendsFrom(pid, pid) {
				t.Fatalf("the child (pid %d) does not lead its own process group", pid)
			}

			if err := host.SignalGroup(pid, testCase.request); err != nil {
				t.Fatalf("SignalGroup(%d, %v): %v", pid, testCase.request, err)
			}
			status := waitForExit(t, command, testCase.request)
			if got := status.Signal(); got != testCase.signal {
				t.Fatalf("the child ended with %v, want %v", got, testCase.signal)
			}
			waitForGroupToBeGone(t, host, pid)
		})
	}
}

// TestKillGroupEndsARealProcessGroup pins the forced path on a real group: the
// process must end with SIGKILL, with no chance to run cleanup.
//
// KillGroup is what the stop path falls back to when a graceful request did not
// work, so a call that returned nil while leaving the process running would make
// the stop report success while the port stayed occupied.
func TestKillGroupEndsARealProcessGroup(t *testing.T) {
	if os.Getenv(groupChildEnv) == helperMarker(os.Getppid()) {
		for {
			time.Sleep(time.Hour)
		}
	}

	command := startBlockingGroupChild(t, "TestKillGroupEndsARealProcessGroup")
	pid := command.Process.Pid
	host := New()

	if err := host.KillGroup(pid); err != nil {
		t.Fatalf("KillGroup(%d): %v", pid, err)
	}
	status := waitForExit(t, command, Force)
	if got := status.Signal(); got != syscall.SIGKILL {
		t.Fatalf("the child ended with %v, want %v", got, syscall.SIGKILL)
	}
	waitForGroupToBeGone(t, host, pid)
}

// impossiblePID is a pid no Unix can allocate, which makes it a group id no
// process group can have: a group id is a pid of its leader. Linux caps pids at
// PID_MAX_LIMIT (1<<22) and documents pid_max as one greater than the maximum pid,
// Solaris/illumos cap pidmax at the same 4,194,304, AIX pids are 24-bit (at most
// 1<<23-1), and darwin and the BSDs allow far fewer than that. 1<<23 is therefore
// above the documented limit of every platform this package targets, so a signal
// aimed at it cannot reach anything — which is what makes the test harmless on a
// machine it did not create.
const impossiblePID = 1 << 23

// TestGroupCallsOnAGroupThatNeverExisted pins the documented idempotence.
//
// A stop runs after a crash, on a record whose processes are already gone, so "the
// group is not there" is the outcome the caller asked for, not an error to report.
// The kernel says ESRCH and the call deliberately treats that as success; anything
// else — a permission problem, an interrupted syscall — must still surface.
func TestGroupCallsOnAGroupThatNeverExisted(t *testing.T) {
	host := New()
	cases := []struct {
		name string
		call func() error
	}{
		{name: "KillGroup", call: func() error { return host.KillGroup(impossiblePID) }},
		{name: "SignalGroup graceful", call: func() error { return host.SignalGroup(impossiblePID, Graceful) }},
		{name: "SignalGroup force", call: func() error { return host.SignalGroup(impossiblePID, Force) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(); err != nil {
				t.Errorf("%s(%d) = %v, want nil: an empty group is the state the caller wanted", testCase.name, impossiblePID, err)
			}
			if host.GroupExists(impossiblePID) {
				t.Errorf("GroupExists(%d) = true for a group that never existed", impossiblePID)
			}
		})
	}
}

// TestGroupCallsRejectNonPositivePIDs pins the Unix refusal of a nonsense target.
//
// A negative pid is how the kernel addresses a group, so the call negates whatever
// it is given: passing 0 or a negative value through would address the caller's own
// process group or every process it may signal, which is the one way a mistake here
// escapes the process tree dshctl owns. Windows has no group to address and returns
// nil for these values instead, which is why the difference is pinned per platform
// rather than assumed.
func TestGroupCallsRejectNonPositivePIDs(t *testing.T) {
	host := New()
	for _, pid := range []int{0, -1} {
		err := host.KillGroup(pid)
		if err == nil {
			t.Errorf("KillGroup(%d) = nil, want a refusal", pid)
		} else if !strings.Contains(err.Error(), strconv.Itoa(pid)) {
			t.Errorf("KillGroup(%d) error = %q, want it to name the pid it refused", pid, err)
		}

		err = host.SignalGroup(pid, Graceful)
		if err == nil {
			t.Errorf("SignalGroup(%d) = nil, want a refusal", pid)
		} else if !strings.Contains(err.Error(), strconv.Itoa(pid)) {
			t.Errorf("SignalGroup(%d) error = %q, want it to name the pid it refused", pid, err)
		}
	}
	// The existence question is answered the same way: pid 0 is not a group.
	if host.GroupExists(0) {
		t.Fatal("GroupExists(0) = true, want false: pid 0 names no process group")
	}
}

// TestDescendsFromRejectsTargetsThatDoNotExist pins that ownership is never
// invented for a pid the kernel cannot resolve.
//
// DescendsFrom is the evidence that a listener belongs to the tree dshctl started,
// so a false positive means signalling something dshctl does not own, and a
// nonsense pid must answer "no" rather than falling back to a default.
func TestDescendsFromRejectsTargetsThatDoNotExist(t *testing.T) {
	host := New()
	cases := []struct {
		name     string
		ancestor int
		pid      int
	}{
		{name: "the pid does not exist", ancestor: os.Getpid(), pid: impossiblePID},
		{name: "the ancestor does not exist", ancestor: impossiblePID, pid: os.Getpid()},
		{name: "neither exists", ancestor: impossiblePID, pid: impossiblePID},
		{name: "zero ancestor", ancestor: 0, pid: os.Getpid()},
		{name: "zero pid", ancestor: os.Getpid(), pid: 0},
		{name: "negative pid", ancestor: os.Getpid(), pid: -1},
		{name: "negative ancestor", ancestor: -1, pid: os.Getpid()},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if host.DescendsFrom(testCase.ancestor, testCase.pid) {
				t.Fatalf("DescendsFrom(%d, %d) = true, want false", testCase.ancestor, testCase.pid)
			}
		})
	}
}

// startBlockingGroupChild starts a copy of this test binary as the leader of a new
// process group, and returns only once that group is observable.
//
// Two properties make the group tests safe on any machine: the child is this test's
// own process, so signalling the group it leads cannot reach anything else, and the
// group is confirmed to exist before a signal is aimed at it — SysProcAttr.Setpgid
// is applied by the child between fork and exec, so the parent can return from Start
// before the group exists, and signalling that gap would deliver nothing (ESRCH is
// treated as success) and leave the child running.
func startBlockingGroupChild(t *testing.T, testName string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	command := exec.Command(executable, "-test.run=^"+testName+"$")
	command.Env = envForChild(groupChildEnv + "=" + helperMarker(os.Getpid()))
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start the child: %v", err)
	}
	t.Cleanup(func() {
		// The child belongs to this test, so ending it is always safe and must
		// happen even when an assertion above failed: a leaked group would outlive
		// the test run. The group is killed only while the child has not been
		// reaped yet — once it has, its pid (and so its group id) may in principle
		// be recycled, and a signal aimed at a recycled group would reach a
		// process this test never created.
		if command.ProcessState == nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	pid := command.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for {
		if group, err := syscall.Getpgid(pid); err == nil && group == pid {
			return command
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child (pid %d) never formed its own process group", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitForGroupToBeGone asserts that nothing is left in the group, which is the
// question the stop path polls while it waits for the server to disappear.
//
// The wait is bounded and polled because the group is released by the kernel when
// its last member is reaped, which the test has just done but which need not be
// visible on the first read.
func waitForGroupToBeGone(t *testing.T, host *Host, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for host.GroupExists(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("group %d still has members after the signal", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
