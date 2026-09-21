package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/lock"
	"github.com/rhczz/dshctl/internal/logfile"
	"github.com/rhczz/dshctl/internal/run"
	"github.com/rhczz/dshctl/internal/state"
)

// TestStartRefusesAForeignListener pins the first safety rule: a port owned by
// another program is reported, never taken over.
func TestStartRefusesAForeignListener(t *testing.T) {
	f := newFixture(t)
	f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "无法确认")
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
}

// TestStartRefusesAnUnidentifiableListener pins the rule that "I cannot prove
// this is mine" must block a start instead of starting a second server.
func TestStartRefusesAnUnidentifiableListener(t *testing.T) {
	f := newFixture(t)
	// Something listens that the platform cannot classify: an opaque process
	// with no harness markers in its command line.
	f.host.serving(7777, "/opt/vendor/agent --listen 3080")

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "无法确认")
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
}

// TestStartIsIdempotentWhenAlreadyRunning pins that a verified running server is
// left alone.
func TestStartIsIdempotentWhenAlreadyRunning(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "http://127.0.0.1:3080/?token=abc")

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatalf("AlreadyRunning = false, want true")
	}
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
	if !strings.Contains(f.out.String(), "已在运行") {
		t.Fatalf("output = %q", f.out.String())
	}
}

// TestStartRefusesARepositoryThatIsNotTheCheckout pins the guard that keeps a
// mistyped --repo from reaching the destructive prune and build steps.
func TestStartRefusesARepositoryThatIsNotTheCheckout(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.repo, "pnpm-workspace.yaml")); err != nil {
		t.Fatalf("remove workspace manifest: %v", err)
	}

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "DeepSeek Harness")
	f.wantNoSpawn(t)
}

// TestStartFailsFastWhenTheChildExits pins that a server that dies immediately
// is reported at once rather than after the whole start timeout.
func TestStartFailsFastWhenTheChildExits(t *testing.T) {
	f := newFixture(t)
	f.host.diesImmediately = true
	f.Settings.StartTimeout = 30 * time.Second

	start := time.Now()
	_, err := f.Start(context.Background())
	elapsed := time.Since(start)

	wantCode(t, err, exitcode.Failure)
	if elapsed > 5*time.Second {
		t.Fatalf("start waited %s for a child that had already exited", elapsed)
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left its runtime record behind")
	}
}

// TestStartCancellationIsPrompt is the regression test for a start that ignored
// SIGINT and spun until the timeout.
func TestStartCancellationIsPrompt(t *testing.T) {
	f := newFixture(t)
	f.Settings.StartTimeout = 10 * time.Minute
	// Nothing ever takes the port, and the child stays alive, so only the
	// context can end the wait.
	f.host.spontaneouslyServed = false

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := f.Start(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("cancelled start took %s to return", elapsed)
	}
	// The failed start must not leave the process it spawned running.
	if signals := f.host.signalsSent(); len(signals) == 0 || signals[0].request != host.Graceful {
		t.Fatalf("signals = %v, want a graceful request for the spawned child", signals)
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a cancelled start left its runtime record behind")
	}
}

// TestStartSucceedsWhenThePortAnswers pins the happy path, including the record
// that later lets stop verify ownership.
func TestStartSucceedsWhenThePortAnswers(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if result.SpawnedPID == 0 {
		t.Fatal("no process was spawned")
	}
	if result.Status.State != StateRunning {
		t.Fatalf("state = %q, want %q", result.Status.State, StateRunning)
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("a successful start must write a runtime record")
	}
	// The record names the process holding the port, which is the wrapper's child
	// when the server is started through a package script; the wrapper itself is
	// kept beside it so the whole group can be ended as one unit.
	if record.SpawnedPID != result.SpawnedPID {
		t.Fatalf("record = %+v, want the spawned pid %d recorded", record, result.SpawnedPID)
	}
	if record.PID != f.host.servedByListener {
		t.Fatalf("record pid = %d, want the listener %d", record.PID, f.host.servedByListener)
	}
	if record.Phase != state.PhaseRunning {
		t.Fatalf("phase = %q, want running", record.Phase)
	}
	if record.StartedAt == 0 {
		t.Fatal("the record must carry the process start time fingerprint")
	}
}

// TestStartReplacesAStaleRecord pins that a record naming a dead pid is cleared
// instead of blocking the start.
func TestStartReplacesAStaleRecord(t *testing.T) {
	f := newFixture(t)
	stale := state.Record{PID: 5555, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning}
	if err := f.Record.Save(stale); err != nil {
		t.Fatalf("save stale record: %v", err)
	}
	f.host.spontaneouslyServed = true

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no record after start")
	}
	if record.PID == stale.PID || record.SpawnedPID == stale.PID {
		t.Fatalf("record still names the dead pid %d: %+v", stale.PID, record)
	}
	if record.SpawnedPID != result.SpawnedPID {
		t.Fatalf("record = %+v, want spawned pid %d", record, result.SpawnedPID)
	}
}

