package service

import (
	"context"
	"os"
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

// The tests in this file pin the multi-instance model: one state directory
// manages every server dshctl started, each with its own record, and a command
// without an explicit port acts on all of them. The failure they guard against
// was reported from a real machine — a server started with `--port 3081`
// disappeared from `status` and survived `stop` the moment a second server was
// started on the configured port, so it could never be managed again.

// TestMovingTheFixturePortMovesItsRecordStore is the guard on the fixture
// itself: `New` builds the record store and the log from the resolved settings,
// so a test whose settings moved to another port must read and write the same
// file that port's own start writes.
//
// The fixture used to pin both at construction, which made a moved fixture read
// a file no command had written. Nothing caught it because the assertions that
// would have noticed went through port-keyed helpers of their own, and a test
// that reads the wrong file can pass while describing a machine that does not
// exist — the failure mode this file exists to rule out.
//
// The port drawn below is a second port of the same fixture and cannot be the
// one the fixture was built on: reserveFreePort never hands a port out twice
// (see TestReserveFreePortDoesNotHandOutAPortTwice).
func TestMovingTheFixturePortMovesItsRecordStore(t *testing.T) {
	f := newFixture(t)
	other := reserveFreePort(t)
	configuredPort(t, f, other)

	if f.Record.Path != f.Settings.StateFile() {
		t.Fatalf("the fixture reads %s while the service writes %s",
			f.Record.Path, f.Settings.StateFile())
	}
	// The record the port's own start would have left has to be the one the
	// fixture reads back, not merely the one it writes.
	serveOnConfiguredPort(t, f, 4321)
	if record, ok := f.stateRecord(t); !ok || record.Port != other {
		t.Fatalf("record = %+v (found %v), want the record of port %d", record, ok, other)
	}
}

// saveRecordFor writes a record for one port, in the shape that port's own
// start would have written it.
func saveRecordFor(t *testing.T, f *fixture, record state.Record) {
	t.Helper()
	store := state.Store{Path: filepath.Join(f.state, recordFileName(t, record.Port))}
	if err := store.Save(record); err != nil {
		t.Fatalf("save the record for port %d: %v", record.Port, err)
	}
}

// recordFileName spells the record file of one port the way the settings do, so
// a test never hardcodes the pattern.
func recordFileName(t *testing.T, port int) string {
	t.Helper()
	settings := config.Default(t.TempDir())
	settings.Port = port
	return filepath.Base(settings.StateFile())
}

// sameInstallation binds the fixture to the port it runs on, so a test can start
// a second instance of one installation by moving to another port. It also
// registers the cleanup, so no test here can leave a detached server behind on
// the machine it runs on.
func sameInstallation(t *testing.T, f *fixture) {
	t.Helper()
	checkout, port := f.Settings.RepoDir, f.Settings.Port
	f.run(t, config.Overrides{RepoDir: &checkout, Port: &port})
	explicitPort(t, f, port)
	t.Cleanup(func() {
		// The cleanup resolves the checkout once more and then stops every
		// instance this state directory manages, so a failure anywhere in a test
		// cannot leave a server running.
		f.run(t, config.Overrides{RepoDir: &checkout})
		explicitPort(t, f, f.Settings.Port)
		if _, err := f.StopAll(context.Background()); err != nil {
			t.Logf("cleanup stop: %v", err)
		}
	})
}

// explicitPort moves the fixture to a port that was named on the command line.
// A named port is the operator asking for one instance, which is what the
// selection model treats as a decision.
func explicitPort(t *testing.T, f *fixture, port int) {
	t.Helper()
	rebuildSettings(t, f, port)
	f.Settings.Sources.Port = "flag"
}

// configuredPort moves the fixture to a port the settings name rather than the
// command line.
//
// The distinction is the whole point of the multi-instance model: a port named
// on the command line is a decision to act on that instance alone, so a test
// about a bare command has to describe a machine where nobody named one.
func configuredPort(t *testing.T, f *fixture, port int) {
	t.Helper()
	rebuildSettings(t, f, port)
	f.Settings.Sources.Port = "file"
}

// rebuildSettings rewrites the fixture's settings around one port, carrying the
// checkout over because a document that names the port says nothing about the
// checkout.
func rebuildSettings(t *testing.T, f *fixture, port int) {
	t.Helper()
	checkout := f.Settings.RepoDir
	// The wait budgets are pacing for the fictional machine rather than policy,
	// and a test may have shortened them on the fixture it owns: carry them over
	// rather than resetting them here, so a caller's budget survives the rebuild.
	startTimeout, stopTimeout := f.Settings.StartTimeout, f.Settings.StopTimeout
	f.Settings = config.Default(f.root)
	f.Settings.RepoDir = checkout
	f.Settings.StateDir = f.state
	f.Settings.ConfigPath = filepath.Join(f.state, "config.json")
	f.Settings.LogPath = filepath.Join(f.state, "dsh-web.log")
	f.Settings.Port = port
	if startTimeout <= 0 {
		startTimeout = 2 * time.Second
	}
	if stopTimeout <= 0 {
		stopTimeout = 2 * time.Second
	}
	f.Settings.StartTimeout = startTimeout
	f.Settings.StopTimeout = stopTimeout
	f.rebind()
	f.Repo.Dir = checkout
}

// serveOnConfiguredPort makes the fixture's fictional machine serve the port the
// settings now name, with a record that matches — the fixture-side half of
// "this installation started a server here".
func serveOnConfiguredPort(t *testing.T, f *fixture, pid int) {
	t.Helper()
	f.host.servingOnPort(f.Settings.Port, pid, "pnpm --dir repo dsh web")
	saveRecordFor(t, f, state.Record{
		PID: pid, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: state.PhaseRunning,
	})
}

// seedRunningPort makes a port other than the fixture's own look like a server
// this state directory started: a listener, a matching record, and the pid.
func seedRunningPort(t *testing.T, f *fixture, port, pid int, url string) {
	t.Helper()
	f.host.servingOnPort(port, pid, "pnpm --dir repo dsh web")
	saveRecordFor(t, f, state.Record{
		PID: pid, StartedAt: fixtureStartTime, Port: port,
		URL: url, Phase: state.PhaseRunning,
	})
}

// wantRecordForPort asserts that one port's record is on disk, or that it is
// gone.
func wantRecordForPort(t *testing.T, f *fixture, port int, want bool) {
	t.Helper()
	path := filepath.Join(f.state, recordFileName(t, port))
	_, err := os.Lstat(path)
	if want && err != nil {
		t.Fatalf("port %d: the record must exist, got err=%v", port, err)
	}
	if !want && !os.IsNotExist(err) {
		t.Fatalf("port %d: the record must be gone, got err=%v", port, err)
	}
}

// wantSignalOn asserts that a signal reached one pid.
func wantSignalOn(t *testing.T, f *fixture, pid int) {
	t.Helper()
	for _, signal := range f.host.signalsSent() {
		if signal.pid == pid {
			return
		}
	}
	t.Fatalf("no signal reached pid %d; delivered %v", pid, f.host.signalsSent())
}

// wantNoSignalOn asserts that one pid was never signalled, which is how these
// tests separate "acted on what it selected" from "acted on everything".
func wantNoSignalOn(t *testing.T, f *fixture, pid int) {
	t.Helper()
	for _, signal := range f.host.signalsSent() {
		if signal.pid == pid {
			t.Fatalf("pid %d was signalled although it was not selected: %v", pid, f.host.signalsSent())
		}
	}
}

// wantStatesByName asserts the observed state of every port, as a map so that a
// failure says which port disagrees instead of which index does.
func wantStatesByName(t *testing.T, statuses []Status, want map[int]string) {
	t.Helper()
	got := map[int]string{}
	for _, status := range statuses {
		got[status.Port] = status.State
	}
	if len(got) != len(want) {
		t.Fatalf("observed ports = %v, want %v", got, want)
	}
	for port, wantState := range want {
		if got[port] != wantState {
			t.Fatalf("port %d state = %q, want %q (all: %v)", port, got[port], wantState, got)
		}
	}
}

// TestStatusReportsEveryManagedPort pins discovery: a server started with an
// explicit port is still reported after the configured port has a server of its
// own, and each is named by its own port.
func TestStatusReportsEveryManagedPort(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	statuses, err := f.Statuses(context.Background())
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	wantStatesByName(t, statuses, map[int]string{
		configured: StateRunning,
		other:      StateRunning,
	})
	for _, status := range statuses {
		if status.ListenerPID == 0 {
			t.Fatalf("port %d reported no listener: %+v", status.Port, status)
		}
	}
}

// TestStatusWithoutPortsListsNothing pins the boundary of discovery: a state
// directory with no records observes exactly the configured port, so a machine
// that never started a server does not look like it has several.
func TestStatusWithoutPortsListsNothing(t *testing.T) {
	f := newFixture(t)

	statuses, err := f.Statuses(context.Background())
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("observed %d ports, want only the configured one: %+v", len(statuses), statuses)
	}
	if statuses[0].Port != f.Settings.Port || statuses[0].State != StateStopped {
		t.Fatalf("status = %+v, want the configured port reported stopped", statuses[0])
	}
}

