//go:build unix

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/run"
)

// lookPath resolves an executable on the real PATH.
func lookPath(name string) (string, error) { return run.LookPath(name) }

// systemToolPath names the directories that hold the operating system's own
// tools — the ones the port and process probes reach for. Tests narrow PATH to
// exclude everything else, so a test never picks up a tool by accident, while
// the probes can still answer.
const systemToolPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// contains reports whether a string holds a substring.
func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestStartFailsFastWhenTheRealChildExitsImmediately is the regression test for
// a wait loop that never noticed the child was gone.
//
// The unit fixture's fictional host reports a dead child correctly, so it cannot
// catch this class of bug. This test therefore runs the REAL launcher and a REAL
// child process: a pnpm that exits at once. Before the child was reaped, that
// child stayed in the process table as a zombie, still answered "does this pid
// exist?", and the start waited out its entire timeout instead of failing.
func TestStartFailsFastWhenTheRealChildExitsImmediately(t *testing.T) {
	f := newFixture(t)
	shell, err := lookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}

	// A pnpm that exits immediately: the server never gets a chance to listen.
	binDir := filepath.Join(f.root, "bin")
	writeFile(t, filepath.Join(binDir, "pnpm"), "#!/bin/sh\necho 'pnpm: simulated failure' 1>&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(binDir, "pnpm"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// The child inherits the fixture's environment through the default launcher,
	// so the stub above is what it resolves. Node is deliberately absent from
	// this PATH, which is also what proves the failure is about the child.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(shell)+string(os.PathListSeparator)+systemToolPath)

	// Use the real launcher, the real PATH lookup and the real probes rather than
	// the fictional ones: the point of this test is what the operating system
	// actually reports about a real child. The fingerprint budget goes back to
	// the production value for the same reason — the fixture shortens it so the
	// tests of a fictional machine do not spend it, and a real process must be
	// given the time the product gives it.
	f.Spawn = nil
	f.LookPath = run.LookPath
	f.Host = host.NewWithLookPath(run.LookPath, hostTools)
	f.fingerprint = fingerprintTimeout
	f.Settings.StartTimeout = 30 * time.Second

	start := time.Now()
	_, err = f.Start(context.Background())
	elapsed := time.Since(start)

	wantCode(t, err, exitcode.Failure)
	if elapsed > 10*time.Second {
		t.Fatalf("start waited %s for a child that had already exited", elapsed)
	}
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed start left its runtime record behind")
	}
	if !contains(err.Error(), ") exited and port") {
		t.Fatalf("error = %v, want it to say the process exited", err)
	}
}

// TestStartReportsASuccessfulRealChild pins the happy path through the real
// launcher: a child that takes the port is detected and recorded.
func TestStartReportsASuccessfulRealChild(t *testing.T) {
	f := newFixture(t)
	nodePath := realNodeShim(t, filepath.Join(f.root, "node-bin"))

	server := filepath.Join(f.root, "server.js")
	writeFile(t, server, `
const net = require('net');
const port = Number(process.argv[process.argv.indexOf('--port') + 1]);
console.log('dsh web: http://127.0.0.1:' + port + '/?token=REAL');
net.createServer(() => {}).listen(port, '127.0.0.1', () => console.log('listening'));
setTimeout(() => process.exit(0), 30000);
`)
	binDir := filepath.Join(f.root, "bin")
	writeFile(t, filepath.Join(binDir, "pnpm"), "#!/bin/sh\nexec "+nodePath+" "+server+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(binDir, "pnpm"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Only the stub and the directories the operating-system tools live in. The
	// port probe needs those tools, and a test that hides them would be testing
	// the wrong thing.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+filepath.Dir(nodePath)+string(os.PathListSeparator)+systemToolPath)

	f.Spawn = nil
	f.LookPath = run.LookPath
	f.Settings.StartTimeout = 20 * time.Second
	f.Dial = nil // the real server really listens
	// Real probes and a real resolver: this test runs real processes, so every
	// substitute that only knows about fictional ones has to go — including the
	// shortened fingerprint budget the fixture uses for those substitutes.
	f.Host = host.NewWithLookPath(run.LookPath, hostTools)
	f.fingerprint = fingerprintTimeout
	f.Node = &nodejs.Resolver{LookPath: run.LookPath, Glob: filepath.Glob, Stat: os.Stat}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := f.Start(ctx)
	if err != nil {
		if data, readErr := os.ReadFile(f.Settings.LogPath); readErr == nil {
			t.Fatalf("Start: %v\n--- server log ---\n%s", err, data)
		}
		t.Fatalf("Start: %v", err)
	}
	if result.SpawnedPID == 0 {
		t.Fatal("no process was reported")
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
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("a successful start must write a runtime record")
	}
	if record.URL != f.Settings.URL()+"/?token=REAL" {
		t.Fatalf("recorded URL = %q, want the address the server announced", record.URL)
	}
	// Clean up the real child through the stop path.
	if _, err := f.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