// TestStartStoresTheAnnouncedURL pins that the token address survives log
// rotation, because it is kept in the record rather than only in the log.
func TestStartStoresTheAnnouncedURL(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	announced := fmt.Sprintf("http://127.0.0.1:%d/?token=deadbeef", f.Settings.Port)
	if err := f.Log.Line("dsh web: " + announced); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no record after start")
	}
	if record.URL != announced {
		t.Fatalf("record URL = %q, want %q", record.URL, announced)
	}
}

// TestStopOnlySignalsTheRecordedProcess pins that a stop ends exactly the
// process the record names.
func TestStopOnlySignalsTheRecordedProcess(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}})
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the runtime record")
	}
	if result.Status.State != StateStopped {
		t.Fatalf("state = %q, want %q", result.Status.State, StateStopped)
	}
	if !strings.Contains(f.out.String(), "已停止") {
		t.Fatalf("output = %q", f.out.String())
	}
}

// TestStopForcesAProcessThatIgnoresGrace is the regression test for the force
// step: a server that survives the graceful request is ended with a force
// signal, and only after the grace period elapsed.
func TestStopForcesAProcessThatIgnoresGrace(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	f.host.processes[4321].survivesGraceful = true
	f.host.mu.Unlock()

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if f.host.isAlive(4321) {
		t.Fatal("the server survived the force signal")
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the runtime record")
	}
}

// TestStopRefusesForceWhenPIDIsRecycledDuringGrace is the regression test for
// the check-then-force race: when the pid is handed to a different process
// while the stop waits for a graceful exit, the force signal must not be sent.
func TestStopRefusesForceWhenPIDIsRecycledDuringGrace(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	entry := f.host.processes[4321]
	entry.survivesGraceful = true
	entry.onGraceful = func() {
		// The operating system recycles the pid the moment the graceful request
		// is delivered: the same pid now belongs to a stranger.
		f.host.mu.Lock()
		f.host.processes[4321] = &fakeProcess{
			command:   "/usr/bin/tail -f /var/log/system.log",
			startedAt: fixtureStartTime + 10_000,
			alive:     true,
		}
		f.host.mu.Unlock()
	}
	f.host.mu.Unlock()

	_, err := f.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop succeeded while the pid belonged to a recycled process")
	}
	if !strings.Contains(err.Error(), "复用") {
		t.Fatalf("Stop error = %v, want a recycled-pid report", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}})
	if !f.host.isAlive(4321) {
		t.Fatal("the recycled process was force-killed")
	}
}

// listenerStartTime is when the server of an interrupted start began, and it is
// deliberately different from the wrapper's: adoption must record the
// *listener's* start time, and a fixture that gives both the same value cannot
// tell the two apart.
const listenerStartTime = fixtureStartTime + 30

