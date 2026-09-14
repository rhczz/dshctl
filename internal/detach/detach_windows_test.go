//go:build windows

package detach

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// TestDetachedLifecycleOnWindows exercises the whole lifecycle with the tools
// Windows actually ships. The reap tests are Unix-only, so without this file
// nothing on Windows would start a child, watch it run, kill it, and see it
// reaped — and on Windows the child's survival is a pair of creation flags
// rather than a POSIX session call, so that path needs its own coverage.
//
// cmd.exe runs the sleeper: the child is created with no console and no
// controlling terminal, and `timeout` refuses to run in exactly that situation,
// so `ping` is the portable way to make cmd.exe stay alive.
func TestDetachedLifecycleOnWindows(t *testing.T) {
	shell := requireCommand(t, "cmd")
	cmd := Command(shell, "/c", "ping", "-n", "31", "127.0.0.1")
	cmd.Stdout = nil
	cmd.Stderr = nil

	process, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)
	// cmd.exe's own child is ended before the child itself is reaped, so a
	// failure above cannot leave a sleeper running past the test.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if !process.Exited(ctx) {
			killTree(t, process.PID)
		}
	})

	// The child started and is detached: it is a real, live process that the
	// Start call did not wait for.
	if process.PID <= 0 {
		t.Fatalf("pid = %d", process.PID)
	}
	if !alive(process.PID) {
		t.Fatal("the detached child is not visible to the operating system")
	}
	if process.Exited(context.Background()) {
		t.Fatal("a freshly started child is reported as ended")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("the child was started without detachment attributes")
	}
	flags := cmd.SysProcAttr.CreationFlags
	if flags&createNewProcessGroup == 0 || flags&detachedProcess == 0 {
		t.Fatalf("creation flags = %#x, want the new process group (%#x) and no console (%#x)",
			flags, createNewProcessGroup, detachedProcess)
	}

	// It must still be there after Start returned: detachment means the child
	// does not depend on the lifetime of the call that created it.
	time.Sleep(200 * time.Millisecond)
	if !alive(process.PID) {
		t.Fatal("the detached child ended with the Start call")
	}
	if process.Exited(context.Background()) {
		t.Fatal("Exited reported the running child as ended")
	}

	// Kill it, and require the lifecycle to notice. The tree kill comes first
	// because ending cmd.exe does not end its sleeper; the assertions below are
	// about the lifecycle, so they do not depend on taskkill succeeding.
	killTree(t, process.PID)
	_ = killProcess(process.PID)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if !process.Wait(ctx) {
		t.Fatal("the killed child was never reaped")
	}
	if !process.Exited(context.Background()) {
		t.Fatal("Exited still reports the killed child as running")
	}
	waitGone(t, process.PID)
}

// killTree ends a detached child and anything it started. Windows does not end
// a process's children with it, and cmd.exe runs the sleeper as its own child,
// so killing only cmd.exe would leave a stray process behind on the machine.
//
// It is deliberately best effort: a pid that has already ended is not a
// failure, because the caller only cares that nothing is left.
func killTree(t *testing.T, pid int) {
	t.Helper()
	out, err := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).CombinedOutput()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
		return // 128: no such process, which is the state the caller wants
	}
	t.Logf("taskkill /F /T /PID %d: %v: %s", pid, err, out)
}
