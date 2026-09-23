package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/logfile"
	"github.com/rhczz/dshctl/internal/logging"
	"github.com/rhczz/dshctl/internal/run"
)

// TestBuildReportsAPruneFailure pins that a prune which cannot even ask git what
// is tracked stops the build. The alternative — building over residue — is
// exactly the state the prune exists to prevent, and an operator who only saw
// "build failed" would have no idea which step failed.
func TestBuildReportsAPruneFailure(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "git" && hasArgument(cmd, "ls-files") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Failure)
	if !strings.Contains(err.Error(), "ls-files") {
		t.Fatalf("error = %v, want it to name the failed prune command", err)
	}
	if strings.Contains(f.describeCommands(), "run build") {
		t.Fatal("the build ran after the prune failed")
	}
}

// TestBuildReportsThePruneResult pins the message an operator needs to
// understand where the deleted directories went: the count is printed and
// recorded, so `dshctl logs` still explains the build afterwards.
func TestBuildReportsThePruneResult(t *testing.T) {
	f := newFixture(t)
	// A directory under a package that no longer exists in the workspace: the
	// shape an upstream package removal leaves behind.
	stale := filepath.Join(f.repo, "packages", "grp", "stale", "node_modules", "dep", "x.js")
	writeFile(t, stale, "left behind")
	// git tracks nothing in it, which is what makes it residue.
	f.host.mu.Lock()
	f.host.trackedFiles = ""
	f.host.mu.Unlock()

	if err := f.RunBuild(context.Background()); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	if !strings.Contains(f.out.String(), "removed 1 residue directories") {
		t.Fatalf("stdout = %q, want the prune count", f.out.String())
	}
	data, err := os.ReadFile(f.Settings.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "removed 1 residue directories") {
		t.Fatalf("log = %q, want the prune count recorded", data)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "packages", "grp", "stale")); !os.IsNotExist(err) {
		t.Fatalf("the stale directory survived the prune (err=%v)", err)
	}
}

// TestBuildReportsItsOwnFailure pins the message of a failing build. "pnpm
// exited 1" is not actionable on its own: the operator needs to know this was
// the build step and where the full output was captured.
func TestBuildReportsItsOwnFailure(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "pnpm" && hasArgument(cmd, "run", "build") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "the build failed")
	wantContains(t, err, f.Settings.LogPath)
	if !strings.Contains(f.out.String(), "building the checkout") {
		t.Fatalf("stdout = %q, want the build attempt reported", f.out.String())
	}
	// The attribution is on the returned error, which is what the CLI prints.
	if !strings.Contains(err.Error(), "see the log") {
		t.Fatalf("error = %v, want it to point at the captured output", err)
	}
}

// TestUpdateRefusesAMissingRepository pins the first precondition the update
// checks, before anything is touched.
func TestUpdateRefusesAMissingRepository(t *testing.T) {
	f := newFixture(t)
	f.Settings.RepoDir = filepath.Join(f.root, "no-such-repo")
	f.Repo.Dir = f.Settings.RepoDir

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "the checkout does not exist")
	f.wantNoCheckoutUpdate(t)
	f.wantNoSpawn(t)
}

// TestUpdateRefusesADirectoryThatIsNotAGitRepository pins the second
// precondition. A directory can hold the harness manifests and still not be a
// checkout — a copied tree, an exported tarball — and `git pull` there would
// either fail or, worse, reach a repository somewhere above it.
func TestUpdateRefusesADirectoryThatIsNotAGitRepository(t *testing.T) {
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Join(f.repo, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "is not a git repository")
	f.wantNoCheckoutUpdate(t)
	f.wantNoSpawn(t)
}

// TestUpdateDoesNotStopWhenTheFetchFails pins the failure that happens before
// anything is touched: latest cannot be resolved without the remote, so the
// update fails while the service keeps serving — no stop, no restore, no spawn.
func TestUpdateDoesNotStopWhenTheFetchFails(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "git" && hasArgument(cmd, "fetch") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "the remote could not be fetched")
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
	if strings.Contains(f.out.String(), "the old build is intact; starting the old version again") {
		t.Fatalf("stdout = %q, want no restore attempt without a running server", f.out.String())
	}
}

// TestUpdateReportsWhenTheRestoreStartFails pins the worst case of a failed
// switch: the server was stopped for the update, the switch failed, and
// starting the old build again failed too. The operator has to be told both
// that the checkout was not changed and that nothing is serving any more.
func TestUpdateReportsWhenTheRestoreStartFails(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true
	f.host.mu.Lock()
	f.host.spawnErr = errors.New("pnpm: no such file or directory")
	f.host.mu.Unlock()
	f.host.fail = func(cmd run.Command) error {
		if filepath.Base(cmd.Name) == "git" && hasArgument(cmd, "merge", "--ff-only") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "update failed")
	if !strings.Contains(f.errOut.String(), "restoring the service failed") {
		t.Fatalf("stderr = %q, want the failed restore reported", f.errOut.String())
	}
	if !strings.Contains(f.out.String(), "the old build is intact; starting the old version again") {
		t.Fatalf("stdout = %q, want the restore attempt reported", f.out.String())
	}
	// The old server was stopped for the update and could not be brought back.
	f.wantSignals(t, []fakeSignal{{4321, hostGraceful}})
	if _, ok := f.stateRecord(t); ok {
		t.Fatal("a failed restore left a record behind")
	}
}