// seedInterruptedStart arranges the state a SIGKILLed start leaves behind: a
// record naming the wrapper, and a live listener — a descendant of that wrapper
// — serving the port. The wrapper may still be alive (it is waiting for its
// server) or already reaped; both are shapes a real interruption produces, and
// both must be recoverable.
func seedInterruptedStart(t *testing.T, f *fixture, wrapper, listener int, wrapperAlive bool) {
	t.Helper()
	f.host.add(wrapper, "pnpm --dir repo dsh web", fixtureStartTime)
	f.host.add(listener, "node apps/cli/lib/bin.js web --port", listenerStartTime)
	f.host.mu.Lock()
	f.host.processes[listener].group = wrapper
	f.host.processes[wrapper].alive = wrapperAlive
	f.host.listener = listener
	f.host.mu.Unlock()
	if err := f.Record.Save(state.Record{
		PID:        wrapper,
		SpawnedPID: wrapper,
		StartedAt:  fixtureStartTime,
		Port:       f.Settings.Port,
		Phase:      state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}
}

// interruptedStartShapes are the two ways an interrupted start can be found.
var interruptedStartShapes = []struct {
	name        string
	wrapperDead bool
}{
	{"wrapper still waiting", false},
	{"wrapper already reaped", true},
}

// TestStartAdoptsASurvivorOfAnInterruptedStart is the regression test for a
// start that was killed between writing the wrapper record and the port
// answering: the server keeps serving, the record names the wrapper, and a later
// start must recover the server instead of reporting it as foreign or spawning a
// second one.
//
// The fingerprint matters as much as the pid: the adopted record must carry the
// *listener's* start time, or every later ownership check compares the record
// against the wrong process value, calls the running server foreign and retires
// the record — leaving a server no command can manage.
func TestStartAdoptsASurvivorOfAnInterruptedStart(t *testing.T) {
	for _, shape := range interruptedStartShapes {
		t.Run(shape.name, func(t *testing.T) {
			f := newFixture(t)
			wrapper, listener := 8000, 8001
			seedInterruptedStart(t, f, wrapper, listener, !shape.wrapperDead)

			result, err := f.Start(context.Background())
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			if !result.AlreadyRunning {
				t.Fatal("the adopted survivor was not reported as already running")
			}
			f.wantNoSpawn(t)
			f.wantNoSignals(t)
			record, ok := f.stateRecord(t)
			if !ok {
				t.Fatal("the adopted survivor must have a record")
			}
			if record.PID != listener || record.SpawnedPID != wrapper {
				t.Fatalf("record = %+v, want the listener %d adopted with the wrapper %d kept", record, listener, wrapper)
			}
			if record.StartedAt != listenerStartTime {
				t.Fatalf("record.StartedAt = %d, want the listener's start time %d", record.StartedAt, listenerStartTime)
			}
			// The adopted record must satisfy the ownership check it exists for.
			status, err := f.Status(context.Background(), f.Settings.Port)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if status.State != StateRunning || !status.RecordLive {
				t.Fatalf("status = %+v, want the adopted server recognized as running", status)
			}
		})
	}
}

// TestStopAdoptsAndStopsASurvivorOfAnInterruptedStart pins the stop side of the
// same recovery: the survivor is adopted and then ended, not refused.
func TestStopAdoptsAndStopsASurvivorOfAnInterruptedStart(t *testing.T) {
	for _, shape := range interruptedStartShapes {
		t.Run(shape.name, func(t *testing.T) {
			f := newFixture(t)
			wrapper, listener := 8000, 8001
			seedInterruptedStart(t, f, wrapper, listener, !shape.wrapperDead)

			if _, err := f.Stop(context.Background()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			// The adopted survivor and the wrapper its record names are one tree.
			want := []fakeSignal{{listener, host.Graceful}}
			if !shape.wrapperDead {
				want = append(want, fakeSignal{wrapper, host.Graceful})
			}
			f.wantSignals(t, want)
			if f.host.isAlive(listener) {
				t.Fatal("the adopted survivor was not ended")
			}
			if _, ok := f.stateRecord(t); ok {
				t.Fatal("a completed stop must clear the record")
			}
		})
	}
}

// TestStatusKeepsTheInterruptedRecordReadOnly pins that reporting never writes:
// status observes the interrupted start and reports the orphan, leaving the
// adoption to a mutating command.
func TestStatusKeepsTheInterruptedRecordReadOnly(t *testing.T) {
	f := newFixture(t)
	wrapper, listener := 8000, 8001
	seedInterruptedStart(t, f, wrapper, listener, false)

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateOrphan {
		t.Fatalf("state = %q, want %q", status.State, StateOrphan)
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID != wrapper {
		t.Fatalf("record = %+v (ok=%v), want the wrapper record kept untouched", record, ok)
	}
	f.wantNoSignals(t)
}

// TestFailedStartCleansUpTheWholeGroup is the regression test for a cleanup
// that ended only the wrapper: the server child kept the port with no record,
// and every later command reported it as unmanageable. A failed start must end
// the wrapper *and* the listener it spawned, free the port, and clear the
// record.
func TestFailedStartCleansUpTheWholeGroup(t *testing.T) {
	f := newFixture(t)
	// The server binds the port but never answers the readiness dial, so the
	// start times out and the cleanup path runs while both processes are alive.
	f.host.spontaneouslyServed = true
	f.host.ready = false
	f.Settings.StartTimeout = 200 * time.Millisecond

	start := time.Now()
	_, err := f.Start(context.Background())
	elapsed := time.Since(start)
	wantCode(t, err, exitcode.Failure)
	// The timeout must actually fire: a listener that binds but never answers
	// once caused the readiness wait to bypass its deadline and spin forever.
	if elapsed > 5*time.Second {
		t.Fatalf("start took %s for a server that never became ready", elapsed)
	}
	// The cleanup reports each step, so an operator reading the console knows
	// what happened to the processes this start created and where to look.
	for _, want := range []string{
		"启动失败或超时，正在清理本次启动的进程 ...",
		"已清理。日志尾部:",
	} {
		if !strings.Contains(f.errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", f.errOut.String(), want)
		}
	}
	// The error names the log, which is where the server's own explanation of
	// why it never came up was written.
	if !strings.Contains(err.Error(), f.Settings.LogPath) {
		t.Fatalf("error = %v, want it to name the log", err)
	}

	wrapper, listener := f.host.servedByWrapper, f.host.servedByListener
	if wrapper == 0 || listener == 0 {
		t.Fatal("the fixture did not spawn the wrapper-and-listener pair")
	}
	signals := f.host.signalsSent()
	sawWrapper, sawListener := false, false
	for _, signal := range signals {
		if signal.pid == wrapper {
			sawWrapper = true
		}
		if signal.pid == listener {
			sawListener = true
		}
	}
	if !sawWrapper || !sawListener {
		t.Fatalf("signals = %v, want both the wrapper %d and the listener %d ended", signals, wrapper, listener)
	}
	if f.host.isAlive(wrapper) || f.host.isAlive(listener) {
		t.Fatal("a process from the failed start survived the cleanup")
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left its runtime record behind")
	}
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q with the port free again", status.State, StateStopped)
	}
}

// TestFailedStartCleansUpWhenTheLeaderDiesFirst is the regression test for a
// cleanup that mistook "the leader was reaped" for "the group is empty": the
// wrapper answers the graceful request but the server ignores it, and the
// cleanup must still force the group down rather than leaving the server
// serving with no record.
func TestFailedStartCleansUpWhenTheLeaderDiesFirst(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	f.host.listenerIgnoresGrace = true
	f.host.ready = false
	f.Settings.StartTimeout = 200 * time.Millisecond

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Failure)

	listener := f.host.servedByListener
	if listener == 0 {
		t.Fatal("the fixture did not spawn the wrapper-and-listener pair")
	}
	if f.host.isAlive(listener) {
		t.Fatal("the server survived the cleanup after its wrapper died first")
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left its runtime record behind")
	}
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q with the port free again", status.State, StateStopped)
	}
}

