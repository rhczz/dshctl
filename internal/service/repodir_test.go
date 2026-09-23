package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
)

// The checkout a command acts on is resolved from the configuration, while the
// service that is running was started from whichever checkout the start that
// launched it named. When the two disagree, every later command describes and
// touches a tree the running process never used — which is how a healthy service
// ended up looking broken: `dshctl --repo /real/checkout start` left the settings
// document naming the built-in guess, and `doctor`, `update` and a plain `start`
// all reported that a directory nobody had chosen did not exist.
//
// The rows below pin both halves of the repair: a run records the checkout it
// used when the document decides nothing, and every later invocation — resolved
// from scratch, the way a new process does — acts on that checkout.

// TestASuccessfulStartMakesTheNextCommandUseTheSameCheckout is the regression
// test for the reported sequence, driven inside the package: the document
// carries the stale guess, the operator names the real checkout with --repo, and
// the commands that follow must resolve the checkout that is actually serving.
func TestASuccessfulStartMakesTheNextCommandUseTheSameCheckout(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	// The shape an older build left behind: the document spells out the built-in
	// guess, which on this machine does not exist.
	f.documentNaming(t, f.guess())

	flag := f.repo
	if got := f.run(t, config.Overrides{RepoDir: &flag}); got.RepoDir != f.repo || got.Sources.RepoDir != "flag" {
		t.Fatalf("settings = %+v, want the flag's checkout", got)
	}
	f.startSucceeds(t)

	f.wantRecordedCheckout(t, f.repo)
	if out := f.out.String(); !strings.Contains(out, "the checkout "+f.repo+" was written into the settings document") {
		t.Fatalf("start did not report the recorded checkout:\n%s", out)
	}

	// The next invocation, resolved the way a new process resolves.
	next := f.run(t, config.Overrides{})
	if next.RepoDir != f.repo {
		t.Fatalf("the next command resolved %q, want %q", next.RepoDir, f.repo)
	}
	if next.Sources.RepoDir != "file" || next.ConfiguredRepoDir != f.repo {
		t.Fatalf("settings = %+v, want the checkout read back from the document", next)
	}
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.RepoDir != f.repo {
		t.Fatalf("status = %+v, want the checkout that is serving", status)
	}
	wantNoFailingCheckoutRow(t, f.Doctor(context.Background()))

	// The record names the tree the instance was started from, so a question
	// about the running service is answered from a fact rather than a guess.
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the start must leave a record")
	}
	if record.RepoDir != f.repo {
		t.Fatalf("record = %+v, want the checkout it was started from", record)
	}
}

// TestTheEnvironmentIsRecordedLikeAFlag pins that the second explicit layer
// behaves the same way: DSH_REPO_DIR names the checkout for this run, and
// because the document decides nothing it becomes the checkout of every later
// command too.
func TestTheEnvironmentIsRecordedLikeAFlag(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.documentNaming(t, f.guess())
	f.withEnv(t, paths.EnvRepoDir, f.repo)

	if got := f.run(t, config.Overrides{}); got.RepoDir != f.repo || got.Sources.RepoDir != "env" {
		t.Fatalf("settings = %+v, want the environment's checkout", got)
	}
	f.startSucceeds(t)

	f.wantRecordedCheckout(t, f.repo)
	// The variable is gone for the next command, which must not need it.
	f.withEnv(t, paths.EnvRepoDir, "")
	if next := f.run(t, config.Overrides{}); next.RepoDir != f.repo {
		t.Fatalf("the next command resolved %q, want %q", next.RepoDir, f.repo)
	}
}

// TestADecidedCheckoutIsNeverRewrittenAndTheMismatchIsSaidOutLoud pins the limit
// of the rule: a document that names its own checkout belongs to the operator. A
// run from another tree works, says which tree it used, and leaves the document
// alone — so the next command still follows the operator's choice rather than
// silently switching to a one-off.
func TestADecidedCheckoutIsNeverRewrittenAndTheMismatchIsSaidOutLoud(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	decided := makeCheckout(t, filepath.Join(f.root, "decided-checkout"))
	f.documentNaming(t, decided)

	flag := f.repo
	if got := f.run(t, config.Overrides{RepoDir: &flag}); got.RepoDir != f.repo {
		t.Fatalf("settings = %+v, want the flag's checkout for this run", got)
	}
	f.startSucceeds(t)

	f.wantRecordedCheckout(t, decided)
	if out := f.out.String(); !strings.Contains(out, "using the checkout "+f.repo) ||
		!strings.Contains(out, "(configured as "+decided) {
		t.Fatalf("start did not report which checkout it used:\n%s", out)
	}
	if next := f.run(t, config.Overrides{}); next.RepoDir != decided {
		t.Fatalf("the next command resolved %q, want the operator's %q", next.RepoDir, decided)
	}
}

