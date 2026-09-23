package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/run"
)

// seedDeployments writes the positions the fixture's checkout has been at.
func (f *fixture) seedDeployments(t *testing.T, records ...history.Record) {
	t.Helper()
	f.seedHistory(t, history.File{Repos: []history.Group{{Repo: f.repo, Records: records}}})
}

// TestRollbackReturnsToThePreviousPosition pins the one-step undo: the update
// is reversed, the stack is truncated at the position returned to, and the
// move is recorded with the step that caused it.
func TestRollbackReturnsToThePreviousPosition(t *testing.T) {
	f := newFixture(t)
	head := f.host.gitHead
	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if f.host.gitHead != f.host.gitRemote {
		t.Fatalf("head = %q, want the update to have moved it", f.host.gitHead)
	}

	if err := f.RunRollback(context.Background(), "", 1); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if f.host.gitHead != head {
		t.Fatalf("head = %q, want the position before the update %q", f.host.gitHead, head)
	}
	records := f.deploymentHistory(t)
	if len(records) != 1 || records[0].Commit != head || records[0].Selector != "-n 1" {
		t.Fatalf("history = %+v, want only the returned-to position", records)
	}
	if !strings.Contains(f.out.String(), "roll back finished") {
		t.Fatalf("stdout = %q, want the rollback reported", f.out.String())
	}
	data, err := os.ReadFile(f.Settings.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "dshctl rollback") {
		t.Fatalf("log = %q, want the rollback section", data)
	}
}

// TestRollbackStepsWalkFurtherBack pins the step arithmetic end to end: two
// steps reach the position before the last two moves.
func TestRollbackStepsWalkFurtherBack(t *testing.T) {
	f := newFixture(t)
	third := f.host.gitHead
	second := fakeSHA(600)
	first := fakeSHA(500)
	f.host.gitLog = []fakeGitCommit{
		fakeCommit(third, "third"),
		fakeCommit(second, "second"),
		fakeCommit(first, "first"),
	}
	f.seedDeployments(t,
		history.Record{Commit: third, Selector: "latest", At: 3},
		history.Record{Commit: second, Selector: "latest", At: 2},
		history.Record{Commit: first, Selector: "latest", At: 1},
	)

	if err := f.RunRollback(context.Background(), "", 2); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if f.host.gitHead != first {
		t.Fatalf("head = %q, want the first position %q", f.host.gitHead, first)
	}
	records := f.deploymentHistory(t)
	if len(records) != 1 || records[0].Commit != first || records[0].Selector != "-n 2" {
		t.Fatalf("history = %+v, want the stack truncated at the first position", records)
	}
}

// TestRollbackRefusesWithoutHistory pins the preflight that keeps a rollback
// from guessing.
func TestRollbackRefusesWithoutHistory(t *testing.T) {
	f := newFixture(t)
	err := f.RunRollback(context.Background(), "", 1)
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "nothing to roll back to: dshctl has not recorded a position for this checkout yet")
	f.wantNoCheckoutUpdate(t)
}

// TestRollbackRefusesACorruptHistory pins that a history nobody can read stops
// the rollback instead of being treated as an empty one.
func TestRollbackRefusesACorruptHistory(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.Settings.StateDir, historyFileName), "{not json")

	err := f.RunRollback(context.Background(), "", 1)
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "the deployment history cannot be read: ")
	f.wantNoCheckoutUpdate(t)
}

// TestRollbackRefusesBeyondTheHistory pins the bound: the operator is told how
// many steps are actually available.
func TestRollbackRefusesBeyondTheHistory(t *testing.T) {
	f := newFixture(t)
	f.seedDeployments(t,
		history.Record{Commit: f.host.gitHead, Selector: "latest", At: 2},
		history.Record{Commit: fakeSHA(500), Selector: "latest", At: 1},
	)

	err := f.RunRollback(context.Background(), "", 5)
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "the history has at most 1 steps left")
	f.wantNoCheckoutUpdate(t)
}

// TestRollbackToANamedVersionDoesNotFetch pins the firefighting path: a named
// version resolves locally, without a network, and the move is recorded as a
// new position.
func TestRollbackToANamedVersionDoesNotFetch(t *testing.T) {
	f := newFixture(t)
	tagged := fakeSHA(500)
	f.host.gitTags = map[string]string{"dsh-v0.1.0": tagged}
	f.seedDeployments(t, history.Record{Commit: f.host.gitHead, Selector: "latest", At: 1})
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "fetch") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	if err := f.RunRollback(context.Background(), "dsh-v0.1.0", 0); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if f.host.gitHead != tagged {
		t.Fatalf("head = %q, want the tag %q", f.host.gitHead, tagged)
	}
	if strings.Contains(f.describeCommands(), "fetch") {
		t.Fatalf("a rollback fetched: %v", f.describeCommands())
	}
	records := f.deploymentHistory(t)
	if len(records) != 2 || records[0].Commit != tagged || records[0].Selector != "dsh-v0.1.0" {
		t.Fatalf("history = %+v, want the tag on top of the old position", records)
	}
}