// TestStartRefusesWhenTheListenerCannotBeNamed is the regression test for
// "cannot look" being read as "not ready": when the server binds the port but
// the platform will not attribute it, the start must refuse instead of waiting
// out the whole timeout and killing a possibly healthy server on a false
// premise.
//
// The refusal is bounded rather than instantaneous — the other half of the rule
// is TestStartSurvivesAListenerThatIsUnnameableForAMoment — so what this pins is
// that a port that stays unnameable is refused long before the start timeout,
// with the exit code of a precondition that could not be verified.
func TestStartRefusesWhenTheListenerCannotBeNamed(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	f.host.opaqueServed = true
	f.Settings.StartTimeout = 10 * time.Second

	start := time.Now()
	_, err := f.Start(context.Background())
	elapsed := time.Since(start)
	// Preflight: like the sibling "the port was taken by another process"
	// race, the start refused a precondition it could not verify, and the
	// innermost classification wins the exit code even after the cleanup.
	wantCode(t, err, exitcode.Preflight)
	if elapsed > 5*time.Second {
		t.Fatalf("start waited %s instead of refusing promptly", elapsed)
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left its runtime record behind")
	}
}

// TestStartSurvivesAListenerThatIsUnnameableForAMoment pins the other half of
// the same rule: one sample is not a verdict.
//
// The port probe chain asks a per-process probe first and a whole-table probe
// after it, and the two read the kernel through different interfaces: a socket
// the server created a moment ago can be in the table and not yet in the process
// scan. Treating that first sample as final failed a start for a server dshctl
// had just spawned itself, which is what a busy machine produced. The state is
// therefore re-examined for a moment — nothing is concluded from it — and the
// start succeeds once the owner can be named.
func TestStartSurvivesAListenerThatIsUnnameableForAMoment(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	spawn := f.Spawn
	f.Spawn = func(path string, args []string, dir string, env []string, log *os.File) (int, func(context.Context) bool, error) {
		// The listener exists from here on, but the first probes cannot name it.
		f.host.mu.Lock()
		f.host.unnameablePolls = 2
		f.host.mu.Unlock()
		return spawn(path, args, dir, env, log)
	}

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.Status.Owning() {
		t.Fatalf("status = %+v, want the started server", result.Status)
	}
	if _, ok := f.stateRecord(t); !ok {
		t.Fatal("a successful start must leave its runtime record behind")
	}
}

// TestStopReportsAProcessThatSurvivesTheForceSignal pins the last-resort answer:
// a server that ignores both the graceful request and the force signal is
// reported as not ended instead of being claimed as stopped.
func TestStopReportsAProcessThatSurvivesTheForceSignal(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.mu.Lock()
	f.host.processes[4321].survivesGraceful = true
	f.host.processes[4321].survivesForce = true
	f.host.mu.Unlock()

	_, err := f.Stop(context.Background())
	wantCode(t, err, exitcode.Failure)
	if !strings.Contains(err.Error(), "强制结束后仍然存在") {
		t.Fatalf("Stop error = %v, want a still-alive report", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}, {4321, host.Force}})
	if !f.host.isAlive(4321) {
		t.Fatal("the stubborn process was reported as ended")
	}
}

