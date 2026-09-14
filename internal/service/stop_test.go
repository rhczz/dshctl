package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/state"
)

// TestStopClearsTheRecordOfARecycledPID pins the shape a stopped-then-reused pid
// leaves behind: the record names a pid that now belongs to somebody else, so it
// describes nothing dshctl may signal and nothing it may report as a live
// server. The record is retired, and the live process is left alone.
//
// It is the counterpart of TestStopReportsALiveRecordWithoutStartedAt: "alive"
// alone does not protect a record — the fingerprint has to match.
func TestStopClearsTheRecordOfARecycledPID(t *testing.T) {
	f := newFixture(t)
	// The pid exists and owns the port, but the process started much later than
	// the record says: the operating system recycled it.
	f.host.serving(4321, "pnpm --dir repo dsh web")
	f.host.mu.Lock()
	f.host.processes[4321].startedAt = fixtureStartTime + 10_000
	f.host.mu.Unlock()
	if err := f.Record.Save(state.Record{
		PID: 4321, SpawnedPID: 4321, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(4321) {
		t.Fatal("a process the record does not vouch for was ended")
	}
	if _, err := os.Lstat(f.Settings.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("a record naming a recycled pid survived the stop (err=%v)", err)
	}
	if result.Status.State != StateForeign {
		t.Fatalf("state = %q, want %q", result.Status.State, StateForeign)
	}
}

// TestStopReportsALiveRecordWithoutStartedAt pins the boundary of "verified".
//
// state.Match deliberately treats an unknown start time as a match, which is
// right for reporting and wrong for signalling: a record that carries no start
// time at all identifies nothing, so the stop must not signal on it. The record
// is the only handle on that process, so it is kept rather than retired.
func TestStopReportsALiveRecordWithoutStartedAt(t *testing.T) {
	f := newFixture(t)
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: 0,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(4242) {
		t.Fatal("a process without a verifiable start time was ended")
	}
	if _, ok := f.stateRecord(t); !ok {
		t.Fatal("the record of a live process was deleted")
	}
	if !result.Status.RecordLive {
		t.Fatalf("status = %+v, want the live record reported as live", result.Status)
	}
	if result.Status.RecordStale {
		t.Fatalf("status = %+v, want a live record never reported as stale", result.Status)
	}
	if !strings.Contains(f.errOut.String(), "记录已保留") {
		t.Fatalf("stderr = %q, want the record-kept note", f.errOut.String())
	}
}

// TestRestartDoesNotStartASecondServerWhenTheStopFails pins the half-operation
// guard on the restart path: a server that survives the stop still holds the
// checkout, so starting another one would leave two servers and one record.
func TestRestartDoesNotStartASecondServerWhenTheStopFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	f.host.processes[4321].survivesGraceful = true
	f.host.processes[4321].survivesForce = true
	f.host.mu.Unlock()

	_, err := f.Restart(context.Background())
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "强制结束后仍然存在")
	f.wantNoSpawn(t)
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if !f.host.isAlive(4321) {
		t.Fatal("the test premise is wrong: the process did not survive the force signal")
	}
}

// TestStopEndsAProcessThatDiesBeforeTheSignal pins the third shape of a failing
// terminate: the process was alive when the stop decided to end it and is gone by
// the time the signal would be delivered. Nothing may be signalled, and the stop
// must succeed: the server is down, which is what was asked for.
func TestStopEndsAProcessThatDiesBeforeTheSignal(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.Host = serviceWithout{inner: f.Host, gone: 4321}
	// The process is gone, so it can neither own the port nor be signalled.
	f.host.remove(4321)

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
	if result.Status.State != StateStopped {
		t.Fatalf("state = %q, want %q", result.Status.State, StateStopped)
	}
}

// TestStopWarnsWhenTheGracefulSignalFails pins that a termination request the
// host refuses is a warning, not the end of the stop.
//
// The graceful request is a courtesy: a process that never received it may still
// be gone a moment later, or may be force-ended like one that ignored it. Giving
// up here would leave a server running because of a signal delivery error while
// the operator was told the stop failed.
func TestStopWarnsWhenTheGracefulSignalFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	// The request is refused AND the process ignores it: on a real host a failed
	// signal means the process never received it.
	f.host.processes[4321].survivesGraceful = true
	f.host.signalErr = func(request host.Request) error {
		if request == host.Graceful {
			return errors.New("signal: operation not permitted")
		}
		return nil
	}
	f.host.mu.Unlock()

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// The warning is not the end of the stop: the force step still ran and ended
	// the server.
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if !strings.Contains(f.errOut.String(), "operation not permitted") {
		t.Fatalf("stderr = %q, want the failed signal reported", f.errOut.String())
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
	if !strings.Contains(f.out.String(), "已停止") {
		t.Fatalf("stdout = %q, want the stop reported as done", f.out.String())
	}
}

// TestStopFailsWhenTheForceSignalFails pins the other side of that boundary: the
// force signal is the last resort, and a host that refuses it means the server
// is still running. That is a Failure, and the record stays so the operator can
// try again.
func TestStopFailsWhenTheForceSignalFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	f.host.processes[4321].survivesGraceful = true
	f.host.signalErr = func(request host.Request) error {
		if request == host.Force {
			return errors.New("kill: no such process")
		}
		return nil
	}
	f.host.mu.Unlock()

	_, err := f.Stop(context.Background())
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "no such process")
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if !f.host.isAlive(4321) {
		t.Fatal("the stubborn server was reported as ended")
	}
	if _, ok := f.stateRecord(t); !ok {
		t.Fatal("a failed stop must keep the record of the surviving server")
	}
}

// TestStopClearsARecordThatDescribesNothing pins the ordinary stale-record
// cleanup, so the "clear" half of clearStaleRecord stays covered next to the
// "keep" half.
func TestStopClearsARecordThatDescribesNothing(t *testing.T) {
	f := newFixture(t)
	if err := f.Record.Save(state.Record{
		PID: 999999, SpawnedPID: 999999, StartedAt: 1_600_000_000,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if _, err := os.Lstat(f.Settings.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("a stale record survived the stop (err=%v)", err)
	}
}