// TestRollbackStopsAndRestoresTheService pins the lifecycle half: a rollback of
// a running instance stops it and starts the rolled-back build again.
func TestRollbackStopsAndRestoresTheService(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true
	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	afterUpdate, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the update left no record")
	}

	if err := f.RunRollback(context.Background(), "", 1); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if f.host.isAlive(4321) {
		t.Fatal("the pre-update server was not stopped")
	}
	if f.host.isAlive(afterUpdate.PID) {
		t.Fatal("the update's server was not stopped by the rollback")
	}
	record, ok := f.stateRecord(t)
	if !ok || record.PID == afterUpdate.PID {
		t.Fatalf("record = %+v (ok=%v), want a freshly started server", record, ok)
	}
	if !strings.Contains(f.out.String(), "starting DSH Web again") {
		t.Fatalf("stdout = %q, want the restart reported", f.out.String())
	}
}

// TestUpdateAfterARollbackReturnsToMaster pins the full cycle the contract
// promises: a rollback leaves a detached head, and a plain update puts the
// checkout back on master at origin/master.
func TestUpdateAfterARollbackReturnsToMaster(t *testing.T) {
	f := newFixture(t)
	head := f.host.gitHead
	remote := f.host.gitRemote

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if err := f.RunRollback(context.Background(), "", 1); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if f.host.gitHead != head || f.host.gitBranch != "" {
		t.Fatalf("after the rollback: head=%q branch=%q, want a detached %q", f.host.gitHead, f.host.gitBranch, head)
	}

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate after the rollback: %v", err)
	}
	if f.host.gitHead != remote {
		t.Fatalf("head = %q, want the remote tip %q", f.host.gitHead, remote)
	}
	if f.host.gitBranch != "master" {
		t.Fatalf("branch = %q, want the checkout back on master", f.host.gitBranch)
	}
	records := f.deploymentHistory(t)
	if len(records) != 2 || records[0].Commit != remote || records[0].Selector != "latest" {
		t.Fatalf("history = %+v, want the remote tip on top of the rolled-back position", records)
	}
}

// TestRollbackNamesTheTagItReturnsTo pins the target line of a step rollback:
// the position is named by its tag when it has one, so the operator can see
// which release they are going back to.
func TestRollbackNamesTheTagItReturnsTo(t *testing.T) {
	f := newFixture(t)
	target := fakeSHA(500)
	head := f.host.gitHead
	f.host.gitLog = []fakeGitCommit{
		fakeCommit(head, "head"),
		fakeCommit(target, "target"),
	}
	f.host.gitTags = map[string]string{"dsh-v0.1.0": target}
	f.seedDeployments(t,
		history.Record{Commit: head, Selector: "latest", At: 2},
		history.Record{Commit: target, Selector: "latest", At: 1},
	)

	if err := f.RunRollback(context.Background(), "", 1); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	want := "roll back: " + head[:7] + " → " + target[:7] + " (dsh-v0.1.0)\n"
	if !strings.Contains(f.out.String(), want) {
		t.Fatalf("stdout = %q, want %q", f.out.String(), want)
	}
}

// TestRollbackRefusesAnUnknownRefWithoutFetching pins the firefighting rule for
// a typo: the rollback never reaches the network, and the failure says the
// version could not be resolved.
func TestRollbackRefusesAnUnknownRefWithoutFetching(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "fetch") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	err := f.RunRollback(context.Background(), "no-such-version", 0)
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "no-such-version")
	if strings.Contains(f.describeCommands(), "fetch") {
		t.Fatalf("a rollback fetched: %v", f.describeCommands())
	}
}

// TestRollbackShortCircuitsAtTheCurrentPosition pins that rolling back to the
// commit the checkout is already at changes nothing.
func TestRollbackShortCircuitsAtTheCurrentPosition(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.seedDeployments(t,
		history.Record{Commit: f.host.gitHead, Selector: "latest", At: 2},
		history.Record{Commit: fakeSHA(500), Selector: "latest", At: 1},
	)

	// -n 2 walks to the first position; -n 1 would move. Point the tag at the
	// current commit so the named path resolves to where we already are.
	f.host.gitTags = map[string]string{"here": f.host.gitHead}
	if err := f.RunRollback(context.Background(), "here", 0); err != nil {
		t.Fatalf("RunRollback: %v", err)
	}
	if !strings.Contains(f.out.String(), "nothing to roll back") {
		t.Fatalf("stdout = %q, want the no-op report", f.out.String())
	}
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
}