// TestStopReportsAnotherHarnessServer pins the StateForeign branch of stop: a
// DeepSeek Harness server of a *different* checkout owns the port, so the stop
// reports it and leaves it alone.
func TestStopReportsAnotherHarnessServer(t *testing.T) {
	f := newFixture(t)
	f.host.serving(4242, "node /opt/other/deepseek-harness/apps/cli/lib/bin.js web --port 3080")

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if result.Status.State != StateForeign {
		t.Fatalf("state = %q, want %q", result.Status.State, StateForeign)
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(4242) {
		t.Fatal("a foreign harness server was ended")
	}
	if !strings.Contains(f.errOut.String(), "非 DSH 进程占用") {
		t.Fatalf("stderr = %q", f.errOut.String())
	}
}

// TestStopRefusesToSignalARecycledPID is the regression test for signalling a
// pid that the operating system handed to an unrelated process.
func TestStopRefusesToSignalARecycledPID(t *testing.T) {
	f := newFixture(t)
	// The record names pid 4321 with one start time; the live pid 4321 is a
	// different process with a different start time.
	f.host.serving(4321, "/usr/bin/tail -f /var/log/system.log")
	if err := f.Record.Save(state.Record{
		PID:       4321,
		StartedAt: 1_600_000_000,
		Port:      f.Settings.Port,
		Phase:     state.PhaseRunning,
	}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(4321) {
		t.Fatal("the unrelated process was ended")
	}
}

// TestStopReportsAnUnverifiableListener pins that a stop never guesses: an
// unowned listener is reported, the process survives, and the exit code is not
// a silent success.
func TestStopReportsAnUnverifiableListener(t *testing.T) {
	f := newFixture(t)
	f.host.serving(7777, "python3 -m http.server 3080")

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !result.Unverifiable {
		t.Fatal("the result must report that the listener could not be verified")
	}
	f.wantNoSignals(t)
	if !f.host.isAlive(7777) {
		t.Fatal("an unverifiable listener was ended")
	}
	if !strings.Contains(f.errOut.String(), "无法确认") {
		t.Fatalf("stderr = %q", f.errOut.String())
	}
}

// TestStopReportsAStrangerOnThePort pins that a stop of a port held by a
// process with no record at all is reported as unverifiable rather than as
// "not running".
func TestStopReportsAStrangerOnThePort(t *testing.T) {
	f := newFixture(t)
	f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !result.Unverifiable {
		t.Fatal("the result must report that the port could not be claimed")
	}
	f.wantNoSignals(t)
	if !strings.Contains(f.errOut.String(), "无法确认") {
		t.Fatalf("stderr = %q", f.errOut.String())
	}
}

// TestStopSaysSoWhenNothingRuns pins the ordinary "there was nothing to stop"
// answer, which must still clear a stale record.
func TestStopSaysSoWhenNothingRuns(t *testing.T) {
	f := newFixture(t)
	if err := f.Record.Save(state.Record{PID: 9999, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(f.out.String(), "未在运行") {
		t.Fatalf("output = %q", f.out.String())
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a stale record survived the stop")
	}
}

// TestRestartStopsThenStarts pins the whole cycle under one lock.
func TestRestartStopsThenStarts(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true

	result, err := f.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}})
	if result.SpawnedPID == 0 {
		t.Fatalf("restart did not start a server: %s", f.describeCommands())
	}
	record, ok := f.stateRecord(t)
	if !ok || record.SpawnedPID != result.SpawnedPID {
		t.Fatalf("record = %+v (ok=%v), want spawned pid %d", record, ok, result.SpawnedPID)
	}
}

// TestRestartRefusesWhileAForeignProgramOwnsThePort pins that a restart does not
// silently become "start failed".
func TestRestartRefusesWhileAForeignProgramOwnsThePort(t *testing.T) {
	f := newFixture(t)
	f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")

	_, err := f.Restart(context.Background())
	wantCode(t, err, exitcode.Preflight)
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
}

// TestStatusClassifiesEveryState walks the whole state machine.
func TestStatusClassifiesEveryState(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(*fixture, *testing.T)
		want      string
		wantStale bool
	}{
		{
			name:  "stopped",
			setup: func(*fixture, *testing.T) {},
			want:  StateStopped,
		},
		{
			name: "running",
			setup: func(f *fixture, t *testing.T) {
				f.startServer(t, 4321, "")
			},
			want: StateRunning,
		},
		{
			name: "starting",
			setup: func(f *fixture, t *testing.T) {
				// The recorded process owns the port but does not answer yet.
				f.host.add(4321, "pnpm --dir repo dsh web", fixtureStartTime)
				f.host.listen(4321)
				f.host.ready = false
				if err := f.Record.Save(state.Record{
					PID: 4321, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: state.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			want: StateStarting,
		},
		{
			name: "harness server of another checkout",
			setup: func(f *fixture, t *testing.T) {
				f.host.serving(4242, "node /opt/other/deepseek-harness/apps/cli/lib/bin.js web --port 3080")
			},
			want: StateForeign,
		},
		{
			name: "unrelated program",
			setup: func(f *fixture, t *testing.T) {
				f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")
			},
			want: StateOrphan,
		},
		{
			name: "unmanaged listener",
			setup: func(f *fixture, t *testing.T) {
				f.host.serving(7777, "python3 -m http.server 3080")
			},
			want: StateOrphan,
		},
		{
			// The recorded pid now belongs to a different process: the record is
			// stale and the listener is positively a stranger.
			name: "recorded pid recycled",
			setup: func(f *fixture, t *testing.T) {
				f.host.add(4321, "pnpm --dir repo dsh web", fixtureStartTime+10_000)
				f.host.listen(4321)
				if err := f.Record.Save(state.Record{
					PID: 4321, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: state.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			want:      StateForeign,
			wantStale: true,
		},
		{
			// The recorded process is gone and a stranger took the port.
			name: "recorded process gone, port taken",
			setup: func(f *fixture, t *testing.T) {
				f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")
				if err := f.Record.Save(state.Record{
					PID: 5555, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			want:      StateForeign,
			wantStale: true,
		},
		{
			// The recorded process is alive, but a different process holds the
			// port: neither stopped nor ours, and the record is kept because it
			// still names a live server dshctl started.
			name: "recorded process alive, port taken by another",
			setup: func(f *fixture, t *testing.T) {
				f.host.add(5555, "pnpm --dir repo dsh web", fixtureStartTime)
				f.host.serving(4242, "/usr/sbin/nginx -g daemon off;")
				if err := f.Record.Save(state.Record{
					PID: 5555, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: state.PhaseRunning,
				}); err != nil {
					t.Fatalf("save record: %v", err)
				}
			},
			want: StateOrphan,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newFixture(t)
			testCase.setup(f, t)
			status, err := f.Status(context.Background(), f.Settings.Port)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if status.State != testCase.want {
				t.Fatalf("state = %q, want %q", status.State, testCase.want)
			}
			if status.RecordStale != testCase.wantStale {
				t.Fatalf("RecordStale = %v, want %v", status.RecordStale, testCase.wantStale)
			}
		})
	}
}

// TestStatusReportsAnUninspectablePortAsAnError pins that a failed probe is a
// Preflight error, never a "not running" answer.
func TestStatusReportsAnUninspectablePortAsAnError(t *testing.T) {
	f := newFixture(t)
	f.host.listenErr = errors.New("lsof: operation not permitted")

	_, err := f.Status(context.Background(), f.Settings.Port)
	wantCode(t, err, exitcode.Preflight)
}

// TestStatusReportsARecordThatDescribesNothing pins that a record naming a dead
// pid is reported as stale and handed to the caller, which retires it.
func TestStatusReportsARecordThatDescribesNothing(t *testing.T) {
	f := newFixture(t)
	if err := f.Record.Save(state.Record{PID: 5555, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning}); err != nil {
		t.Fatalf("save record: %v", err)
	}

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q", status.State, StateStopped)
	}
	if !status.RecordStale || status.StaleRecord == nil {
		t.Fatal("a record naming a dead pid must be reported as stale with the record attached")
	}
	if status.StaleRecord.PID != 5555 {
		t.Fatalf("stale record = %+v, want pid 5555", status.StaleRecord)
	}
}

// TestStatusCreatesNothingAtAll pins the read-only boundary by observation
// rather than by inspecting a chosen file.
//
// An earlier version of this test removed the lock file and then only checked
// the returned state, so it passed while Status was creating the state
// directory, the config file and a new lock. The assertion below is what makes
// the promise real: after a status, the state directory must not exist.
func TestStatusCreatesNothingAtAll(t *testing.T) {
	f := newFixture(t)
	if err := os.RemoveAll(f.state); err != nil {
		t.Fatalf("remove state: %v", err)
	}

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q", status.State, StateStopped)
	}
	for _, path := range []string{f.state, f.Settings.ConfigPath, f.Settings.LogPath, f.Settings.StateFile(), f.Settings.LockFile()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("status created %s", path)
		}
	}
}

// TestStatusReportsStaleWithoutRetiringIt pins the other half of being
// read-only: a stale record is reported so the operator can see it, and it is
// still on disk afterwards. The next mutating command is what retires it.
func TestStatusReportsStaleWithoutRetiringIt(t *testing.T) {
	f := newFixture(t)
	stale := state.Record{PID: 999999, StartedAt: 1_600_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning}
	if err := f.Record.Save(stale); err != nil {
		t.Fatalf("save record: %v", err)
	}

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.RecordStale {
		t.Fatal("a record naming a dead pid must be reported as stale")
	}
	if status.StaleRecord == nil || status.StaleRecord.PID != stale.PID {
		t.Fatalf("stale record = %+v, want pid %d", status.StaleRecord, stale.PID)
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID != stale.PID {
		t.Fatalf("record = %+v (ok=%v), want it left in place", record, ok)
	}
}

// TestStatusDoesNotTakeTheLock pins that a status beside a running operation
// still answers, because it does not wait for the operation lock.
func TestStatusDoesNotTakeTheLock(t *testing.T) {
	f := newFixture(t)
	held, err := lock.Acquire(context.Background(), f.Settings.LockFile(), time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	// Another operation holds the lock, which a lock-taking status would wait on
	// for the whole lock timeout.
	start := time.Now()
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("status waited %s for the lock", elapsed)
	}
	if !status.LockHeld || status.LockHolder != lockHolderThroughLock(os.Getpid()) {
		t.Fatalf("status = %+v, want it to report the holder without waiting", status)
	}
}

// TestBuildWritesTheSectionBeforeRotating pins that a build section and its body
// stay in the same file, which is what makes `logs --build` work.
func TestBuildWritesTheSectionBeforeRotating(t *testing.T) {
	f := newFixture(t)
	f.Settings.LogRotateBytes = 512
	f.Log = logfile.New(f.Settings.LogPath, f.Settings.LogRotateBytes)
	// Fill the log so the next build rotates it.
	if err := f.Log.Line(strings.Repeat("x", 1200)); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "pnpm" && hasArgument(cmd, "run", "build") {
			return emitStdout(cmd.Stdout, "build output line\n")
		}
		return nil
	}
	// The prune's tracking query is answered by the fixture's fake git, not by
	// the real checkout: the tracked set is the fixture's scripted answer.

	if err := f.RunBuild(context.Background()); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	body, outcome, err := logfile.LastSection(f.Settings.LogPath, buildSectionTitles)
	if err != nil {
		t.Fatalf("LastSection: %v", err)
	}
	if outcome != logfile.Found {
		t.Fatalf("outcome = %d, want Found; live sections = %v, backup sections = %v",
			outcome, sectionTitles(t, f.Settings.LogPath), sectionTitles(t, f.Settings.LogPath+".old"))
	}
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "build output line") {
		t.Fatalf("section body = %q, want the build output", joined)
	}
}

// TestBuildRefusesANonCheckout pins that the destructive prune is only ever run
// against a directory that is actually the managed checkout.
func TestBuildRefusesANonCheckout(t *testing.T) {
	f := newFixture(t)
	other := filepath.Join(f.root, "other")
	writeFile(t, filepath.Join(other, "package.json"), "{}")
	writeFile(t, filepath.Join(other, "node_modules", ".keep"), "")
	f.Settings.RepoDir = other
	f.Repo.Dir = other

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "DeepSeek Harness")
	for _, command := range f.host.commandsRun() {
		if strings.Contains(command, "run build") {
			t.Fatalf("a non-checkout reached the build: %v", f.host.commandsRun())
		}
	}
}

// TestUpdateSurfacesTheBuildFailure pins the error attribution: a failed build
// is reported as a failed build, not as a missing build record.
func TestUpdateSurfacesTheBuildFailure(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "pnpm" && hasArgument(cmd, "run", "build") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "pnpm run build 失败")
	if strings.Contains(err.Error(), "尚未构建") {
		t.Fatalf("the build failure was masked by the missing build record: %v", err)
	}
}

// TestUpdateRestoresTheServiceWhenTheSwitchFails pins that the original failure
// is what the operator sees, and that the old build keeps serving.
func TestUpdateRestoresTheServiceWhenTheSwitchFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "git" && hasArgument(cmd, "merge", "--ff-only") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "更新失败")
	// The old server was stopped, then the old build was started again.
	if signals := f.host.signalsSent(); len(signals) != 1 || signals[0] != (fakeSignal{4321, host.Graceful}) {
		t.Fatalf("signals = %v, want the old server stopped once", signals)
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID == 4321 {
		t.Fatalf("record = %+v (ok=%v), want the restored server", record, ok)
	}
	if !strings.Contains(f.out.String(), "恢复启动旧版本") {
		t.Fatalf("output = %q", f.out.String())
	}
	if !strings.Contains(f.out.String(), "启动成功") {
		t.Fatalf("the restored server was not reported as started: %q", f.out.String())
	}
}

// TestUpdateRefusesWhileAnUnidentifiedServerRuns pins that an update never
// fetches, switches and rebuilds underneath a process it cannot vouch for.
func TestUpdateRefusesWhileAnUnidentifiedServerRuns(t *testing.T) {
	f := newFixture(t)
	f.host.serving(7777, "python3 -m http.server 3080")

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	for _, command := range f.host.commandsRun() {
		if strings.Contains(command, "fetch") || strings.Contains(command, "install") {
			t.Fatalf("the update ran under an unidentified server: %v", f.host.commandsRun())
		}
	}
}

// TestUpdateLeavesTheServiceStoppedWhenInstallFails pins the documented failure
// semantics of the install step.
func TestUpdateLeavesTheServiceStoppedWhenInstallFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "pnpm" && hasArgument(cmd, "install") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "保持停止")
	f.wantNoSpawn(t)
}

