package app

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

// deploymentHistory reads the fixture's recorded positions for its checkout.
func (f *fixture) deploymentHistory(t *testing.T) []history.Record {
	t.Helper()
	store := history.Store{Path: filepath.Join(f.Settings.StateDir, historyFileName)}
	file, ok, err := store.Load()
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if !ok {
		return nil
	}
	return file.Records(f.Settings.RepoDir)
}

// TestUpdateRefusesALocalBranchWithoutStopping pins that the branch trap is a
// preflight: `update <branch>` never deploys wherever the local pointer happens
// to be, and the service keeps serving.
func TestUpdateRefusesALocalBranchWithoutStopping(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")

	err := f.RunUpdate(context.Background(), "main")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "本地分支")
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
}

// TestUpdateHEADIsANoOp pins the other end of the selector rule: HEAD names the
// commit the checkout is already at, so the update reports it and changes
// nothing.
func TestUpdateHEADIsANoOp(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")

	if err := f.RunUpdate(context.Background(), "HEAD"); err != nil {
		t.Fatalf("RunUpdate(HEAD): %v", err)
	}
	if !strings.Contains(f.out.String(), "无需更新") {
		t.Fatalf("stdout = %q, want the no-op report", f.out.String())
	}
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
}

// TestUpdateRefusesWhenTheWorktreeCannotBeChecked pins the fail-closed rule for
// the switch: a status query that could not answer is not a clean worktree, so
// the update refuses before anything is stopped.
func TestUpdateRefusesWhenTheWorktreeCannotBeChecked(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "status", "--porcelain") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
	for _, forbidden := range []string{"merge", "install", "run build"} {
		if strings.Contains(f.describeCommands(), forbidden) {
			t.Fatalf("an unanswerable worktree check reached %s: %v", forbidden, f.describeCommands())
		}
	}
}

// TestUpdatePrependsAManualPosition pins the starting point rule: a checkout
// moved by hand is recorded before the move, so a later rollback can return to
// it even though dshctl never deployed it.
func TestUpdatePrependsAManualPosition(t *testing.T) {
	f := newFixture(t)
	manual := fakeSHA(700)
	deployed := fakeSHA(600)
	f.host.gitHead = manual
	f.seedDeployments(t, history.Record{Commit: deployed, Selector: "latest", At: 1})

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	records := f.deploymentHistory(t)
	if len(records) != 3 {
		t.Fatalf("history = %+v, want the target, the manual position and the deployed one", records)
	}
	if records[0].Commit != f.host.gitRemote || records[0].Selector != "latest" {
		t.Fatalf("newest = %+v, want the target", records[0])
	}
	if records[1].Commit != manual || records[1].Selector != "" {
		t.Fatalf("starting point = %+v, want the manual position", records[1])
	}
	if records[2].Commit != deployed {
		t.Fatalf("oldest = %+v, want the deployed position", records[2])
	}
}

// TestUpdateLatestRefusesWithoutOrigin pins the preflight that turns "this
// checkout has no origin" into a clear answer instead of git's fetch error:
// latest has no meaning without a remote.
func TestUpdateLatestRefusesWithoutOrigin(t *testing.T) {
	f := newFixture(t)
	f.host.gitOrigin = ""

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "origin")
	f.wantNoCheckoutUpdate(t)
}

