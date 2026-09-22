package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run on every platform. They ask the real port probe about sockets
// this test created, which is the only way to pin that the platform probe —
// lsof/ss/netstat on Unix, the TCP table API on Windows — is asked the right
// question. A stub can prove how output is interpreted; it cannot prove that the
// real tool accepts the arguments or that the right table is read.

// bindLoopback binds a socket on a loopback address and returns it together with
// its port.
//
// The socket belongs to this test process, which is what makes the assertions
// below safe on any machine: the port is one the kernel just handed out, nothing
// else is listening on it, and it is closed when the test ends.
func bindLoopback(t *testing.T, network, address string) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatalf("bind %s/%s: %v", network, address, err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener, listener.Addr().(*net.TCPAddr).Port
}

// reserveLoopbackPort returns a loopback port that was free a moment ago and is
// not held by anything when the function returns.
func reserveLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, port := bindLoopback(t, "tcp", "127.0.0.1:0")
	if err := listener.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return port
}

// wantListening asks the probe about a port and fails when the host cannot look
// at all, unless the caller accepts an unsupported host.
//
// A host without any port probe tool cannot answer anything these tests assert,
// so the test is skipped loudly instead of passing silently: a pass with zero
// assertions executed is the one outcome that hides a regression.
func wantListening(t *testing.T, port int) (PortResult, error, bool) {
	t.Helper()
	result, err := New(testTools).Listening(context.Background(), port)
	if errors.Is(err, ErrUnsupported) {
		t.Skipf("no port probe tool is installed here: %v", err)
	}
	if err != nil {
		t.Fatalf("Listening(%d): %v", port, err)
	}
	return result, nil, true
}

// TestListeningFindsARealListenerOwnedByThisProcess pins that a socket this
// process holds is reported as occupied.
//
// A probe that reads the wrong table, or that passes an argument the real tool
// rejects, answers "free" for a port that is plainly occupied — the one mistake
// that turns into a second server on the same port.
func TestListeningFindsARealListenerOwnedByThisProcess(t *testing.T) {
	_, port := bindLoopback(t, "tcp", "127.0.0.1:0")

	result, _, ok := wantListening(t, port)
	if !ok {
		// No probe tool is installed here. The fail-closed answer is what the
		// contract requires and probe_test.go covers it.
		return
	}
	if !result.Listening {
		t.Fatalf("Listening(%d) = %+v, want the socket this process holds", port, result)
	}
	// A probe that cannot attribute the socket (netstat) answers 0; anything
	// else must be this process, because nothing else can hold the socket.
	if result.PID != 0 && result.PID != os.Getpid() {
		t.Fatalf("Listening(%d).PID = %d, want 0 or this process (%d)", port, result.PID, os.Getpid())
	}
}

// TestListeningFindsAnIPv6Listener pins that the probe covers the IPv6 table as
// well as the IPv4 one.
//
// A server bound to ::1 is invisible to a probe that only reads IPv4, and the
// answer it gives — "nothing is listening" — is indistinguishable from a free
// port. That is how a start refuses a port it should have adopted, or a stop
// leaves a server behind.
func TestListeningFindsAnIPv6Listener(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		// A machine without an IPv6 loopback has nothing for the probe to find;
		// failing here would report the machine, not the code.
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	result, _, ok := wantListening(t, port)
	if !ok {
		return
	}
	if !result.Listening {
		t.Fatalf("Listening(%d) = %+v, want the IPv6 socket this process holds", port, result)
	}
	if result.PID != 0 && result.PID != os.Getpid() {
		t.Fatalf("Listening(%d).PID = %d, want 0 or this process (%d)", port, result.PID, os.Getpid())
	}
}

// assertAFreePortIsReportedFree pins the other verdict against a real socket
// table: a port nothing holds must not be reported as occupied.
//
// The kernel hands the port out again the moment the reserving socket is closed,
// so the probe is retried with a fresh port when another process won the race:
// an occupied port is a fact about the machine, not about the probe, and a test
// that failed on it would be reporting somebody else's process.
func assertAFreePortIsReportedFree(t *testing.T, host *Host, exclude int) {
	t.Helper()
	for attempt := 0; attempt < 5; attempt++ {
		port := reserveLoopbackPort(t)
		if port == exclude {
			continue
		}
		result, err := host.Listening(context.Background(), port)
		if errors.Is(err, ErrUnsupported) {
			return
		}
		if err != nil {
			t.Fatalf("Listening(%d): %v", port, err)
		}
		if result.Listening {
			// Somebody took the port between the release and the probe.
			continue
		}
		return
	}
	t.Fatal("no released loopback port stayed free long enough to be probed")
}