// TestAnEnvironmentOverrideOfTheCheckoutIsReported pins the warning that makes an
// invisible layer visible.
//
// An exported DSH_REPO_DIR leaves no trace in the command that ran, so a worker
// shell carrying one must not silently move the service onto another checkout:
// the line names the variable, the checkout in effect and the one the document
// holds, which is everything the operator needs to decide which of the two is
// wrong.
func TestAnEnvironmentOverrideOfTheCheckoutIsReported(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	decided := makeCheckout(t, filepath.Join(f.root, "decided-checkout"))
	f.documentNaming(t, decided)
	f.withEnv(t, paths.EnvRepoDir, f.repo)
	if got := f.run(t, config.Overrides{}); got.RepoDir != f.repo || got.Sources.RepoDir != "env" {
		t.Fatalf("settings = %+v, want the environment's checkout", got)
	}

	f.startSucceeds(t)

	// The document still decides, so nothing was recorded for later commands.
	f.wantRecordedCheckout(t, decided)
	errOut := f.errOut.String()
	for _, want := range []string{paths.EnvRepoDir + "=" + f.repo, "repoDir=" + decided} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr = %q, want it to contain %q", errOut, want)
		}
	}
}

// TestAnAlreadyRunningServiceTeachesTheDocumentItsCheckout pins the repair that
// needs no new start: a start that finds the port already served records the
// checkout the running instance was started from, so the invocation that says
// "already running" is also the one that makes the configuration true again.
//
// The invocation names no checkout of its own — no flag, no environment — so the
// only thing that can supply the value is the record of the running instance.
// That is the difference between "the configuration follows reality" and "the
// configuration follows whatever this one command happened to resolve", and only
// the first one can repair a machine whose document is silent.
func TestAnAlreadyRunningServiceTeachesTheDocumentItsCheckout(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)

	// Model a document from before the checkout was recorded: it names nothing.
	f.emptyDocument(t)
	f.run(t, config.Overrides{})
	if got := f.Settings.RepoDir; got != f.guess() {
		t.Fatalf("this run resolved %q, want the built-in guess: the record is the only source", got)
	}

	spawns := len(f.host.spawnCalls())
	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatal("the second start must report the server as already running")
	}
	if len(f.host.spawnCalls()) != spawns {
		t.Fatalf("the second start launched a process: %v", f.host.spawnCalls())
	}
	f.wantRecordedCheckout(t, f.repo)
	if out := f.out.String(); !strings.Contains(out, "the checkout "+f.repo+" was written into the settings document") {
		t.Fatalf("start did not report the recorded checkout:\n%s", out)
	}
}

// TestAnAlreadyRunningServiceRecordsAnExplicitCheckoutWhenTheRecordNamesNone
// pins the fallback for a record written by a build that did not record the
// checkout. The operator's own --repo is then the only evidence of which tree
// they mean, and it is written down — but only once the directory proves to be a
// DeepSeek Harness checkout, because a mistyped path must not become the
// configuration of every later command.
func TestAnAlreadyRunningServiceRecordsAnExplicitCheckoutWhenTheRecordNamesNone(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)
	f.emptyDocument(t)

	// The record is rewritten the way an older build wrote it: no checkout.
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the start must leave a record")
	}
	record.RepoDir = ""
	if err := f.Record.Save(record); err != nil {
		t.Fatalf("save the record: %v", err)
	}

	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})
	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start with a named checkout: %v", err)
	}
	f.wantRecordedCheckout(t, f.repo)
}

// TestAnAlreadyRunningServiceRefusesToRecordAMistypedCheckout pins the other half
// of that fallback: a directory that is not a DeepSeek Harness checkout is not
// written down, so a typo cannot become every later command's configuration.
//
// The record is blanked first on purpose. While it names the checkout the running
// instance really serves, that fact wins over this invocation's flag — which is
// the right answer, and the row above pins it — so the fallback is only reachable
// for a record that names none.
func TestAnAlreadyRunningServiceRefusesToRecordAMistypedCheckout(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)
	f.emptyDocument(t)
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the start must leave a record")
	}
	record.RepoDir = ""
	if err := f.Record.Save(record); err != nil {
		t.Fatalf("save the record: %v", err)
	}

	mistyped := filepath.Join(f.root, "not-a-checkout")
	if err := os.MkdirAll(mistyped, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f.run(t, config.Overrides{RepoDir: &mistyped})
	if _, err := f.Start(context.Background()); err != nil {
		t.Fatalf("Start with a mistyped checkout: %v", err)
	}
	f.wantRecordedCheckout(t, "")
}

