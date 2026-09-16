package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/state"
)

// This file pins the behaviour every command shares, so the model stays one
// model: what counts as "a server of ours is running", which process a command
// may signal, and what each command reports about the same state.

// TestStopEndsTheRecordedServerBehindAStranger pins that stop follows the
// record, not the port: the process dshctl started is ended even when the port
// is held by somebody else, and the stranger is reported instead of confused
// with it.
func TestStopEndsTheRecordedServerBehindAStranger(t *testing.T) {
	f := newFixture(t)
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.serving(6666, "/usr/sbin/nginx -g daemon off;")
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantSignals(t, []fakeSignal{{4242, host.Graceful}})
	if f.host.isAlive(4242) {
		t.Fatal("the recorded server survived the stop")
	}
	if !f.host.isAlive(6666) {
		t.Fatal("the stranger on the port was signalled")
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
}

// TestStopSucceedsWhenAStrangerTakesThePort pins the distinction a stop has to
// make: our server ended, and the port is held by somebody else. That is a
// successful stop with a note, not a failed one.
func TestStopSucceedsWhenAStrangerTakesThePort(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.strangerAfterSignal = 7000
	f.Settings.StopTimeout = 150 * time.Millisecond

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(f.errOut.String(), "现由其他进程") {
		t.Fatalf("stderr = %q, want a note about the stranger on the port", f.errOut.String())
	}
	if !strings.Contains(f.out.String(), "已停止") {
		t.Fatalf("stdout = %q, want the stop reported as done", f.out.String())
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
}

// TestStatusReportsALiveRecordAsNotStale pins the meaning of staleness: a record
// naming a live, fingerprint-matching server describes something stop can still
// end, so it is not stale — even when nothing is listening on the port.
func TestStatusReportsALiveRecordAsNotStale(t *testing.T) {
	f := newFixture(t)
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q", status.State, StateStopped)
	}
	if !status.RecordLive {
		t.Fatal("the record names a live server and must be reported as live")
	}
	if status.RecordStale || status.StaleRecord != nil {
		t.Fatalf("a live record was reported as stale: %+v", status)
	}
	if !strings.Contains(statusSummary(status), "仍然存活") {
		t.Fatalf("summary = %q, want it to say the recorded process is alive", statusSummary(status))
	}

	// The human report says the same thing, and names the command that ends it.
	var out strings.Builder
	if err := PrintStatus(&out, status); err != nil {
		t.Fatalf("PrintStatus: %v", err)
	}
	if !strings.Contains(out.String(), "dshctl stop") {
		t.Fatalf("status output = %q, want it to name dshctl stop", out.String())
	}
}

// TestBuildRefusesWhileThisPortServes pins that build applies the same rule
// update states: the checkout is not rewritten while a server of ours serves it.
func TestBuildRefusesWhileThisPortServes(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), "正被 dshctl 管理的服务使用") {
		t.Fatalf("error = %v, want the checkout-in-use report", err)
	}
	if strings.Contains(f.describeCommands(), "run build") {
		t.Fatal("the build ran while a server was serving the checkout")
	}
}

// TestBuildRefusesWhileAnotherPortsSurvivorServes pins the cross-port guard
// against the shape an interrupted start leaves behind: the other port's record
// names a dead wrapper, and the server that descends from it is still serving
// from the very checkout a build would replace.
func TestBuildRefusesWhileAnotherPortsSurvivorServes(t *testing.T) {
	f := newFixture(t)
	otherPort := f.Settings.Port + 1
	wrapper, listener := 8000, 8001
	f.host.add(wrapper, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.add(listener, "node apps/cli/lib/bin.js web --port", fixtureStartTime)
	f.host.mu.Lock()
	f.host.processes[listener].group = wrapper
	f.host.processes[wrapper].alive = false
	f.host.listenersByPort = map[int]int{otherPort: listener}
	f.host.mu.Unlock()

	other := state.Store{Path: filepath.Join(f.state, fmt.Sprintf(config.StateFileNamePattern, otherPort))}
	if err := other.Save(state.Record{
		PID: wrapper, SpawnedPID: wrapper, StartedAt: fixtureStartTime,
		Port: otherPort, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save other port's record: %v", err)
	}

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), strconv.Itoa(otherPort)) {
		t.Fatalf("error = %v, want it to name the other port %d", err, otherPort)
	}
}

// TestUpdateStopsARecordedServerThatNoLongerListens pins the same rule on the
// update side: a server of ours that is alive but no longer listening is stopped
// before the checkout is touched, instead of being missed because the port looks
// free.
func TestUpdateStopsARecordedServerThatNoLongerListens(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if err := f.RunUpdate(context.Background()); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if f.host.isAlive(4242) {
		t.Fatal("the recorded server was not stopped before the update")
	}
	sawStop := false
	for _, signal := range f.host.signalsSent() {
		if signal.pid == 4242 && signal.request == host.Graceful {
			sawStop = true
		}
	}
	if !sawStop {
		t.Fatalf("signals = %v, want the recorded server stopped first", f.host.signalsSent())
	}
}

// TestDoctorAgreesWithStatusOnASurvivor pins that the diagnosis and the status
// describe the same machine the same way: the interrupted start is reported as
// a survivor with the command that recovers it, not as a foreign process.
func TestDoctorAgreesWithStatusOnASurvivor(t *testing.T) {
	f := newFixture(t)
	seedInterruptedStart(t, f, 8000, 8001, false)

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Survivor {
		t.Fatalf("status = %+v, want the survivor reported", status)
	}

	checks := f.Doctor(context.Background())
	var port, record string
	for _, check := range checks {
		switch check.Name {
		case "端口":
			port = check.Detail
		case "运行记录":
			record = check.Detail
		}
	}
	if !strings.Contains(port, "上次启动") || !strings.Contains(port, "恢复管理") {
		t.Fatalf("端口 check = %q, want the survivor and how to recover it", port)
	}
	if !strings.Contains(record, "pid=8000") {
		t.Fatalf("运行记录 check = %q, want the record the survivor left behind", record)
	}
}