// TestExplicitPortSelectsOneInstance pins that --port still narrows every
// command to one instance: the configured port may be serving, and a command
// that named another port must not touch it.
func TestExplicitPortSelectsOneInstance(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	explicitPort(t, f, other)

	statuses, err := f.Statuses(context.Background())
	if err != nil {
		t.Fatalf("Statuses: %v", err)
	}
	wantStatesByName(t, statuses, map[int]string{other: StateRunning})

	stopped, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if len(stopped.Results) != 1 || stopped.Results[0].Status.Port != other {
		t.Fatalf("stopped %+v, want only port %d", stopped.Results, other)
	}
	if !f.host.isAlive(4321) {
		t.Fatalf("the server on port %d was ended although %d was selected", configured, other)
	}
	wantNoSignalOn(t, f, 4321)
	wantRecordForPort(t, f, configured, true)
	wantRecordForPort(t, f, other, false)
}

// TestStopEndsEveryManagedServer is the regression test for the reported bug: a
// bare stop must end the server on the configured port *and* the one started
// with an explicit port, and it must retire both records.
func TestStopEndsEveryManagedServer(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	stopped, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if len(stopped.Results) != 2 {
		t.Fatalf("stopped %d instances, want 2: %+v", len(stopped.Results), stopped.Results)
	}
	if f.host.isAlive(4321) || f.host.isAlive(4322) {
		t.Fatalf("a managed server survived the stop: 4321=%v 4322=%v",
			f.host.isAlive(4321), f.host.isAlive(4322))
	}
	wantSignalOn(t, f, 4321)
	wantSignalOn(t, f, 4322)
	wantRecordForPort(t, f, configured, false)
	wantRecordForPort(t, f, other, false)
}

