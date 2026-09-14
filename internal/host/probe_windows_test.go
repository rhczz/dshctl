//go:build windows

package host

import (
	"context"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestListeningFindsThisProcess pins that the native port table answers for a
// socket this process holds, without any external tool.
func TestListeningFindsThisProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	result, err := New().Listening(context.Background(), port)
	if err != nil {
		t.Fatalf("Listening: %v", err)
	}
	if !result.Listening {
		t.Fatalf("port %d is listening but was reported free", port)
	}
	if result.PID != os.Getpid() {
		t.Fatalf("pid = %d, want this process %d", result.PID, os.Getpid())
	}
}

// TestListeningReportsAFreePort pins the negative case.
//
// The kernel hands a released port out again immediately, so the probe is
// retried with a fresh port when another process won the race: an occupied port
// is a fact about the machine, not a failure of the probe.
func TestListeningReportsAFreePort(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		result, err := New().Listening(context.Background(), port)
		if err != nil {
			t.Fatalf("Listening: %v", err)
		}
		if result.Listening {
			continue
		}
		return
	}
	t.Fatal("no released loopback port stayed free long enough to be probed")
}

// TestInspectReportsThisProcess pins the process facts Windows can answer.
func TestInspectReportsThisProcess(t *testing.T) {
	facts := New().Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("this process must be reported as alive")
	}
	if facts.StartedAt <= 0 {
		t.Fatal("the start time must be readable: it is the PID-reuse fingerprint")
	}
	if facts.Command == "" {
		t.Fatal("the image path must be readable")
	}
}

// TestAliveRejectsNonsense pins that impossible pids never reach the API.
func TestAliveRejectsNonsense(t *testing.T) {
	host := New()
	for _, pid := range []int{0, -1} {
		if host.Alive(context.Background(), pid) {
			t.Fatalf("Alive(%d) = true", pid)
		}
		if facts := host.Inspect(context.Background(), pid); facts.Alive {
			t.Fatalf("Inspect(%d).Alive = true", pid)
		}
		if err := host.Signal(pid, Force); err == nil {
			t.Fatalf("Signal(%d) must be refused", pid)
		}
	}
}

// TestSignalEndsARealChild pins the termination path end to end: the handle
// Signal opens must carry PROCESS_TERMINATE, or every stop on Windows fails
// with ERROR_ACCESS_DENIED while the server keeps running.
func TestSignalEndsARealChild(t *testing.T) {
	if os.Getenv("DSHCTL_HOST_SIGNAL_CHILD") == helperMarker(os.Getppid()) {
		time.Sleep(30 * time.Second)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestSignalEndsARealChild$")
	command.Env = envForChild("DSHCTL_HOST_SIGNAL_CHILD=" + helperMarker(os.Getpid()))
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() { _ = command.Wait() }()

	host := New()
	pid := command.Process.Pid
	if !host.Alive(context.Background(), pid) {
		t.Fatal("the child is not reported as alive before the signal")
	}
	if err := host.Signal(pid, Force); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for host.Alive(context.Background(), pid) {
		if time.Now().After(deadline) {
			t.Fatal("the child survived a force signal")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestGroupExistsTracksTheRootProcess pins the Windows answer to "is the tree
// still there": it follows the root process, which is what taskkill ends last.
func TestGroupExistsTracksTheRootProcess(t *testing.T) {
	if os.Getenv("DSHCTL_HOST_GROUP_CHILD") == helperMarker(os.Getppid()) {
		time.Sleep(30 * time.Second)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestGroupExistsTracksTheRootProcess$")
	command.Env = envForChild("DSHCTL_HOST_GROUP_CHILD=" + helperMarker(os.Getpid()))
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	host := New()
	pid := command.Process.Pid
	if !host.GroupExists(pid) {
		t.Fatal("a live root process must count as an existing tree")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_ = command.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for host.GroupExists(pid) {
		if time.Now().After(deadline) {
			t.Fatal("a reaped root process must not count as an existing tree")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if host.GroupExists(0) {
		t.Fatal("pid 0 must never be reported as an existing tree")
	}
}

// TestListeningFindsAnIPv6OnlyListener pins the second address family: a
// listener that only exists on ::1 must not be reported as a free port.
func TestListeningFindsAnIPv6OnlyListener(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	result, err := New().Listening(context.Background(), port)
	if err != nil {
		t.Fatalf("Listening: %v", err)
	}
	if !result.Listening {
		t.Fatalf("port %d is listening on ::1 but was reported free", port)
	}
	if result.PID != os.Getpid() {
		t.Fatalf("pid = %d, want this process %d", result.PID, os.Getpid())
	}
}
