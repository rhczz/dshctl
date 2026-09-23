package service

import (
	"context"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/lock"
)

// This file pins the operation lock from the outside: a mutating command that
// finds another operation holding it reports exit code 5 without touching
// anything, and one lock covers the whole state directory rather than one
// instance at a time.

// TestAMutatingCommandReportsExitCodeFiveWhileAnotherOperationHoldsTheLock pins
// the documented exit code for a busy state directory.
//
// Exit code 5 is the contract scripts branch on, and "not 1" is the point of it:
// a generic failure tells the caller to investigate, while a lock timeout tells
// it to retry. The refusal must be complete — no signal to the running server
// and no process started — and it must be a *wait* first: a holder that lets go
// while the command is still waiting is not a timeout.
func TestAMutatingCommandReportsExitCodeFiveWhileAnotherOperationHoldsTheLock(t *testing.T) {
	t.Run("another operation holds the lock", func(t *testing.T) {
		f := newFixture(t)
		// A healthy instance of a machine where a restart would have real work
		// to do: signal the server, then bring it back.
		f.startServer(t, 4321, "")
		f.host.spontaneouslyServed = true
		f.Settings.LockTimeout = 50 * time.Millisecond
		held := mustHoldLock(t, f)
		defer held.Release()

		_, err := f.Restart(context.Background())

		wantCode(t, err, exitcode.LockTimeout)
		wantContains(t, err, "another dshctl operation is running")
		f.wantNoSignals(t)
		f.wantNoSpawn(t)
		if !f.host.isAlive(4321) {
			t.Fatalf("pid %d alive = false after the refused restart, want it left running", 4321)
		}
	})

	t.Run("the holder lets go while the command waits", func(t *testing.T) {
		f := newFixture(t)
		f.startServer(t, 4321, "")
		// The budget is far wider than the release below, so a command that
		// waits instead of failing at once is what makes the stop succeed.
		f.Settings.LockTimeout = 5 * time.Second
		held := mustHoldLock(t, f)
		go func() {
			time.Sleep(50 * time.Millisecond)
			held.Release()
		}()

		result, err := f.Stop(context.Background())
		if err != nil {
			t.Fatalf("stop returned %v while the holder was about to let go, want it to wait and succeed", err)
		}
		if result.Status.State != domain.StateStopped {
			t.Fatalf("state = %q, want %q after a stop that waited for the lock",
				result.Status.State, domain.StateStopped)
		}
		f.wantSignals(t, []fakeSignal{{4321, hostGraceful}})
	})
}

// TestOneLockCoversEveryInstanceOfAStateDirectory pins the granularity of the
// operation lock: it serializes the whole state directory, not one instance at
// a time.
//
// The package document promises that a command covering several instances holds
// one lock for all of them, so no other command sees half of a stop or a
// restart. A lock taken per instance would still refuse a command started while
// another operation runs; what it would not prevent is a second command
// slipping in between two instances and observing a machine that is halfway
// through a multi-port operation. This test pins the directory-level guarantee
// from the only side a test can see it without injecting timing: a lock held on
// the state directory refuses the whole operation before the first instance is
// touched, and once it is free the same command covers every instance.
func TestOneLockCoversEveryInstanceOfAStateDirectory(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)
	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	f.Settings.LockTimeout = 50 * time.Millisecond
	held := mustHoldLock(t, f)

	_, err := f.StopAll(context.Background())
	wantCode(t, err, exitcode.LockTimeout)
	f.wantNoSignals(t)
	if !f.host.isAlive(4321) || !f.host.isAlive(4322) {
		t.Fatalf("alive after the refused stop = 4321:%v 4322:%v, want both true",
			f.host.isAlive(4321), f.host.isAlive(4322))
	}
	wantRecordForPort(t, f, configured, true)
	wantRecordForPort(t, f, other, true)

	held.Release()

	// The same command, with the directory free again, covers every instance
	// the state directory manages rather than only the one it names.
	stopped, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("stop returned %v with the lock free, want it to end every instance", err)
	}
	if len(stopped.Results) != 2 {
		t.Fatalf("stopped %d instances, want 2: %+v", len(stopped.Results), stopped.Results)
	}
	if f.host.isAlive(4321) || f.host.isAlive(4322) {
		t.Fatalf("alive after the stop = 4321:%v 4322:%v, want both false",
			f.host.isAlive(4321), f.host.isAlive(4322))
	}
	wantSignalOn(t, f, 4321)
	wantSignalOn(t, f, 4322)
	wantRecordForPort(t, f, configured, false)
	wantRecordForPort(t, f, other, false)
}

// mustHoldLock takes the operation lock the way another dshctl operation would,
// so a test can observe what a command does while the state directory is busy.
func mustHoldLock(t *testing.T, f *fixture) *lock.Lock {
	t.Helper()
	held, err := lock.Acquire(context.Background(), f.Settings.LockFile(), time.Second)
	if err != nil {
		t.Fatalf("could not take the operation lock (%v), want it held for this test", err)
	}
	return held
}