// TestStopReportsEveryPortItEnded pins the output of a multi-port stop: an
// operator who started several servers has to be able to see that all of them
// were ended, not just the one the configuration happens to name.
func TestStopReportsEveryPortItEnded(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	if _, err := f.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	for _, port := range []int{configured, other} {
		if !strings.Contains(f.out.String(), strconv.Itoa(port)) {
			t.Fatalf("the stop report never names port %d:\n%s", port, f.out.String())
		}
	}
	if got := strings.Count(f.out.String(), "已停止"); got != 2 {
		t.Fatalf("the stop report announces %d stops, want 2:\n%s", got, f.out.String())
	}
}

// TestStopEndsEveryInstanceThatIsRunning pins the mixed case: one live instance
// and one whose process is gone. Both are reconciled, and only the live one is
// signalled.
func TestStopEndsEveryInstanceThatIsRunning(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	// A record for another port whose process is gone: residue that a bare stop
	// has to retire, or `status` keeps reporting a server that does not exist.
	saveRecordFor(t, f, state.Record{
		PID: 9001, StartedAt: fixtureStartTime, Port: other, Phase: state.PhaseRunning,
	})

	if _, err := f.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	wantRecordForPort(t, f, other, false)
	wantRecordForPort(t, f, configured, false)
	if f.host.isAlive(4321) {
		t.Fatal("the live instance survived the stop")
	}
	wantNoSignalOn(t, f, 9001)
}

