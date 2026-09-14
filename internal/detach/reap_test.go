//go:build unix

package detach

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestExitedReportsAFinishedChild pins the property the whole lifecycle depends
// on: once a detached child has ended, it stops answering existence checks.
//
// The failure this guards against is subtle and was real. A child that is
// started and never waited for stays in the process table as a zombie, and a
// zombie still answers signal 0 — so "is the process I started still alive?"
// answers yes forever, and every wait loop built on it runs to its full timeout
// instead of failing at once.
func TestExitedReportsAFinishedChild(t *testing.T) {
	shell := requireShellBinary(t)

	dir := t.TempDir()
	marker := dir + "/exit-now"
	script := dir + "/wait-for-marker.sh"
	body := "#!/bin/sh\nwhile [ ! -f " + marker + " ]; do sleep 0.01; done\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	process, err := Start(Command(shell, script))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	if process.Exited(context.Background()) {
		t.Fatal("a freshly started child is reported as ended")
	}

	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("release the child: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if process.Exited(context.Background()) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !process.Exited(context.Background()) {
		t.Fatal("the child ended but Exited still reports it running")
	}

	// The reaped child must not be visible to the operating system at all.
	// This is the check that fails when the process is left as a zombie.
	if aliveBySignal(process.PID) {
		t.Fatalf("pid %d still exists after the child ended: it was never reaped", process.PID)
	}
}

// TestWaitBlocksUntilTheChildEnds pins the blocking form of the same question.
func TestWaitBlocksUntilTheChildEnds(t *testing.T) {
	shell := requireShellBinary(t)
	dir := t.TempDir()
	marker := dir + "/go"
	script := dir + "/sleep.sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile [ ! -f "+marker+" ]; do sleep 0.01; done\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	process, err := Start(Command(shell, script))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)
	// A bounded wait must not report an exit for a child that is still running.
	short, cancelShort := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelShort()
	if process.Wait(short) {
		t.Fatal("Wait returned an exit for a child that is still running")
	}

	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("release: %v", err)
	}
	// The child ends on its own, and Wait must then report it promptly.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !process.Wait(ctx) {
		t.Fatal("Wait did not report the child's exit")
	}
}

// TestWaitRespectsCancellation pins that waiting on a server that keeps running
// is interruptible.
func TestWaitRespectsCancellation(t *testing.T) {
	// A single-process sleeper: a shell wrapper could leave its own child
	// behind when the shell is killed, and the test would then leak what it
	// meant to reap.
	name, args := sleeper(t, 30*time.Second)
	process, err := Start(Command(name, args...))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if process.Wait(ctx) {
		t.Fatal("Wait reported an exit for a child that is still running")
	}
	if process.Exited(ctx) {
		t.Fatal("Exited reported an exit for a child that is still running")
	}
}

// requireShellBinary returns a POSIX shell path.
func requireShellBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}
	return path
}

// aliveBySignal reports whether the operating system still knows a pid.
func aliveBySignal(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
