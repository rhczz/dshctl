//go:build unix

package host

import (
	"context"
	"os"
	"syscall"
	"testing"
)

// TestInspectionDoesNotDisturbTheProcess pins that reading process facts has no
// side effect on the process being read.
//
// The observation that can fail is made on a real child: "this test is still
// running after inspecting itself" proves nothing — the assertions could not
// execute otherwise — while a child is a process an inspection could end
// without the test noticing any other way. The kernel reports a child that
// exited or was stopped through a WNOHANG wait, and 0 while it keeps running.
//
// The self-inspection that closes the test is the cheap half: it can only fail
// if the probe answers nonsense about a process that is patently running.
func TestInspectionDoesNotDisturbTheProcess(t *testing.T) {
	// The child blocks until it is signalled, and belongs to this test: the
	// helper registers the kill that ends it, so nothing survives the test.
	command := startBlockingChild(t)
	pid := command.Process.Pid
	facts := New(testTools).Inspect(context.Background(), pid)
	if facts.PID != pid {
		t.Fatalf("Inspect(%d).PID = %d, want the process that was asked about", pid, facts.PID)
	}
	if !facts.Alive {
		t.Fatalf("pid %d is reported as gone while the child is running", pid)
	}

	// The kernel's answer, not the inspection's: a wait that has something to
	// report means the inspection ended or stopped the child it was reading.
	var status syscall.WaitStatus
	observed, err := syscall.Wait4(pid, &status, syscall.WNOHANG|syscall.WUNTRACED, nil)
	if err != nil {
		t.Fatalf("wait4(%d): %v", pid, err)
	}
	if observed != 0 {
		t.Fatalf("inspecting pid %d ended or stopped the child: status %v, signal %v", pid, status, status.Signal())
	}

	host := New(testTools)
	// Inspecting a pid that exists must not change its state.
	facts = host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("inspection reported this process as gone")
	}
	if !host.Alive(context.Background(), os.Getpid()) {
		t.Fatal("this process disappeared during inspection")
	}
}