// TestAnAlreadyRunningServiceKeepsADecidedCheckoutAndWarns pins the other side:
// with a document that decides a checkout, a running instance that disagrees with
// it is reported and nothing is rewritten — the operator's file is not dshctl's
// to correct.
func TestAnAlreadyRunningServiceKeepsADecidedCheckoutAndWarns(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)

	decided := makeCheckout(t, filepath.Join(f.root, "decided-checkout"))
	f.documentNaming(t, decided)
	if got := f.run(t, config.Overrides{}); got.RepoDir != decided {
		t.Fatalf("settings checkout = %q, want the decided %q", got.RepoDir, decided)
	}

	spawns := len(f.host.spawnCalls())
	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatal("the server must be reported as running")
	}
	if len(f.host.spawnCalls()) != spawns {
		t.Fatalf("a second server was launched: %v", f.host.spawnCalls())
	}
	f.wantRecordedCheckout(t, decided)
	if errOut := f.errOut.String(); !strings.Contains(errOut, "the running service (pid=") ||
		!strings.Contains(errOut, f.repo) {
		t.Fatalf("the mismatch was not reported:\n%s", errOut)
	}
}

// TestABuildRecordsTheCheckoutItBuilt pins the same rule for the command that
// proves a checkout is usable without serving it: recording it is what keeps the
// `dshctl build && dshctl start` sequence on the tree that was just built.
func TestABuildRecordsTheCheckoutItBuilt(t *testing.T) {
	f := newFixture(t)
	f.documentNaming(t, f.guess())
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	if err := f.RunBuild(context.Background()); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	f.wantRecordedCheckout(t, f.repo)
	// A build must not decide the runtime: the release is recorded by a start,
	// which is the run that actually serves with it.
	f.wantNoRecordedNodeVersion(t)
	if next := f.run(t, config.Overrides{}); next.RepoDir != f.repo {
		t.Fatalf("the next command resolved %q, want %q", next.RepoDir, f.repo)
	}
}

// TestAnUpdateRecordsTheCheckoutItUpdated pins the third writer: an update runs
// git, install and build in the checkout, and the checkout it did that in is the
// one every later command has to resolve.
func TestAnUpdateRecordsTheCheckoutItUpdated(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.documentNaming(t, f.guess())
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	f.wantRecordedCheckout(t, f.repo)
	if next := f.run(t, config.Overrides{}); next.RepoDir != f.repo {
		t.Fatalf("the next command resolved %q, want %q", next.RepoDir, f.repo)
	}
}

// TestANoOpUpdateRecordsTheCheckoutItRanAgainst pins the write-back contract for
// the short circuit: the update succeeded against this checkout even though
// nothing moved, so a document that decides nothing has to learn it — the next
// plain command must not fall back to the built-in guess.
func TestANoOpUpdateRecordsTheCheckoutItRanAgainst(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.documentNaming(t, f.guess())
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})
	f.host.gitHead = f.host.gitRemote

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	f.wantRecordedCheckout(t, f.repo)
	if next := f.run(t, config.Overrides{}); next.RepoDir != f.repo {
		t.Fatalf("the next command resolved %q, want %q", next.RepoDir, f.repo)
	}
}

// TestAFailedStartRecordsNothing pins the boundary: only what ran is written
// down. A start that never served must leave the document deciding nothing, or
// the next command would follow a checkout that demonstrably did not work.
func TestAFailedStartRecordsNothing(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.emptyDocument(t)
	f.host.spawnErr = errors.New("no processes today")
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Failure)
	if _, present := f.configDocument(t)["repoDir"]; present {
		t.Fatalf("document = %v, want no checkout: the start never served", f.configDocument(t))
	}
}