// TestStopAllLeavesAForeignListenerAloneAcrossPorts pins the safety rule on the
// multi-port path: a port held by a stranger is reported and skipped, the other
// instances are still ended, and the stop says which of the two it was.
//
// The two shapes are kept apart because they answer different questions. A port
// with no record of ours was never ours to end, so leaving it alone is the whole
// answer. A port whose record names a process of ours that is still alive is an
// instance this command was asked to end and did not — the stop is incomplete,
// and a script that stops a machine and moves on has to be able to tell.
func TestStopAllLeavesAForeignListenerAloneAcrossPorts(t *testing.T) {
	f := newFixture(t)
	// The stop budget is not what this test asserts, and the instance it
	// cannot end keeps the port past the deadline: a short budget keeps the
	// wait (and the same wait under the deadline mutation) proportional to
	// what is being pinned.
	f.Settings.StopTimeout = 100 * time.Millisecond
	// The second instance is discovered rather than named, which is the model
	// under test: a bare stop covers every port this state directory knows.
	other := reserveFreePort(t)
	configuredPort(t, f, other)

	f.startServer(t, 4321, "")
	// A stranger on the configured port, with no record of ours: a port this
	// state directory never started anything on, and not an instance the stop was
	// asked about.
	f.host.servingOnPort(f.Settings.Port, 7001, "/usr/sbin/nginx -g daemon off;")
	// A discovered instance whose process cannot be vouched for: its record names
	// a live pid whose start time disagrees, and no wrapper group to fall back on.
	// The stop was asked to end this one and must not guess, which is what makes
	// the whole operation incomplete.
	strangerPort := reserveFreePort(t)
	f.host.servingOnPort(strangerPort, 7001, "/usr/sbin/nginx -g daemon off;")
	f.host.add(9999, "pnpm --dir repo dsh web", fixtureStartTime+60)
	saveRecordFor(t, f, state.Record{
		PID: 9999, StartedAt: fixtureStartTime, Port: strangerPort, Phase: state.PhaseRunning,
	})

	stopped, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	wantNoSignalOn(t, f, 7001)
	if !f.host.isAlive(7001) {
		t.Fatal("a stranger on another port was ended")
	}
	if f.host.isAlive(4321) {
		t.Fatal("the healthy instance survived the stop")
	}
	// The bare stop did not cover everything it was asked to, and says so: a
	// script that stops a machine and moves on has to be able to tell that a
	// server it meant to end is still there.
	if !stopped.Unverifiable {
		t.Fatal("a bare stop that skipped an instance reported complete success")
	}
}

// TestStopOfANamedForeignPortIsACompleteAnswer pins the other half of the exit
// code: naming a port puts the caller in charge of that instance. The occupant
// is reported and left alone, which is the whole answer to the question that was
// asked, so the command is not "incomplete".
func TestStopOfANamedForeignPortIsACompleteAnswer(t *testing.T) {
	f := newFixture(t)
	other := reserveFreePort(t)

	f.host.servingOnPort(other, 7001, "/usr/sbin/nginx -g daemon off;")
	explicitPort(t, f, other)

	stopped, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	wantNoSignalOn(t, f, 7001)
	if len(stopped.Results) != 1 {
		t.Fatalf("stopped %d instances, want only the named port", len(stopped.Results))
	}
	if !stopped.Results[0].Unverifiable {
		t.Fatal("the port's occupant was not reported as unverifiable")
	}
	if stopped.Unverifiable {
		t.Fatal("a stop of one named port reported itself incomplete")
	}
}