// TestUpdateRefusesWhileAnotherPortServesTheCheckout pins the cross-port guard
// on the update path.
//
// A server on another port is not stopped by this update and not restarted by
// it either, so pulling and rebuilding under it would replace the artifacts it
// is serving and leave it broken with no warning. `build` refuses for the same
// reason; this is the update half of that rule, and the message has to name the
// port so the operator knows which server to stop.
func TestUpdateRefusesWhileAnotherPortServesTheCheckout(t *testing.T) {
	f := newFixture(t)
	otherPort := f.Settings.Port + 1
	f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
	other := recordStore(filepath.Join(f.state, fmt.Sprintf(config.StateFileNamePattern, otherPort)))
	if err := other.Save(domain.Record{
		PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
		Port: otherPort, Phase: domain.PhaseRunning,
	}); err != nil {
		t.Fatalf("save the other port's record: %v", err)
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), strconv.Itoa(otherPort)) {
		t.Fatalf("error = %v, want it to name the other port %d", err, otherPort)
	}
	f.wantNoCheckoutUpdate(t)
	f.wantNoSpawn(t)
	f.wantNoSignals(t)
}

// TestBuildAndUpdateSeeASiblingRecordInAnAwkwardStateDirectory pins the
// cross-port guard against a state directory whose own name looks like a
// pattern.
//
// The guard finds the other ports by globbing the state directory, so a "[" in
// the directory name used to be read as the start of a character class: the
// pattern "<state>[1]/dsh-web-*.state.json" matched nothing, the guard concluded
// that no other server exists, and the build replaced the artifacts of a server
// that was still serving them. The directory is a literal in the pattern now
// (config.StateFileGlob quotes it), so the sibling record is found and both
// commands refuse before touching the checkout.
func TestBuildAndUpdateSeeASiblingRecordInAnAwkwardStateDirectory(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*fixture) error
	}{
		{"build", func(f *fixture) error { return f.RunBuild(context.Background()) }},
		{"update", func(f *fixture) error { return f.RunUpdate(context.Background(), "latest") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newFixture(t)
			// A state directory that is a legal file name and a broken pattern.
			f.Settings.StateDir = filepath.Join(f.root, "state[1]")
			f.Settings.ConfigPath = filepath.Join(f.Settings.StateDir, config.ConfigFileName)
			f.Settings.LogPath = filepath.Join(f.Settings.StateDir, config.DefaultLogFileName)
			f.Record = recordStore(f.Settings.StateFile())
			f.LogFile = logfile.New(f.Settings.LogPath, f.Settings.LogRotateBytes, logFormat)
			f.Log = logging.New(f.LogFile, logging.LevelInfo)
			f.Emit = TextEmitter{Out: f.out, Err: f.errOut}

			otherPort := f.Settings.Port + 1
			other := recordStore(filepath.Join(f.Settings.StateDir, fmt.Sprintf(config.StateFileNamePattern, otherPort)))
			f.host.add(4242, "pnpm --dir repo dsh web", fixtureStartTime)
			if err := other.Save(domain.Record{
				PID: 4242, SpawnedPID: 4242, StartedAt: fixtureStartTime,
				Port: otherPort, Phase: domain.PhaseRunning,
			}); err != nil {
				t.Fatalf("save the other port's record: %v", err)
			}

			err := testCase.run(f)
			wantCode(t, err, exitcode.Preflight)
			if !strings.Contains(err.Error(), strconv.Itoa(otherPort)) {
				t.Fatalf("error = %v, want it to name the other port %d: the guard did not see the sibling record", err, otherPort)
			}
			if strings.Contains(f.describeCommands(), "run build") || strings.Contains(f.describeCommands(), "fetch") {
				t.Fatalf("the checkout was rewritten despite a live server on another port: %v", f.describeCommands())
			}
		})
	}
}

// TestBuildRefusesWhenAnotherPortsRecordCannotBeRead pins the fail-closed
// reading of the cross-port guard: a record nobody can parse is not a port
// nobody serves. Replacing artifacts under a server this build cannot see is
// exactly the failure the guard exists to prevent.
func TestBuildRefusesWhenAnotherPortsRecordCannotBeRead(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	other := 4321
	path := filepath.Join(f.Settings.StateDir, fmt.Sprintf(config.StateFileNamePattern, other))
	if err := os.MkdirAll(f.Settings.StateDir, 0o700); err != nil {
		t.Fatalf("state dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not a record"), 0o600); err != nil {
		t.Fatalf("seed the corrupt record: %v", err)
	}

	err := f.RunBuild(context.Background())
	if got := exitcode.Of(err); got != exitcode.Preflight {
		t.Fatalf("build exit = %d (err = %v), want %d", got, err, exitcode.Preflight)
	}
	f.wantNoSpawn(t)
}

// TestBuildRefusesWhenAnotherPortsProbeFails pins the other half: a record that
// cannot be confirmed as "not serving" must not be read as "not serving" either.
func TestBuildRefusesWhenAnotherPortsProbeFails(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true
	other := 4321
	store := recordStore(filepath.Join(f.Settings.StateDir, fmt.Sprintf(config.StateFileNamePattern, other)))
	if err := f.Settings.Provision(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := store.Save(domain.Record{
		PID: 9001, SpawnedPID: 9000, StartedAt: fixtureStartTime, Port: other, Phase: domain.PhaseRunning,
	}); err != nil {
		t.Fatalf("seed the record: %v", err)
	}
	f.host.listenErr = errors.New("lsof timed out")

	err := f.RunBuild(context.Background())
	if got := exitcode.Of(err); got != exitcode.Preflight {
		t.Fatalf("build exit = %d (err = %v), want %d when a probe fails", got, err, exitcode.Preflight)
	}
	f.wantNoSpawn(t)
}
