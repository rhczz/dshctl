package lock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockPath returns a lock path inside a fresh directory.
func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "dshctl.lock")
}

// lockChildEnv returns the parent environment with every DSHCTL_LOCK_* helper
// variable removed, plus the given entries. Unix resolves a duplicated key to
// its first occurrence, so an ambient value would shadow a plain append.
func lockChildEnv(extra ...string) []string {
	environment := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DSHCTL_LOCK_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, extra...)
}

// TestAcquireAndRelease pins the basic protocol, including the idempotent
// release that defer-heavy call sites rely on.
func TestAcquireAndRelease(t *testing.T) {
	path := lockPath(t)
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	holder, locked, err := Held(path)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if !locked || holder != os.Getpid() {
		t.Fatalf("Held = (%d, %v), want this process", holder, locked)
	}
	held.Release()
	_, locked, err = Held(path)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if locked {
		t.Fatal("the lock must be free after Release")
	}
	held.Release()
}

// TestSecondAcquireTimesOut pins that a live holder blocks a second operation.
func TestSecondAcquireTimesOut(t *testing.T) {
	path := lockPath(t)
	first, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	_, err = Acquire(context.Background(), path, 250*time.Millisecond)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected a TimeoutError, got %v", err)
	}
	if timeout.Holder != os.Getpid() {
		t.Fatalf("holder = %d, want %d", timeout.Holder, os.Getpid())
	}
	if timeout.Waited != 250*time.Millisecond {
		t.Fatalf("waited = %s, want the timeout", timeout.Waited)
	}
	if !strings.Contains(timeout.Error(), "另一个 dshctl 操作正在进行") {
		t.Fatalf("message = %q", timeout.Error())
	}
}

// TestCancelledWaitReturnsTheContextError pins that an interrupt reaches the
// waiter instead of being swallowed by the retry loop.
func TestCancelledWaitReturnsTheContextError(t *testing.T) {
	path := lockPath(t)
	first, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, path, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire error = %v, want context.Canceled", err)
	}
}

// TestALeftoverRecordDoesNotHoldTheLock pins that the pid inside the file is a
// record, not the lock.
func TestALeftoverRecordDoesNotHoldTheLock(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	holder, locked, err := Held(path)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if locked || holder != 0 {
		t.Fatalf("Held = (%d, %v), want a free lock", holder, locked)
	}
	held, err := Acquire(context.Background(), path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("a leftover record must not block Acquire: %v", err)
	}
	defer held.Release()
}

// TestDeletingTheLockFileDoesNotDeadlockTheWaiter is the regression test for
// removing the state directory while an operation runs: the waiting operation
// must still end up holding the lock at the fresh path after the first holder
// releases, instead of waiting forever on an unlinked inode.
//
// What this test cannot observe without a seam is whether the two holders ever
// overlapped: the waiter is expected to acquire the recreated file only after
// the first holder releases, and the assertion that pins that sequencing is the
// fresh acquire succeeding promptly once the release happened.
func TestDeletingTheLockFileDoesNotDeadlockTheWaiter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dshctl.lock")

	first, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	// A second operation is waiting while the state directory is removed.
	secondResult := make(chan error, 1)
	go func() {
		second, err := Acquire(context.Background(), path, 3*time.Second)
		if err != nil {
			secondResult <- err
			return
		}
		second.Release()
		secondResult <- nil
	}()

	time.Sleep(100 * time.Millisecond)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove state dir: %v", err)
	}
	// The first holder now holds a lock on an unlinked inode; releasing it must
	// let the waiter through rather than deadlocking.
	first.Release()

	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatalf("the waiting operation failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting operation never acquired the lock")
	}
}

// TestHeldReportsAnUnreadableLock pins that a lock that cannot be inspected is
// an error rather than a confident "free".
func TestHeldReportsAnUnreadableLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dshctl.lock")
	// A directory at the lock path cannot be opened as a file.
	if err := os.MkdirAll(filepath.Join(path, "residue"), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, _, err := Held(path); err == nil {
		t.Fatal("an uninspectable lock must be reported as an error")
	}
}

