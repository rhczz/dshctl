//go:build unix

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/run"
)

// TestStartAcrossAWrapperThatSpawns is the regression test for the shape every
// real launch has.
//
// `pnpm <script>` runs package scripts in a *child* process rather than
// replacing itself, so the pid dshctl spawns is not the pid that binds the port.
// The earlier fixtures used `exec`, which made the two the same and hid the
// problem: against a real checkout every start failed with "the port is held by
// another process", complaining about the server dshctl had just started.
func TestStartAcrossAWrapperThatSpawns(t *testing.T) {
	requirePosix(t)
	f := newFixture(t)
	nodePath := realNodeShim(t, filepath.Join(f.root, "node-bin"))
	server := filepath.Join(f.root, "server.js")
	writeFile(t, server, `
const net = require('net');
const port = Number(process.argv[process.argv.indexOf('--port') + 1]);
console.log('dsh web: http://127.0.0.1:' + port + '/?token=WRAPPER');
net.createServer(() => {}).listen(port, '127.0.0.1', () => console.log('listening'));
setInterval(() => {}, 1000);
`)
	// A wrapper that spawns the server and waits, exactly as pnpm does.
	binDir := filepath.Join(f.root, "bin")
	writeFile(t, filepath.Join(binDir, "pnpm"), "#!/bin/sh\n"+
		nodePath+" "+server+" \"$@\" &\n"+
		"child=$!\n"+
		"echo spawned=$child\n"+
		"wait $child\n")
	if err := os.Chmod(filepath.Join(binDir, "pnpm"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(nodePath)+string(os.PathListSeparator)+systemToolPath)

	f.Spawn = nil
	f.LookPath = run.LookPath
	f.Host = host.NewWithLookPath(run.LookPath, hostTools)
	// The real host answers a real child's start time, so this test keeps the
	// production budget instead of the shortened one the fixture gives the
	// fictional machine.
	f.fingerprint = fingerprintTimeout
	f.Node = &nodejs.Resolver{LookPath: run.LookPath, Glob: filepath.Glob, Stat: os.Stat}
	f.Dial = nil // the server really listens
	f.Settings.StartTimeout = 20 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := f.Start(ctx)
	if err != nil {
		if data, readErr := os.ReadFile(f.Settings.LogPath); readErr == nil {
			t.Fatalf("Start: %v\n--- server log ---\n%s", err, data)
		}
		t.Fatalf("Start: %v", err)
	}

	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("a successful start must write a record")
	}
	// The server this test just started is a real detached process. Stopping it
	// through the service is registered here, immediately, so that a failure in
	// any assertion below cannot leave it serving on the machine forever; the
	// explicit stop at the end of the test is the assertion that the stop path
	// works. Stopping twice is safe: the second call finds nothing recorded.
	t.Cleanup(func() {
		if _, err := f.Stop(context.Background()); err != nil {
			t.Logf("cleanup stop: %v", err)
		}
	})
	// The record names the process that holds the port...
	if record.PID != result.Status.ListenerPID {
		t.Fatalf("record pid = %d, want the listener %d", record.PID, result.Status.ListenerPID)
	}
	// ...and the wrapper is recorded beside it, because the two are how the whole
	// group is ended.
	if record.SpawnedPID == 0 || record.SpawnedPID == record.PID {
		t.Fatalf("record = %+v, want the spawned wrapper recorded separately", record)
	}
	if !strings.Contains(record.URL, "token=WRAPPER") {
		t.Fatalf("record URL = %q, want the address the server announced", record.URL)
	}

	// The port really is served by a process other than the wrapper, which is the
	// whole point of this test.
	if record.PID == record.SpawnedPID {
		t.Fatal("the fixture did not produce a wrapper and a listener as separate processes")
	}

	// Stopping must end the server, not only the wrapper. The answer comes from
	// the real host that started them — the fictional table would report the
	// real pids as dead before they ever ran, which is how this check used to
	// pass while a server kept serving.
	if _, err := f.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// A detached process is reaped by init, and a zombie still answers an
	// existence probe, so a moment is allowed for the reaper; Inspect is the
	// probe that can tell a zombie from a live process where ps is available.
	for _, pid := range []int{record.PID, record.SpawnedPID} {
		deadline := time.Now().Add(5 * time.Second)
		for f.Host.Inspect(ctx, pid).Alive && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if f.Host.Inspect(ctx, pid).Alive {
			t.Fatalf("pid %d survived the stop", pid)
		}
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
}

// TestStartRefusesWhenALiveRecordIsNotListening pins that a record naming a live
// server dshctl started is never deleted just because the port is free.
//
// Deleting it throws away the only handle on that process: nothing would ever
// signal it again, and the next start would quietly run a second server.
func TestStartRefusesWhenALiveRecordIsNotListening(t *testing.T) {
	f := newFixture(t)
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(stateRecordWithWrapper(f, 4242)); err != nil {
		t.Fatalf("save record: %v", err)
	}

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !contains(err.Error(), "is still alive") {
		t.Fatalf("error = %v, want it to say the recorded server is still alive", err)
	}
	f.wantNoSpawn(t)
	if _, ok := f.stateRecord(t); !ok {
		t.Fatal("the record of a live server was deleted")
	}
}

// TestStopEndsARecordedProcessThatNoLongerListens pins that a record naming a
// live, verified server is honoured by stop even when nothing listens on the
// port: the hint start prints ("run dshctl stop to end it") must be true, or
// the operator is locked out of both commands.
func TestStopEndsARecordedProcessThatNoLongerListens(t *testing.T) {
	f := newFixture(t)
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(stateRecordWithWrapper(f, 4242)); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantSignals(t, []fakeSignal{{4242, host.Graceful}})
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a completed stop must clear the record")
	}
}

// TestStopRemovesARecordWhoseProcessIsGone pins the other half: a record that
// describes nothing is still cleared, so the next start is not blocked by it.
func TestStopRemovesARecordWhoseProcessIsGone(t *testing.T) {
	f := newFixture(t)
	if err := f.Record.Save(stateRecordWithWrapper(f, 999999)); err != nil {
		t.Fatalf("save record: %v", err)
	}

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a record naming a dead pid must be cleared")
	}
}

// stateRecordWithWrapper builds a record in the shape a real start writes: the
// listener is recorded together with the wrapper that spawned it.
func stateRecordWithWrapper(f *fixture, pid int) domain.Record {
	return domain.Record{
		PID:        pid,
		SpawnedPID: pid,
		StartedAt:  fixtureStartTime,
		Port:       f.Settings.Port,
		Phase:      domain.PhaseRunning,
	}
}

// requirePosix skips a test that needs a POSIX shell.
func requirePosix(t *testing.T) {
	t.Helper()
	if _, err := run.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
}