// TestUpdateRestoresTheServiceOnSuccess pins the full happy path.
func TestUpdateRestoresTheServiceOnSuccess(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID == 4321 {
		t.Fatalf("record = %+v (ok=%v), want a freshly started server", record, ok)
	}
	if record.Phase != state.PhaseRunning {
		t.Fatalf("phase = %q, want running", record.Phase)
	}
}

// TestBuildDoesNotPruneOutsideTheCheckout pins the prune's outermost safety
// rule: a symlinked packages/ directory must never let the residue walk delete
// anything outside the checkout.
//
// The victim sits at the depth the walk reaches — packages/<group>/<name> — so
// that the boundary check is what saves it: a candidate whose resolved path is
// outside the repository must be left alone. A victim at depth one would never
// become a candidate at all, and the guard could disappear without this test
// noticing.
func TestBuildDoesNotPruneOutsideTheCheckout(t *testing.T) {
	f := newFixture(t)
	outside := filepath.Join(f.root, "outside")
	victim := filepath.Join(outside, "grp", "pkg", "lib", "important.js")
	writeFile(t, victim, "keep me")
	if err := os.Symlink(outside, filepath.Join(f.repo, "packages")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if err := f.RunBuild(context.Background()); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a directory outside the checkout was pruned: %v", err)
	}
}