// TestListeningReportsARealFreePortAsFree pins the free verdict end to end.
func TestListeningReportsARealFreePortAsFree(t *testing.T) {
	assertAFreePortIsReportedFree(t, New(testTools), 0)
}

// TestListeningFollowsThePortAcrossTwoSockets pins that the port number is what
// is matched, not the socket: a listener on one port must not shadow the answer
// for another.
func TestListeningFollowsThePortAcrossTwoSockets(t *testing.T) {
	_, busy := bindLoopback(t, "tcp", "127.0.0.1:0")

	host := New(testTools)
	result, err := host.Listening(context.Background(), busy)
	if errors.Is(err, ErrUnsupported) {
		return
	}
	if err != nil {
		t.Fatalf("Listening(%d): %v", busy, err)
	}
	if !result.Listening {
		t.Fatalf("the bound port %d was reported free", busy)
	}
	assertAFreePortIsReportedFree(t, host, busy)
}

// listenHelperEnv marks the child process of the attribution test below.
//
// The value names the parent that set it, so an exported variable in the ambient
// environment cannot make the parent test process take the child branch and hang
// instead of running the test.
const listenHelperEnv = "DSHCTL_TEST_LISTEN_HELPER"

// TestListeningAttributesAPortToTheProcessThatHoldsIt is the cross-process test:
// a child process holds the socket, and the probe must name that child rather
// than this test process or nobody.
//
// Attribution is the whole value of the probe: "something listens" without an
// owner is the state a start must refuse and a stop must not touch, so a probe
// that reports the wrong pid is worse than one that reports none.
func TestListeningAttributesAPortToTheProcessThatHoldsIt(t *testing.T) {
	if os.Getenv(listenHelperEnv) == helperMarker(os.Getppid()) {
		runListenerHelper()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	command := exec.Command(executable, "-test.run=TestListeningAttributesAPortToTheProcessThatHoldsIt")
	command.Env = envForChild(listenHelperEnv + "=" + helperMarker(os.Getpid()))
	command.Stdin = nil
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	t.Cleanup(func() {
		// The child is this test's own, so ending it is safe and must happen
		// even when an assertion below fails: a leaked process would hold a
		// bound socket for the rest of the run.
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	port := readHelperPort(t, stdout)
	result, _, ok := wantListening(t, port)
	if !ok {
		return
	}
	if !result.Listening {
		t.Fatalf("Listening(%d) = %+v, want the socket the child process holds", port, result)
	}
	if result.PID != 0 && result.PID != command.Process.Pid {
		t.Fatalf("Listening(%d).PID = %d, want 0 or the holder (%d)", port, result.PID, command.Process.Pid)
	}
}

// runListenerHelper binds a loopback port, reports it, and blocks until the
// parent kills it. It never returns.
func runListenerHelper() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stdout, "ERROR %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "PORT=%d\n", listener.Addr().(*net.TCPAddr).Port)
	// Sleeping rather than parking on an empty select keeps the runtime from
	// reporting a deadlock while the parent inspects the port.
	for {
		time.Sleep(time.Hour)
	}
}

// readHelperPort reads the port the helper reported.
func readHelperPort(t *testing.T, stdout interface{ Read([]byte) (int, error) }) int {
	t.Helper()
	type answer struct {
		port int
		err  error
	}
	found := make(chan answer, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				found <- answer{err: fmt.Errorf("reading the helper's port: %w (line %q)", err, line)}
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "PORT=") {
				continue
			}
			port, convErr := strconv.Atoi(strings.TrimPrefix(line, "PORT="))
			if convErr != nil {
				found <- answer{err: fmt.Errorf("helper reported %q: %w", line, convErr)}
				return
			}
			found <- answer{port: port}
			return
		}
	}()
	select {
	case got := <-found:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.port
	case <-time.After(30 * time.Second):
		t.Fatal("the helper never reported a port")
		return 0
	}
}
