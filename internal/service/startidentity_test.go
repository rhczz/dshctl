package service

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/rhczz/dshctl/internal/host"
)

// listenerFateDecorator changes what the machine answers about the listener as
// soon as the port has answered once: the shape a server that ends between the
// readiness check and the record write leaves behind.
//
// It exists because the window is real and short: the readiness check proves the
// listener exists at one instant, and the record is written a moment later. A
// pid recycled in that moment must not be recorded as this start's server, or
// every later ownership check would confirm the stranger and `stop` would signal
// a process dshctl never started.
type listenerFateDecorator struct {
	OsHost
	host      *fakeHost
	recycle   bool
	command   string
	startedAt int64
	// fireAt is which inspection of the listener the fate happens on: the second
	// is the fingerprint read, the fourth the final observation. It is a count
	// because the window under test is defined by the flow, not by a hook the
	// production code offers.
	fireAt int
	armed  bool
	seen   int
}

func (d *listenerFateDecorator) Inspect(ctx context.Context, pid int) host.Facts {
	if d.armed {
		d.seen++
		// The first inspection of the listener is the readiness probe describing
		// it; the second is the fingerprint read the record is written from. The
		// decorator fires on the second, which is the window under test.
		if d.seen == d.fireAt && d.host.hasProcess(pid) {
			d.armed = false
			if d.recycle {
				d.host.recycle(pid, d.command, d.startedAt)
			} else {
				d.host.remove(pid)
			}
		}
	}
	return d.OsHost.Inspect(ctx, pid)
}

// armAfterReadiness arms the decorator once the port has answered, so it cannot
// fire during an unrelated flow.
func armAfterReadiness(f *fixture, fate *listenerFateDecorator) {
	inner := f.Dial
	f.Dial = func(ctx context.Context, port int) bool {
		ready := inner(ctx, port)
		if ready {
			fate.armed = true
		}
		return ready
	}
}

// TestStartRefusesToRecordARecycledListener pins the fix for a poisoned record:
// a listener that ends before its fingerprint is read must fail the start, not
// be recorded with the start time of whatever process holds the pid now.
func TestStartRefusesToRecordARecycledListener(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	fate := &listenerFateDecorator{
		OsHost: f.Host, host: f.host, recycle: true, fireAt: 2,
		command: "/usr/bin/unrelated", startedAt: fixtureStartTime + 500,
	}
	f.Host = fate
	armAfterReadiness(f, fate)

	result, err := f.Start(context.Background())
	t.Logf("start err = %v result = %+v", err, result)
	if err == nil {
		t.Fatalf("start reported success for a recycled listener: %+v", result)
	}
	record, ok, loadErr := f.Record.Load()
	if loadErr != nil {
		t.Fatalf("load record: %v", loadErr)
	}
	if ok && record.StartedAt == fixtureStartTime+500 {
		t.Fatalf("the record carries the stranger's fingerprint: %+v", record)
	}
	for _, signal := range f.host.signalsSent() {
		if signal.pid != 9000 {
			t.Fatalf("a process outside this start's group was signalled: %+v", signal)
		}
	}
}

// TestStartRefusesWhenTheListenerEndsBeforeTheRecord pins the same window with
// no replacement: the server is gone, so the start failed even though the port
// answered a moment earlier.
func TestStartRefusesWhenTheListenerEndsBeforeTheRecord(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	fate := &listenerFateDecorator{OsHost: f.Host, host: f.host, fireAt: 2}
	f.Host = fate
	armAfterReadiness(f, fate)

	result, err := f.Start(context.Background())
	if err == nil {
		t.Fatalf("start reported success after the listener ended: %+v", result)
	}
	record, ok, loadErr := f.Record.Load()
	if loadErr != nil && !errors.Is(loadErr, fs.ErrNotExist) {
		t.Fatalf("load record: %v", loadErr)
	}
	if ok {
		t.Fatalf("a failed start left a record behind: %+v", record)
	}
	for _, signal := range f.host.signalsSent() {
		if signal.pid != 9000 {
			t.Fatalf("a process outside this start's group was signalled: %+v", signal)
		}
	}
}

// TestTheAdoptedSurvivorKeepsWorkingAfterTheIdentityCheck pins the other side:
// a wrapper that is gone while its listener serves is legitimate (the survivor
// shape), and the group check must not mistake it for a recycled pid.
func TestTheAdoptedSurvivorKeepsWorkingAfterTheIdentityCheck(t *testing.T) {
	f := newFixture(t)
	wrapper, listener := 8000, 8001
	seedInterruptedStart(t, f, wrapper, listener, false)

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatalf("result = %+v, want the survivor adopted", result)
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID != listener {
		t.Fatalf("record = %+v, want the listener adopted", record)
	}
	if record.StartedAt != listenerStartTime {
		t.Fatalf("record.StartedAt = %d, want the listener's own start time", record.StartedAt)
	}
}

// TestStartFailsWhenTheServerEndsDuringTheFinalObservation pins the last window:
// the record was written from a listener that was still ours, and the server
// ends before the observation that reports the start. Reporting success there
// would be a claim about a process that is already gone.
func TestStartFailsWhenTheServerEndsDuringTheFinalObservation(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	fate := &listenerFateDecorator{OsHost: f.Host, host: f.host, fireAt: 4}
	f.Host = fate
	armAfterReadiness(f, fate)

	result, err := f.Start(context.Background())
	if err == nil {
		t.Fatalf("start reported success after the server ended: %+v", result)
	}
}