// TestResidueAtTheLockPathIsDiscarded pins that a directory left where the lock
// belongs is cleared, because the state directory is disposable by design.
func TestResidueAtTheLockPathIsDiscarded(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Join(path, "pid"), 0o700); err != nil {
		t.Fatalf("seed residue: %v", err)
	}
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the lock path must hold a regular file, got %v (%v)", info, err)
	}
}

// TestASymlinkAtTheLockPathIsNotFollowed pins that clearing residue cannot reach
// outside the state directory.
func TestASymlinkAtTheLockPathIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(stateDir, "dshctl.lock")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatalf("the symlink target was modified: %v", err)
	}
}

// TestHeldOnAMissingLock pins the first-run answer.
func TestHeldOnAMissingLock(t *testing.T) {
	holder, locked, err := Held(lockPath(t))
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if locked || holder != 0 {
		t.Fatalf("Held = (%d, %v), want a free lock", holder, locked)
	}
}

// TestConcurrentAcquiresAreSerialized pins that only one of many contenders can
// hold the lock at a time, and that every contender eventually wins.
func TestConcurrentAcquiresAreSerialized(t *testing.T) {
	path := lockPath(t)
	const contenders = 8
	var (
		mu      sync.Mutex
		inside  int
		maxSeen int
		failed  []error
		wait    sync.WaitGroup
	)
	wait.Add(contenders)
	for index := 0; index < contenders; index++ {
		go func() {
			defer wait.Done()
			held, err := Acquire(context.Background(), path, 10*time.Second)
			if err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
				return
			}
			defer held.Release()
			mu.Lock()
			inside++
			if inside > maxSeen {
				maxSeen = inside
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wait.Wait()
	mu.Lock()
	defer mu.Unlock()
	if maxSeen != 1 {
		t.Fatalf("up to %d holders were inside the lock at once", maxSeen)
	}
	if len(failed) != 0 {
		t.Fatalf("%d contenders never acquired the lock: %v", len(failed), failed)
	}
}

// TestLockIsReleasedWhenTheHolderIsKilled pins the property the whole design
// rests on: the kernel drops the lock when the process dies, however it dies.
// A real child process holds the lock while its parent kills it with SIGKILL,
// and a fresh Acquire must then succeed without waiting for any bookkeeping.
func TestLockIsReleasedWhenTheHolderIsKilled(t *testing.T) {
	// The marker names the parent pid and the key is stripped from the child's
	// environment before it is set: an ambient DSHCTL_LOCK_HOLDER would
	// otherwise take this branch in the parent test process and exit the whole
	// run, or shadow the appended value in the child and start a fork chain.
	if os.Getenv("DSHCTL_LOCK_HOLDER") == "child-of-"+strconv.Itoa(os.Getppid()) {
		holdUntilKilled()
		return
	}
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestLockIsReleasedWhenTheHolderIsKilled$")
	command.Env = lockChildEnv("DSHCTL_LOCK_HOLDER=child-of-"+strconv.Itoa(os.Getpid()), "DSHCTL_LOCK_PATH="+path)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}

	// Wait until the child actually holds the lock, then prove nobody else can
	// take it while it lives.
	ready := path + ".ready"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the holder never took the lock: %s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := Acquire(context.Background(), path, 100*time.Millisecond); err == nil {
		t.Fatal("a second holder appeared while the first lived")
	}

	// The holder dies without releasing anything, and the kernel must free the
	// lock with it.
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = command.Wait()
	held, err := Acquire(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire after the holder died: %v", err)
	}
	held.Release()
}

// holdUntilKilled runs in a child test process: take the lock, announce it, and
// stay alive until the parent kills the process.
func holdUntilKilled() {
	path := os.Getenv("DSHCTL_LOCK_PATH")
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(path+".ready", []byte("held"), 0o600); err != nil {
		os.Exit(3)
	}
	// Block until SIGKILL. KeepAlive matters: the *Lock wraps an os.File, and
	// letting it become unreachable would let the finalizer close the file and
	// release the lock early.
	for {
		time.Sleep(time.Minute)
		runtime.KeepAlive(held)
	}
}
