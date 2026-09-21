//go:build unix

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/host"
)

// TestStartWithoutAFingerprintStillWorksAndSaysSo pins the degraded mode.
//
// When the process start time cannot be read, ownership rests on the port check
// alone: the record's pid must also be the process holding the port. That still
// makes a wrong kill impossible, so refusing to start would be needlessly
// strict — but the degradation is reported rather than left to be discovered.
func TestStartWithoutAFingerprintStillWorksAndSaysSo(t *testing.T) {
	f := newFixture(t)
	f.Host = unknownFingerprint{inner: f.Host}
	f.host.spontaneouslyServed = true
	f.Settings.StartTimeout = 2 * time.Second
	// The fixture shortens the fingerprint budget: this host never produces a
	// start time, so the start spends the whole budget before recording the
	// degraded mode, and that is the behaviour under test. How long the
	// production budget is belongs to TestFingerprintTimeoutIsGenerous, not
	// here; what matters is that the budget is exhausted and the start then
	// proceeds. The context is wider than the budget so the assertion is about
	// the outcome, not the clock.
	ctx, cancel := context.WithTimeout(context.Background(), fingerprintTimeout+10*time.Second)
	defer cancel()

	result, err := f.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if result.SpawnedPID == 0 {
		t.Fatal("no server was started")
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("a successful start must write a record")
	}
	if record.StartedAt != 0 {
		t.Fatalf("StartedAt = %d, want 0 with no readable start time", record.StartedAt)
	}
	if !strings.Contains(f.errOut.String(), "无法读取") {
		t.Fatalf("stderr = %q, want the degradation reported", f.errOut.String())
	}

	// The record is still usable: the pid is the listener, so a stop is safe.
	stopped, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status.State != StateStopped {
		t.Fatalf("state = %q, want stopped", stopped.Status.State)
	}
	// Stopping ends the whole tree this start created: the listener that held
	// the port and the wrapper that spawned it.
	f.wantSignals(t, []fakeSignal{
		{f.host.servedByListener, host.Graceful},
		{f.host.servedByWrapper, host.Graceful},
	})
}

// TestStopRefusesARecycledPIDEvenWithoutAFingerprint pins the remaining
// protection: without a fingerprint the port check is what keeps a stranger
// safe. A recorded pid that is alive but not the listener is never signalled.
func TestStopRefusesARecycledPIDEvenWithoutAFingerprint(t *testing.T) {
	f := newFixture(t)
	f.Host = unknownFingerprint{inner: f.Host}
	// The record names a live pid that is not the listener; a different process
	// holds the port, which is the shape of a recycled pid.
	f.host.add(5555, "/usr/bin/tail -f /var/log/system.log", fixtureStartTime)
	f.host.serving(6666, "/usr/sbin/nginx -g daemon off;")
	if err := f.Record.Save(stateRecord(f, 5555)); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(5555) {
		t.Fatal("a pid that is alive but is not the listener was ended")
	}
}

// TestFailedStartLeavesNoRecordBehind pins the post-crash state: a start whose
// server never came up leaves no runtime record. (A record *is* written while
// the wait runs — it is what lets the cleanup end the whole group — but the
// failed start removes it, so nothing survives to describe a server that does
// not exist.)
func TestFailedStartLeavesNoRecordBehind(t *testing.T) {
	f := newFixture(t)
	f.host.diesImmediately = true

	if _, err := f.Start(context.Background()); err == nil {
		t.Fatal("expected the start to fail")
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left a runtime record behind")
	}
}

// TestFingerprintTimeoutIsGenerous pins that the fingerprint budget is larger
// than a trivial wait, because a slow process listing must not degrade the
// ownership check on a busy machine.
func TestFingerprintTimeoutIsGenerous(t *testing.T) {
	if fingerprintTimeout < 5*time.Second {
		t.Fatalf("fingerprintTimeout = %s, want at least 5s", fingerprintTimeout)
	}
}

// TestProcessStartTimeGivesUpCleanly pins that an unreadable fingerprint ends
// the retry loop instead of hanging the start.
func TestProcessStartTimeGivesUpCleanly(t *testing.T) {
	f := newFixture(t)
	f.Host = unknownFingerprint{inner: f.Host}
	f.host.add(4242, "node server.js", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan int64, 1)
	go func() { done <- f.processStartTime(ctx, 4242, nil) }()
	select {
	case got := <-done:
		if got != 0 {
			t.Fatalf("processStartTime = %d, want 0 when it cannot be read", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processStartTime did not give up when its context expired")
	}
}