// TestTheRecordWrittenBeforeThePortAnswersCarriesTheCheckout pins the first of
// the two records a start writes. It is the only handle on a server that came up
// while dshctl was killed, and a survivor whose checkout is unknown cannot be
// described to the operator — so the checkout is written with the wrapper, not
// only with the listener.
func TestTheRecordWrittenBeforeThePortAnswersCarriesTheCheckout(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	// The port never answers, so the start stays in the window between writing the
	// wrapper record and replacing it with the listener — which is exactly the
	// window this row is about. The record is read from the test while the start
	// is still waiting.
	done := make(chan error, 1)
	go func() {
		_, err := f.Start(context.Background())
		done <- err
	}()
	var seen domain.Record
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && seen.PID == 0 {
		if record, ok, err := f.Record.Load(); err == nil && ok {
			seen = record
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := <-done; err == nil {
		t.Fatal("a start whose port never answers must fail")
	}
	if seen.PID == 0 {
		t.Fatal("the wrapper record was never written")
	}
	if seen.RepoDir != f.repo {
		t.Fatalf("wrapper record = %+v, want the checkout it was started from", seen)
	}
}

// TestAnAdoptedSurvivorKeepsTheCheckoutItWasStartedFrom pins that adoption
// describes the process rather than the configuration: a server left behind by
// an interrupted start of another checkout keeps its own checkout, because that
// is the tree it is serving.
func TestAnAdoptedSurvivorKeepsTheCheckoutItWasStartedFrom(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	other := makeCheckout(t, filepath.Join(f.root, "other-checkout"))
	// The wrapper is gone; its group still serves the port.
	f.host.add(8101, "node apps/cli/src/bin.ts web", fixtureStartTime).group = 8100
	f.host.listen(8101)
	f.survivorRecord(t, 8100, other)

	// The operator's current configuration names a different checkout.
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatal("the survivor must be adopted and reported as running")
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("adoption must leave a record")
	}
	if record.RepoDir != other {
		t.Fatalf("record = %+v, want the survivor's own checkout %q", record, other)
	}
}

// TestDoctorNamesAServiceRunningFromAnotherCheckout pins the row that explains
// the mismatch: without it, a diagnosis of the configured checkout reads like a
// broken service even though one is serving happily from somewhere else.
func TestDoctorNamesAServiceRunningFromAnotherCheckout(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)
	decided := makeCheckout(t, filepath.Join(f.root, "decided-checkout"))
	f.documentNaming(t, decided)
	f.run(t, config.Overrides{})

	checks := f.Doctor(context.Background())
	wantNoFailingCheckoutRow(t, checks)
	var found *Check
	for index := range checks {
		if checks[index].Name == "service checkout" {
			found = &checks[index]
		}
	}
	if found == nil {
		t.Fatalf("doctor did not report the running instance's checkout:\n%s", renderChecks(checks))
	}
	if found.Status != CheckWarn {
		t.Fatalf("row = %+v, want a warning", *found)
	}
	for _, want := range []string{f.repo, decided} {
		if !strings.Contains(found.Detail, want) {
			t.Fatalf("row = %+v, want it to name %q", *found, want)
		}
	}
}

// TestStatusNamesTheCheckoutTheRunningServiceUses pins the same fact in the
// human-readable status, which is the command an operator reads first.
func TestStatusNamesTheCheckoutTheRunningServiceUses(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)
	decided := makeCheckout(t, filepath.Join(f.root, "decided-checkout"))
	f.documentNaming(t, decided)
	f.run(t, config.Overrides{})

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.RecordedRepoDir != f.repo {
		t.Fatalf("status = %+v, want the running instance's checkout", status)
	}
	// The rendered line is the shell's business (internal/cli renders it); what
	// this test pins is that the observation carries both checkouts, which is
	// what any renderer needs to name them: the running instance serves the
	// recorded one, while the configuration points somewhere else.
	if status.RecordedRepoDir != f.repo {
		t.Fatalf("status = %+v, want the checkout this instance serves", status)
	}
	if status.RepoDir == status.RecordedRepoDir {
		t.Fatalf("status = %+v, want the configured checkout to differ from the served one", status)
	}
}

// TestAForeignCheckoutDoesNotBlockABuild pins the cross-port guard's other
// direction: a sibling server built from a different tree cannot be disturbed by
// rebuilding this one, so refusing the build would be a refusal for a reason that
// is not true.
func TestAForeignCheckoutDoesNotBlockABuild(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	other := makeCheckout(t, filepath.Join(f.root, "other-checkout"))
	f.host.serving(8200, "pnpm --dir other dsh web")
	f.saveRecord(t, domain.Record{
		PID: 8200, StartedAt: fixtureStartTime, Port: f.Settings.Port + 1,
		Phase: domain.PhaseRunning, RepoDir: other,
	})

	if err := f.RunBuild(context.Background()); err != nil {
		t.Fatalf("a build must not be blocked by a server from another checkout: %v", err)
	}
}