// TestLogsPrintsTheTailAndFollowsWithoutGaps pins that following starts where
// the tail ended.
func TestLogsPrintsTheTailAndFollowsWithoutGaps(t *testing.T) {
	f := newFixture(t)
	if err := f.Log.Line("first"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.Log.SetPollInterval(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Logs(ctx, LogsOptions{Lines: 10, Follow: true}) }()

	// Give the follow a moment to attach, then write.
	time.Sleep(50 * time.Millisecond)
	if err := f.Log.Line("second"); err != nil {
		t.Fatalf("append: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(f.out.String(), "second") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Logs error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Logs did not stop after cancellation")
	}
	if !strings.Contains(f.out.String(), "first") || !strings.Contains(f.out.String(), "second") {
		t.Fatalf("output = %q, want both lines", f.out.String())
	}
}

// TestLogsBuildOnlyReportsAMissingRecord pins the honest message when a log has
// never recorded a build.
func TestLogsBuildOnlyReportsAMissingRecord(t *testing.T) {
	f := newFixture(t)
	if err := f.Log.Line("just server output"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := f.Logs(context.Background(), LogsOptions{BuildOnly: true}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !strings.Contains(f.errOut.String(), "没有 build/update/rollback 记录") {
		t.Fatalf("stderr = %q", f.errOut.String())
	}
}

// TestWebURLPrefersTheRecordedAddress pins that the token survives log rotation.
func TestWebURLPrefersTheRecordedAddress(t *testing.T) {
	f := newFixture(t)
	announced := fmt.Sprintf("http://127.0.0.1:%d/?token=abc", f.Settings.Port)
	f.startServer(t, 4321, announced)
	// The log holds an older address that must not win.
	if err := f.Log.Line("dsh web: " + announced + "-stale"); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	got, err := f.WebURL(context.Background())
	if err != nil {
		t.Fatalf("WebURL: %v", err)
	}
	if got != announced {
		t.Fatalf("WebURL = %q, want the recorded address %q", got, announced)
	}
}

// TestWebURLFallsBackToTheLog pins that a running server whose record carries no
// address can still be found through the log.
func TestWebURLFallsBackToTheLog(t *testing.T) {
	f := newFixture(t)
	announced := fmt.Sprintf("http://127.0.0.1:%d/?token=fromlog", f.Settings.Port)
	if err := f.Log.Line("dsh web: " + announced); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	// A running server with an address-less record.
	f.startServer(t, 4321, "")

	got, err := f.WebURL(context.Background())
	if err != nil {
		t.Fatalf("WebURL: %v", err)
	}
	if got != announced {
		t.Fatalf("WebURL = %q, want %q", got, announced)
	}
}

// TestWebURLRefusesAMissingAddress pins the error instead of printing nothing.
func TestWebURLRefusesAMissingAddress(t *testing.T) {
	f := newFixture(t)
	if err := f.Log.Line("no address here"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.startServer(t, 4321, "")

	_, err := f.WebURL(context.Background())
	wantCode(t, err, exitcode.Failure)
}

// TestWebURLDoesNotLeakAnUnrelatedURL pins the pattern: only the line the server
// prints on its own counts, so a diagnostic that mentions a URL is ignored.
func TestWebURLDoesNotLeakAnUnrelatedURL(t *testing.T) {
	f := newFixture(t)
	if err := f.Log.Line("see https://example.com/help for details"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.startServer(t, 4321, "")
	if _, err := f.WebURL(context.Background()); err == nil {
		t.Fatal("an unrelated URL was reported as the server address")
	}
}

// TestDoctorReportsEveryItem pins that the diagnosis covers the environment and
// that a missing checkout is a failure rather than a silent pass.
func TestDoctorReportsEveryItem(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	checks := f.Doctor(context.Background())
	byName := map[string]Check{}
	for _, check := range checks {
		byName[check.Name] = check
	}
	for _, want := range []string{"状态目录", "配置文件", "仓库版本", "依赖", "构建产物", "Node", "pnpm", "端口", "运行记录", "操作锁", "日志"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("doctor did not report %q: %+v", want, checks)
		}
	}
	if byName["端口"].Status != CheckOK {
		t.Fatalf("port check = %+v, want ok while the managed server runs", byName["端口"])
	}
	if byName["运行记录"].Status != CheckOK {
		t.Fatalf("record check = %+v, want ok", byName["运行记录"])
	}
}

// TestDoctorFlagsANonCheckout pins that a mistyped repository is a hard failure.
func TestDoctorFlagsANonCheckout(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.repo, "package.json")); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	checks := f.Doctor(context.Background())
	if !ChecksFailed(checks) {
		t.Fatalf("doctor passed a non-checkout: %+v", checks)
	}
}

// wantCode asserts the exit code carried by an error.
func wantCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with exit code %d, got success", want)
	}
	if got := exitcode.Of(err); got != want {
		t.Fatalf("exit code = %d (%v), want %d", got, err, want)
	}
}

// wantContains asserts that an error mentions a phrase.
func wantContains(t *testing.T, err error, phrase string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got success", phrase)
	}
	if !strings.Contains(err.Error(), phrase) {
		t.Fatalf("error %q does not mention %q", err, phrase)
	}
}

// emitStdout writes canned output to a command's standard output.
func emitStdout(w interface{ Write([]byte) (int, error) }, text string) error {
	if w == nil {
		return nil
	}
	_, err := w.Write([]byte(text))
	return err
}