// TestWebURLExplainsASurvivor pins that the address command answers for a server
// that is really serving, and says how to manage it again.
func TestWebURLExplainsASurvivor(t *testing.T) {
	f := newFixture(t)
	seedInterruptedStart(t, f, 8000, 8001, false)
	address := "http://127.0.0.1:" + strconv.Itoa(f.Settings.Port) + "/?token=survivor"
	if err := f.Log.Line("dsh web: " + address); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	got, err := f.WebURL(context.Background())
	if err != nil {
		t.Fatalf("WebURL: %v", err)
	}
	if got != address {
		t.Fatalf("WebURL = %q, want %q", got, address)
	}
	if !strings.Contains(f.errOut.String(), "恢复管理") {
		t.Fatalf("stderr = %q, want the recovery hint", f.errOut.String())
	}
}

// TestUpdateRefusesWhenItCannotVerifyTheRunningServer pins the fail-closed
// choice on a host that cannot report process start times: the record names a
// live process, so the update cannot prove it is safe to end it — and it must
// not rewrite the checkout underneath it either.
func TestUpdateRefusesWhenItCannotVerifyTheRunningServer(t *testing.T) {
	f := newFixture(t)
	f.Host = unknownFingerprint{inner: f.Host}
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	err := f.RunUpdate(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), "无法验证") {
		t.Fatalf("error = %v, want the unverifiable-identity report", err)
	}
	if strings.Contains(f.describeCommands(), "git pull") {
		t.Fatal("the checkout was updated while a server could not be safely stopped")
	}
	if !f.host.isAlive(4242) {
		t.Fatal("a process whose identity could not be verified was ended")
	}
}

// TestStatusReportsARecycledRecordAsStaleEvenWithAStrangerOnThePort pins the
// shape no state branch names explicitly: the recorded pid now belongs to a
// different process, and yet another process holds the port. The record
// describes nothing usable, so it is stale — decided by the same predicates
// every command uses rather than by the branch that happened to run.
func TestStatusReportsARecycledRecordAsStaleEvenWithAStrangerOnThePort(t *testing.T) {
	f := newFixture(t)
	// 4242 exists, but it is not the process the record was written for.
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime+500)
	f.host.serving(6666, "/usr/sbin/nginx -g daemon off;")
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateOrphan {
		t.Fatalf("state = %q, want %q", status.State, StateOrphan)
	}
	if status.RecordLive {
		t.Fatal("a recycled pid was reported as a live record")
	}
	if !status.RecordStale || status.StaleRecord == nil || status.StaleRecord.PID != 4242 {
		t.Fatalf("status = %+v, want the recycled record reported as stale", status)
	}
}

// TestAdoptSurvivorRefusesAListenerOutsideTheRecordedTree pins the guard that
// keeps adoption from claiming a stranger: the record names a live wrapper, but
// the process on the port belongs to a different tree. Nothing is adopted,
// nothing on the port is signalled, and the record is left exactly as it was.
func TestAdoptSurvivorRefusesAListenerOutsideTheRecordedTree(t *testing.T) {
	f := newFixture(t)
	f.host.add(8000, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.add(8001, "node /somewhere/else/apps/cli/lib/bin.js web", fixtureStartTime)
	f.host.listen(8001)
	saved := state.Record{
		PID: 8000, SpawnedPID: 8000, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}
	if err := f.Record.Save(saved); err != nil {
		t.Fatalf("save record: %v", err)
	}

	// The state machine must not call this a survivor.
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Survivor {
		t.Fatalf("status = %+v, want no survivor claim for a listener outside the tree", status)
	}

	// Starting refuses instead of adopting or spawning a second server.
	_, err = f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
	record, ok := f.stateRecord(t)
	if !ok || record.PID != saved.PID || record.SpawnedPID != saved.SpawnedPID {
		t.Fatalf("record = %+v (ok=%v), want it untouched", record, ok)
	}
	if !f.host.isAlive(8001) {
		t.Fatal("a listener outside the recorded tree was ended")
	}
}

// TestStatusReportsPortReadiness pins the readiness fact independently of the
// state: it is whether the port accepted a connection, not whether a record
// exists.
func TestStatusReportsPortReadiness(t *testing.T) {
	f := newFixture(t)
	f.host.ready = false
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.listen(4242)
	if err := f.Record.Save(state.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}
	starting, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if starting.State != StateStarting || starting.Ready {
		t.Fatalf("status = %+v, want a not-ready starting state", starting)
	}

	f.host.ready = true
	running, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if running.State != StateRunning || !running.Ready {
		t.Fatalf("status = %+v, want a ready running state", running)
	}
}

// TestStartRefusesWhenThePortProbeFails pins that a start cannot proceed on a
// port nobody could inspect: "cannot look" is a precondition failure, never an
// assumption that the port is free.
func TestStartRefusesWhenThePortProbeFails(t *testing.T) {
	f := newFixture(t)
	f.host.listenErr = errors.New("lsof: operation not permitted")

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	f.wantNoSpawn(t)
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a refused start must not leave a record behind")
	}
}