// TestASiblingOnTheSameCheckoutStillBlocksABuild pins the guard staying as
// strict as it was: a sibling that names this checkout, or one that names none
// because it was written before the field existed, still refuses.
func TestASiblingOnTheSameCheckoutStillBlocksABuild(t *testing.T) {
	for name, checkout := range map[string]string{"the same checkout": "same", "an older record": ""} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.servePATHNode(t, config.TestedNodeVersion)
			record := domain.Record{
				PID: 8200, StartedAt: fixtureStartTime, Port: f.Settings.Port + 1, Phase: domain.PhaseRunning,
			}
			if checkout == "same" {
				record.RepoDir = f.repo
			}
			f.host.serving(8200, "pnpm --dir repo dsh web")
			f.saveRecord(t, record)

			err := f.RunBuild(context.Background())
			wantCode(t, err, exitcode.Preflight)
			wantContains(t, err, "is in use by services dshctl manages (ports")
		})
	}
}

// TestAStaleConfigurationStillStopsTheRunningService pins that a configuration
// pointing at a directory that does not exist cannot take away the ability to end
// the service: stopping is about the recorded process, not about the checkout.
func TestAStaleConfigurationStillStopsTheRunningService(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)
	// The configuration now names a checkout that does not exist.
	f.documentNaming(t, filepath.Join(f.root, "gone-checkout"))
	f.run(t, config.Overrides{})

	if _, err := f.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	f.wantNoRecordOnDisk(t)
}

// TestTheSpawnUsesTheCheckoutThisRunResolved pins that the directory the server
// is launched in follows the resolved checkout: the fixture set the settings and
// the repository helper to the same value, so a start that used the wrong one of
// the two would have passed every other test.
func TestTheSpawnUsesTheCheckoutThisRunResolved(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	flag := f.repo
	f.run(t, config.Overrides{RepoDir: &flag})

	f.startSucceeds(t)

	call := f.wantSpawn(t)
	if call.dir != f.repo {
		t.Fatalf("spawn dir = %q, want the resolved checkout %q", call.dir, f.repo)
	}
	if !hasArg(call.args, "--dir") || !hasArg(call.args, f.repo) {
		t.Fatalf("spawn args = %v, want the resolved checkout", call.args)
	}
}

// TestAMissingCheckoutNamesTheRemedy pins the message an operator sees when the
// configuration points nowhere: it has to name the path it tried and both ways of
// naming another one, or the only way out is guessing.
func TestAMissingCheckoutNamesTheRemedy(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	gone := filepath.Join(f.root, "gone-checkout")
	f.documentNaming(t, gone)
	f.run(t, config.Overrides{})

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	for _, want := range []string{gone, paths.EnvRepoDir, "--repo"} {
		wantContains(t, err, want)
	}
}

// documentNaming writes a settings document that names one checkout.
func (f *fixture) documentNaming(t *testing.T, checkout string) {
	t.Helper()
	f.writeDocument(t, map[string]any{"repoDir": checkout})
}

// emptyDocument writes a settings document that names nothing, which is what a
// fresh installation has and what an installation whose checkout was never
// recorded keeps.
func (f *fixture) emptyDocument(t *testing.T) {
	t.Helper()
	f.writeDocument(t, map[string]any{})
}

// writeDocument writes a settings document from a decoded map.
func (f *fixture) writeDocument(t *testing.T, document map[string]any) {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the document: %v", err)
	}
	writeFile(t, f.Settings.ConfigPath, string(data)+"\n")
}

// withEnv makes one environment variable visible to the resolution this fixture
// performs, which is how a sequence can carry DSH_REPO_DIR for one call.
func (f *fixture) withEnv(t *testing.T, key, value string) {
	t.Helper()
	previous := f.Getenv
	f.Getenv = func(name string) string {
		if name == key {
			return value
		}
		return previous(name)
	}
}

// saveRecord writes a runtime record for another port into the state directory.
func (f *fixture) saveRecord(t *testing.T, record domain.Record) {
	t.Helper()
	store := recordStore(filepath.Join(f.state, fmt.Sprintf(config.StateFileNamePattern, record.Port)))
	if err := store.Save(record); err != nil {
		t.Fatalf("save the record: %v", err)
	}
}

// survivorRecord writes the record shape an interrupted start leaves behind: a
// wrapper that is gone, with the port served by a process in its group.
func (f *fixture) survivorRecord(t *testing.T, wrapper int, checkout string) domain.Record {
	t.Helper()
	record := domain.Record{
		PID: wrapper, SpawnedPID: wrapper, StartedAt: fixtureStartTime,
		Port: f.Settings.Port, Phase: domain.PhaseRunning, RepoDir: checkout,
	}
	f.host.add(wrapper, "pnpm --dir repo dsh web", fixtureStartTime)
	if err := f.Record.Save(record); err != nil {
		t.Fatalf("save the survivor record: %v", err)
	}
	f.host.remove(wrapper)
	return record
}

// hasArg reports whether an argument list carries want.
func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