// TestRestartWithoutAPortRestartsEveryInstance pins that a bare restart covers
// every managed instance: each server that was running comes back on its own
// port, and one process per instance is started.
func TestRestartWithoutAPortRestartsEveryInstance(t *testing.T) {
	f := newFixture(t)
	// The first instance is started for real, so its record, its listener and
	// its address are the ones a start writes rather than ones a test drew.
	sameInstallation(t, f)
	first := f.Settings.Port
	f.host.spontaneouslyServed = true

	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("start on %d: %v", first, err)
	}
	spawnedFirst := f.host.spawnCalls()[0]

	// The second instance lives on another port of the same installation.
	second := reserveFreePort(t)
	configuredPort(t, f, second)
	serveOnConfiguredPort(t, f, 4322)

	f.host.spontaneouslyServed = true
	results, err := f.RestartAll(context.Background())
	if err != nil {
		t.Fatalf("RestartAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("restarted %d instances, want 2: %+v", len(results), results)
	}
	// The configured instance leads the sequence whatever its number: a bare
	// command reads the instance it is about off the front of the selection, so
	// the order is part of the contract rather than an artefact of the numbers.
	restarted := []int{results[0].Status.Port, results[1].Status.Port}
	if !equalInts(restarted, []int{second, first}) {
		t.Fatalf("restarted ports = %v, want %v (the configured instance first)",
			restarted, []int{second, first})
	}
	wantRecordForPort(t, f, first, true)
	wantRecordForPort(t, f, second, true)

	// Each instance came back on its own port: the launch arguments decide which
	// server a child becomes, and starting both on the same port would leave one
	// of the two instances down.
	ports := make([]int, 0, 2)
	for _, call := range f.host.spawnCalls() {
		ports = append(ports, portFromArgs(call.args))
	}
	if got := len(ports); got != 3 {
		t.Fatalf("spawn calls = %d (%v), want three: the first start and one per restarted instance", got, ports)
	}
	if want := []int{portFromArgs(spawnedFirst.args), second, first}; !equalInts(ports, want) {
		t.Fatalf("spawned ports = %v, want %v", ports, want)
	}
}

// equalInts reports whether two port lists agree, so a failure names the whole
// sequence instead of one index.
func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestRestartRefusesBeforeStoppingAnything pins the half-operation guard on the
// multi-port path: a port this dshctl cannot claim blocks the whole restart
// before any instance has been ended.
func TestRestartRefusesBeforeStoppingAnything(t *testing.T) {
	f := newFixture(t)
	sameInstallation(t, f)
	f.host.spontaneouslyServed = true
	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	other := reserveFreePort(t)
	configuredPort(t, f, other)
	// A DeepSeek Harness server of a *different* checkout on a port this state
	// directory holds a record for: the one shape the classification can
	// positively call somebody else's, and therefore the one that has to block
	// the restart before any instance is ended.
	f.host.servingOnPort(other, 7001, "/other/apps/cli/src/bin.ts web --port "+strconv.Itoa(other))
	saveRecordFor(t, f, state.Record{
		PID: 4322, StartedAt: fixtureStartTime, Port: other, Phase: state.PhaseRunning,
	})

	_, err := f.RestartAll(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, strconv.Itoa(other))
	// Nothing was ended and nothing was launched: the refusal is the whole
	// answer, and every instance is exactly where it was.
	f.wantNoSignals(t)
	if got := len(f.host.spawnCalls()); got != 1 {
		t.Fatalf("spawn calls = %d, want only the original start", got)
	}
}

