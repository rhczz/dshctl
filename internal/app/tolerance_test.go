package app

import (
	"context"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/host"
)

// This file pins the tolerance the ownership fingerprint allows. It is the
// boundary that keeps two opposite mistakes out: reading a clock's granularity
// as a recycled pid would make dshctl refuse to end its own server, and reading
// every disagreement as a match would make the fingerprint no evidence at all.

// TestTheFingerprintToleranceAbsorbsTheGranularityOfTheClock walks both sides
// of the tolerance instead of restating its value.
//
// `ps` reports elapsed time in whole seconds and the record is written a moment
// after the process starts, so the recorded and observed start times of the
// same process can legitimately differ by a little. A difference that only a
// clock can explain is still this dshctl's own server: it is reported as
// running and a stop delivers the signal. A difference far larger is the shape
// of a recycled pid: nothing is signalled and the record is retired.
func TestTheFingerprintToleranceAbsorbsTheGranularityOfTheClock(t *testing.T) {
	// A few seconds: inside the tolerance, and deliberately not zero, so that
	// collapsing the tolerance is visible here rather than swallowed.
	const clockDrift = 3 * time.Second
	// Far outside any patient reading of a one-second clock: the reference is
	// the tolerance itself, so the two cases stay on opposite sides of whatever
	// the tolerance is.
	farDrift := int64(fingerprintTolerance/time.Second) + 600

	t.Run("a clock a few seconds apart is still our own process", func(t *testing.T) {
		f := newFixture(t)
		f.host.serving(4321, "pnpm --dir repo dsh web")
		// The live process looks older than the record, which is the direction
		// a one-second clock produces when the record is written just after
		// the process was started.
		f.host.mu.Lock()
		f.host.processes[4321].startedAt = fixtureStartTime - int64(clockDrift/time.Second)
		f.host.mu.Unlock()
		if err := f.Record.Save(stateRecord(f, 4321)); err != nil {
			t.Fatalf("save record: %v", err)
		}

		status, err := f.Status(context.Background(), f.Settings.Port)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if status.State != StateRunning {
			t.Fatalf("state = %q, want %q: a start time %s apart was read as somebody else's process",
				status.State, StateRunning, clockDrift)
		}

		result, err := f.Stop(context.Background())
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		f.wantSignals(t, []fakeSignal{{4321, host.Graceful}})
		if result.Status.State != StateStopped {
			t.Fatalf("state = %q, want %q after a stop of a clock-drift match",
				result.Status.State, StateStopped)
		}
	})

	t.Run("a start time far apart is a recycled pid", func(t *testing.T) {
		f := newFixture(t)
		f.host.serving(4321, "pnpm --dir repo dsh web")
		f.host.mu.Lock()
		f.host.processes[4321].startedAt = fixtureStartTime + farDrift
		f.host.mu.Unlock()
		if err := f.Record.Save(stateRecord(f, 4321)); err != nil {
			t.Fatalf("save record: %v", err)
		}

		result, err := f.Stop(context.Background())
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		f.wantNoSignals(t)
		if !f.host.isAlive(4321) {
			t.Fatalf("pid %d alive = false, want true: a start time %d s past the record is a recycled pid",
				4321, farDrift)
		}
		if result.Status.State != StateForeign {
			t.Fatalf("state = %q, want %q for a record whose pid was recycled",
				result.Status.State, StateForeign)
		}
		if _, ok := f.stateRecord(t); ok {
			t.Fatal("the record of a recycled pid is still on disk, want it retired")
		}
	})
}
