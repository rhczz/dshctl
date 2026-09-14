//go:build windows

package host

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestDescendsFromWalksTheParentChain pins the Windows evidence: the platform
// has no process group a signal can address, so descent is answered through the
// parent-pid chain of a snapshot. Without it a start on Windows could never
// recognize the server that its own pnpm shim spawned.
func TestDescendsFromWalksTheParentChain(t *testing.T) {
	if os.Getenv("DSHCTL_HOST_LINEAGE_CHILD") == helperMarker(os.Getppid()) {
		// Hold still while the parent inspects the process table.
		time.Sleep(3 * time.Second)
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestDescendsFromWalksTheParentChain$")
	command.Env = envForChild("DSHCTL_HOST_LINEAGE_CHILD=" + helperMarker(os.Getpid()))
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()

	host := New()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if host.DescendsFrom(os.Getpid(), command.Process.Pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child was not recognized as descending from this process")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if host.DescendsFrom(command.Process.Pid, os.Getpid()) {
		t.Fatal("the parent was reported as descending from its own child")
	}
	if !host.DescendsFrom(os.Getpid(), os.Getpid()) {
		t.Fatal("a process must descend from itself")
	}
}

// treeChildEnv marks the child process of the group tests below.
const treeChildEnv = "DSHCTL_HOST_TREE_CHILD"

// TestGroupCallsEndARealTree pins that both termination calls actually end the
// process they are aimed at, and that they report success only when they did.
//
// KillGroup and SignalGroup are what a stop falls back to when a graceful request
// does not work, and on Windows both go through taskkill. A call that returned nil
// without ending the tree would let the stop report success while the server kept
// serving on the port.
func TestGroupCallsEndARealTree(t *testing.T) {
	if os.Getenv(treeChildEnv) == helperMarker(os.Getppid()) {
		// The child: hold still until taskkill ends it. It never reaches the
		// assertions below, so nothing in this test runs twice.
		time.Sleep(30 * time.Second)
		return
	}

	cases := []struct {
		name string
		call func(host *Host, pid int) error
	}{
		{name: "KillGroup", call: func(host *Host, pid int) error { return host.KillGroup(pid) }},
		{name: "SignalGroup graceful", call: func(host *Host, pid int) error { return host.SignalGroup(pid, Graceful) }},
		{name: "SignalGroup force", call: func(host *Host, pid int) error { return host.SignalGroup(pid, Force) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestGroupCallsEndARealTree$")
			command.Env = envForChild(treeChildEnv + "=" + helperMarker(os.Getpid()))
			command.Stdin = nil
			command.Stdout = nil
			command.Stderr = nil
			if err := command.Start(); err != nil {
				t.Fatalf("start the child: %v", err)
			}
			t.Cleanup(func() {
				// The child is this test's own process, so ending it is always
				// safe and must happen even when an assertion above failed: a
				// leaked process would outlive the test run.
				_ = command.Process.Kill()
				_ = command.Wait()
			})

			host := New()
			pid := command.Process.Pid
			if !host.Alive(context.Background(), pid) {
				t.Fatalf("the child (pid %d) is not alive before the call", pid)
			}
			if err := testCase.call(host, pid); err != nil {
				t.Fatalf("%s(%d): %v", testCase.name, pid, err)
			}
			// The tree is gone only when the root process has really exited;
			// taskkill returns before the kernel has finished tearing it down, so
			// the deadline is a wait rather than an assumption.
			deadline := time.Now().Add(15 * time.Second)
			for host.Alive(context.Background(), pid) || host.GroupExists(pid) {
				if time.Now().After(deadline) {
					t.Fatalf("pid %d survived %s", pid, testCase.name)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

// TestGroupCallsOnANonPositivePIDDoNothing pins the intentional difference from
// Unix: Windows has no process group a signal can address, so there is nothing to
// send and the call reports success without running taskkill.
//
// Unix refuses these values with an error because a negative pid is how the kernel
// addresses a group there, and passing one through would reach the caller's own
// group. Pinning both halves keeps the asymmetry visible: a caller may not rely on
// an error from a pid that names no tree on Windows.
func TestGroupCallsOnANonPositivePIDDoNothing(t *testing.T) {
	host := New()
	for _, pid := range []int{0, -1} {
		if err := host.KillGroup(pid); err != nil {
			t.Errorf("KillGroup(%d) = %v, want nil: there is no group to end on Windows", pid, err)
		}
		if err := host.SignalGroup(pid, Force); err != nil {
			t.Errorf("SignalGroup(%d) = %v, want nil: there is no group to end on Windows", pid, err)
		}
		if host.GroupExists(pid) {
			t.Errorf("GroupExists(%d) = true, want false", pid)
		}
	}
}