// TestWebURLsReportsEveryRunningAddress pins that the addresses of every
// running instance are reachable without naming a port, so an operator can open
// any of them.
func TestWebURLsReportsEveryRunningAddress(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "http://127.0.0.1:"+strconv.Itoa(configured)+"/?token=CONFIGURED")
	seedRunningPort(t, f, other, 4322, "http://127.0.0.1:"+strconv.Itoa(other)+"/?token=OTHER")

	addresses, err := f.WebURLs(context.Background())
	if err != nil {
		t.Fatalf("WebURLs: %v", err)
	}
	if len(addresses) != 2 {
		t.Fatalf("addresses = %v, want two", addresses)
	}
	if !strings.Contains(addresses[configured], "token=CONFIGURED") {
		t.Fatalf("port %d address = %q", configured, addresses[configured])
	}
	if !strings.Contains(addresses[other], "token=OTHER") {
		t.Fatalf("port %d address = %q", other, addresses[other])
	}
}

// TestWebURLsNamesAnExplicitlySelectedInstance pins that --port narrows the
// address report to that instance.
func TestWebURLsNamesAnExplicitlySelectedInstance(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "http://127.0.0.1:"+strconv.Itoa(configured)+"/?token=CONFIGURED")
	seedRunningPort(t, f, other, 4322, "http://127.0.0.1:"+strconv.Itoa(other)+"/?token=OTHER")

	explicitPort(t, f, other)
	addresses, err := f.WebURLs(context.Background())
	if err != nil {
		t.Fatalf("WebURLs: %v", err)
	}
	if len(addresses) != 1 || addresses[other] == "" {
		t.Fatalf("addresses = %v, want only port %d", addresses, other)
	}
}

// TestWebURLsKeepsAWorkingInstanceOutOfTheFallback pins that discovery never
// invents an address: a port with a record but no live server contributes
// nothing to the list.
func TestWebURLsKeepsAWorkingInstanceOutOfTheFallback(t *testing.T) {
	f := newFixture(t)
	configured := f.Settings.Port
	other := reserveFreePort(t)

	f.startServer(t, 4321, "http://127.0.0.1:"+strconv.Itoa(configured)+"/?token=CONFIGURED")
	saveRecordFor(t, f, state.Record{
		PID: 9001, StartedAt: fixtureStartTime, Port: other,
		URL: "http://127.0.0.1:" + strconv.Itoa(other) + "/?token=DEAD", Phase: state.PhaseRunning,
	})

	addresses, err := f.WebURLs(context.Background())
	if err != nil {
		t.Fatalf("WebURLs: %v", err)
	}
	if len(addresses) != 1 || addresses[other] != "" {
		t.Fatalf("addresses = %v, want only port %d", addresses, configured)
	}
}

// TestPortDiscoveryFailsClosedWhenAProbeFails pins the invariant that an
// unanswerable probe is never read as a fact: when the host cannot look at a
// discovered port, the report fails instead of quietly describing the others.
func TestPortDiscoveryFailsClosedWhenAProbeFails(t *testing.T) {
	f := newFixture(t)
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	saveRecordFor(t, f, state.Record{
		PID: 9999, StartedAt: fixtureStartTime, Port: other, Phase: state.PhaseRunning,
	})
	f.host.listenErr = host.ErrUnsupported

	_, err := f.Statuses(context.Background())
	if err == nil {
		t.Fatal("a failed port probe was reported as a state")
	}
	wantCode(t, err, exitcode.Preflight)
}

// TestStopAllIsIdempotentAcrossPorts pins that a second bare stop finds nothing
// to do and says so, instead of failing because the first one removed every
// record.
func TestStopAllIsIdempotentAcrossPorts(t *testing.T) {
	f := newFixture(t)
	other := reserveFreePort(t)

	f.startServer(t, 4321, "")
	seedRunningPort(t, f, other, 4322, "")

	first, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("first StopAll: %v", err)
	}
	if len(first.Results) != 2 {
		t.Fatalf("first stop covered %d instances, want 2", len(first.Results))
	}
	second, err := f.StopAll(context.Background())
	if err != nil {
		t.Fatalf("second StopAll: %v", err)
	}
	if len(second.Results) != 1 {
		t.Fatalf("second stop covered %d instances, want only the configured port: %+v",
			len(second.Results), second.Results)
	}
	if second.Results[0].Status.State != StateStopped {
		t.Fatalf("second stop state = %q, want stopped", second.Results[0].Status.State)
	}
}