// TestUpdateANamedVersionWorksWithoutOrigin pins the other side: a tag that
// exists locally is deployable on a checkout that has no remote at all. The
// failed fetch is a warning, not a refusal.
func TestUpdateANamedVersionWorksWithoutOrigin(t *testing.T) {
	f := newFixture(t)
	tagged := fakeSHA(500)
	f.host.gitTags = map[string]string{"dsh-v0.1.0": tagged}
	f.host.gitOrigin = ""

	if err := f.RunUpdate(context.Background(), "dsh-v0.1.0"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if f.host.gitHead != tagged {
		t.Fatalf("head = %q, want the tag %q", f.host.gitHead, tagged)
	}
	if !strings.Contains(f.errOut.String(), "无法获取远程更新") {
		t.Fatalf("stderr = %q, want the fetch warning", f.errOut.String())
	}
}

// TestUpdateNamesAShaTargetOnce pins the target line: a selector that is only
// an abbreviation of the commit adds nothing, so it is not repeated in
// parentheses, while a tag or latest is named.
func TestUpdateNamesAShaTargetOnce(t *testing.T) {
	f := newFixture(t)
	target := fakeSHA(500)
	f.host.gitLog = []fakeGitCommit{fakeCommit(target, "the target")}
	current := f.host.gitHead

	if err := f.RunUpdate(context.Background(), target[:8]); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	want := "更新: " + current[:7] + " → " + target[:7] + "\n"
	if !strings.Contains(f.out.String(), want) {
		t.Fatalf("stdout = %q, want %q", f.out.String(), want)
	}

	f2 := newFixture(t)
	f2.host.gitTags = map[string]string{"dsh-v0.1.0": target}
	current2 := f2.host.gitHead
	if err := f2.RunUpdate(context.Background(), "dsh-v0.1.0"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	wantTagged := "更新: " + current2[:7] + " → " + target[:7] + "（dsh-v0.1.0）\n"
	if !strings.Contains(f2.out.String(), wantTagged) {
		t.Fatalf("stdout = %q, want %q", f2.out.String(), wantTagged)
	}
}

// TestUpdateShortCircuitsWhenAlreadyAtTheTarget pins that a no-op update does
// not stop, rebuild or restart anything: the version is the one the operator
// asked for, so the only honest answer is "already there".
func TestUpdateShortCircuitsWhenAlreadyAtTheTarget(t *testing.T) {
	f := newFixture(t)
	f.host.gitHead = f.host.gitRemote
	f.startServer(t, 4321, "")

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if !strings.Contains(f.out.String(), "无需更新") {
		t.Fatalf("stdout = %q, want the no-op report", f.out.String())
	}
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
	for _, forbidden := range []string{"merge", "install", "run build"} {
		if strings.Contains(f.describeCommands(), forbidden) {
			t.Fatalf("a no-op update ran %s: %v", forbidden, f.describeCommands())
		}
	}
	if records := f.deploymentHistory(t); len(records) != 0 {
		t.Fatalf("history = %+v, want a no-op to record nothing", records)
	}
}

// TestUpdateSwitchesToATagWithoutMovingMaster pins the detached switch: the
// checkout lands on the tagged commit, the branch stays where it was, and the
// move is recorded with the selector the operator typed.
func TestUpdateSwitchesToATagWithoutMovingMaster(t *testing.T) {
	f := newFixture(t)
	head := f.host.gitHead
	tagged := fakeSHA(500)
	f.host.gitTags = map[string]string{"dsh-v0.1.0": tagged}
	master := f.host.gitMaster

	if err := f.RunUpdate(context.Background(), "dsh-v0.1.0"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if f.host.gitHead != tagged {
		t.Fatalf("head = %q, want the tagged commit %q", f.host.gitHead, tagged)
	}
	if f.host.gitMaster != master {
		t.Fatalf("master = %q, want it untouched at %q", f.host.gitMaster, master)
	}
	if f.host.gitBranch != "" {
		t.Fatalf("branch = %q, want a detached head", f.host.gitBranch)
	}
	records := f.deploymentHistory(t)
	if len(records) != 2 {
		t.Fatalf("history = %+v, want the starting point and the tag", records)
	}
	if records[0].Commit != tagged || records[0].Selector != "dsh-v0.1.0" {
		t.Fatalf("newest record = %+v, want the tag", records[0])
	}
	if records[1].Commit != head || records[1].Selector != "" {
		t.Fatalf("starting point = %+v, want the old head with no selector", records[1])
	}
}

// TestUpdateRefusesADirtyWorktreeWithoutStopping pins the boundary the operator
// keeps hitting: a modified tracked file blocks the switch while the service
// keeps serving, and the message says what to look at.
func TestUpdateRefusesADirtyWorktreeWithoutStopping(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.gitStatus = " M pnpm-lock.yaml\n"

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "未提交修改")
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
	for _, forbidden := range []string{"merge", "install", "run build"} {
		if strings.Contains(f.describeCommands(), forbidden) {
			t.Fatalf("a dirty worktree reached %s: %v", forbidden, f.describeCommands())
		}
	}
}

// TestUpdateRefusesAnUnknownVersionWithoutStopping pins that a typo never takes
// the service down: the target is resolved before anything is stopped.
func TestUpdateRefusesAnUnknownVersionWithoutStopping(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")

	err := f.RunUpdate(context.Background(), "no-such-version")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "no-such-version")
	f.wantNoSignals(t)
	f.wantNoSpawn(t)
}

// TestUpdateResolvesANamedVersionWithoutTheRemote pins the offline path: a tag
// that exists locally is enough, and the failed fetch is a warning rather than
// a refusal.
func TestUpdateResolvesANamedVersionWithoutTheRemote(t *testing.T) {
	f := newFixture(t)
	tagged := fakeSHA(500)
	f.host.gitTags = map[string]string{"dsh-v0.1.0": tagged}
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "fetch") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	if err := f.RunUpdate(context.Background(), "dsh-v0.1.0"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if f.host.gitHead != tagged {
		t.Fatalf("head = %q, want the tag despite the failed fetch", f.host.gitHead)
	}
	if !strings.Contains(f.errOut.String(), "无法获取远程更新") {
		t.Fatalf("stderr = %q, want the fetch warning", f.errOut.String())
	}
}

// TestUpdateWarnsAboutATargetOutsideOrigin pins the advisory rule: a commit
// origin/master cannot reach is deployable, but the operator is told.
func TestUpdateWarnsAboutATargetOutsideOrigin(t *testing.T) {
	f := newFixture(t)
	f.host.gitTags = map[string]string{"dsh-v0.1.0-side": fakeSHA(500)}
	f.host.gitAncestor = false

	if err := f.RunUpdate(context.Background(), "dsh-v0.1.0-side"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if !strings.Contains(f.errOut.String(), "不在 origin/master 的历史上") {
		t.Fatalf("stderr = %q, want the outside-history warning", f.errOut.String())
	}
}

// TestUpdateKeepsDeployingWhenTheHistoryCannotBeWritten pins the failure order:
// the deployment is already a fact on disk when the record is written, so a
// failed record must not abort the build and restart — it is reported at the
// end, after the service is back.
func TestUpdateKeepsDeployingWhenTheHistoryCannotBeWritten(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.spontaneouslyServed = true
	// A directory where the history file belongs: the record cannot be
	// replaced, and the move itself must still complete.
	if err := os.MkdirAll(filepath.Join(f.Settings.StateDir, historyFileName), 0o700); err != nil {
		t.Fatalf("mkdir history path: %v", err)
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "更新历史未写入")
	if !strings.Contains(f.describeCommands(), "install") {
		t.Fatalf("the deployment stopped at the record: %v", f.describeCommands())
	}
	if !strings.Contains(f.out.String(), "更新完成") {
		t.Fatalf("stdout = %q, want the deployment to have finished", f.out.String())
	}
	if record, ok := f.stateRecord(t); !ok || record.PID == 4321 {
		t.Fatalf("record = %+v (ok=%v), want the restored server", record, ok)
	}
}

// TestUpdateRecordsTheMoveBeforeTheBuild pins the write order that makes
// rollback possible after a failed build: the position is on disk before
// pnpm runs.
func TestUpdateRecordsTheMoveBeforeTheBuild(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "run", "build") {
			return &run.ExitError{Command: cmd.String(), Code: 1}
		}
		return nil
	}

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Failure)
	records := f.deploymentHistory(t)
	if len(records) != 2 || records[0].Commit != f.host.gitRemote {
		t.Fatalf("history = %+v, want the new position recorded before the build failed", records)
	}
}
