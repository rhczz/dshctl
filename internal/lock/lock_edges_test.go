package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAcquireReportsAnErrorWhenTheParentIsARegularFile pins the shape a file in
// place of the state directory produces on the acquiring side: a lock that
// cannot be created must fail the operation instead of returning a lock that
// guards nothing.
//
// This half is the one that is identical on every platform. What Held answers
// for the same path differs: Unix reports an error, while Windows maps the
// failure to "the path does not exist" and answers "free". That difference is
// pinned in the platform-specific test files, and it does not make the refusal
// any less real — Acquire fails everywhere.
func TestAcquireReportsAnErrorWhenTheParentIsARegularFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	held, err := Acquire(context.Background(), filepath.Join(parent, "dshctl.lock"), time.Second)
	if err == nil {
		held.Release()
		t.Fatal("Acquire must not report a lock it could not create")
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("error = %v, want it to name %s", err, parent)
	}
	if data, readErr := os.ReadFile(parent); readErr != nil || string(data) != "not a directory" {
		t.Fatalf("the occupying file changed: %q (%v)", data, readErr)
	}
}

// TestHeldDoesNotFollowASymlinkAtTheLockPath pins the documented
// short-circuit: a symlink where the lock belongs is residue that Acquire
// removes, so the file it points at is not this path's lock. Following it would
// report a lock as held that any Acquire takes away in the same breath.
func TestHeldDoesNotFollowASymlinkAtTheLockPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.lock")
	held, err := Acquire(context.Background(), target, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	path := filepath.Join(root, "dshctl.lock")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	holder, locked, err := Held(path)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if locked || holder != 0 {
		t.Fatalf("Held = (%d, %v), want the symlink reported as free", holder, locked)
	}
	// The target really is held: the short-circuit is about the link, not about
	// a lock that was never there in the first place.
	if holder, locked, err := Held(target); err != nil || !locked || holder != holderThroughLock(os.Getpid()) {
		t.Fatalf("Held(target) = (%d, %v, %v), want this process as the holder", holder, locked, err)
	}
}

// TestAcquireWithANonPositiveTimeoutRefusesImmediately pins what a caller that
// asks for no patience gets: an immediate refusal while the lock is busy, with
// the wait reported honestly.
//
// A negative budget must never be echoed back as a negative wait. "已等待 -1s"
// describes a wait that cannot have happened, and an operator reading it has to
// work out whether dshctl waited, how long, or whether the timeout was even
// understood — the honest answer is that nothing was waited.
func TestAcquireWithANonPositiveTimeoutRefusesImmediately(t *testing.T) {
	path := lockPath(t)
	first, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			started := time.Now()
			_, err := Acquire(context.Background(), path, timeout)
			elapsed := time.Since(started)

			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("Acquire(timeout=%s) = %v, want a TimeoutError", timeout, err)
			}
			if elapsed >= pollInterval {
				t.Fatalf("Acquire waited %s for a timeout of %s", elapsed, timeout)
			}
			if timeoutErr.Waited < 0 {
				t.Fatalf("a timeout of %s was reported as a wait of %s: %q",
					timeout, timeoutErr.Waited, timeoutErr.Error())
			}
			if strings.Contains(timeoutErr.Error(), "-") {
				t.Fatalf("the message claims a wait that never happened: %q", timeoutErr.Error())
			}
			if timeoutErr.Holder != holderThroughLock(os.Getpid()) {
				t.Fatalf("holder = %d, want %d", timeoutErr.Holder, holderThroughLock(os.Getpid()))
			}
		})
	}
}

// TestAcquireWithANonPositiveTimeoutTakesAFreeLock pins that zero patience is
// not a refusal in itself: with nobody holding the lock there is nothing to
// wait for, so the lock is taken as usual.
func TestAcquireWithANonPositiveTimeoutTakesAFreeLock(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		held, err := Acquire(context.Background(), lockPath(t), timeout)
		if err != nil {
			t.Fatalf("Acquire(timeout=%s): %v", timeout, err)
		}
		held.Release()
	}
}

// TestReleaseIsNilSafeAndIdempotent pins the defer-heavy call pattern: a nil
// lock — a failed Acquire whose error was checked after the deferred Release —
// and a second Release must both be no-ops rather than a panic or a lock that
// is dropped twice.
func TestReleaseIsNilSafeAndIdempotent(t *testing.T) {
	var absent *Lock
	absent.Release()

	path := lockPath(t)
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	held.Release()
	held.Release()

	// The lock must really be released, not merely look released.
	again, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("a second Release left the lock held: %v", err)
	}
	again.Release()
}
