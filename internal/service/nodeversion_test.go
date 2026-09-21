package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/state"
)

// The Node release is decided once and then written down: the first successful
// start records what the machine provided, and every later run uses that. The
// tables below are the service half of the decision table — W is the write-back
// and E is the gate at each entry point — and they are frozen in
// TestServiceNodeTablesAreComplete.

// startSucceeds runs a start whose child takes the port, which is what makes the
// call a success and therefore the moment a release may be recorded.
func (f *fixture) startSucceeds(t *testing.T) StartResult {
	t.Helper()
	f.host.spontaneouslyServed = true
	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return result
}

// writeBackRows is the W section of the decision table.
var writeBackRows = map[string]func(*testing.T){
	"W1":  testWriteBackW1RecordsTheDiscoveredRelease,
	"W2":  testWriteBackW2RecordsNothingWhenTheStartFails,
	"W3":  testWriteBackW3RecordsNoReleaseWhenTheServiceIsAlreadyRunning,
	"W4":  testWriteBackW4RecordsAReleaseNamedByAFlag,
	"W5":  testWriteBackW5RecordsAReleaseNamedByTheEnvironment,
	"W6":  testWriteBackW6KeepsAConfiguredReleaseWhenAFlagOverridesIt,
	"W7":  testWriteBackW7UsesTheEnvironmentAndKeepsTheConfiguredRelease,
	"W8":  testWriteBackW8RecordsAnUnverifiedMajorVersion,
	"W9":  testWriteBackW9RecordsDuringAnUpdate,
	"W10": testWriteBackW10NeverWritesFromAReadOnlyCommand,
	"W11": testWriteBackW11ReportsADocumentItCannotWrite,
}

// TestWriteBackMatrix runs every row of the write-back table.
func TestWriteBackMatrix(t *testing.T) {
	for id, run := range writeBackRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testWriteBackW1RecordsTheDiscoveredRelease pins the first start of a fresh
// installation: nothing named a release, so PATH decided it, and the start wrote
// the decision down.
func testWriteBackW1RecordsTheDiscoveredRelease(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, config.TestedNodeVersion)
	resolved := f.resolvedNode(t)
	if resolved.Source != nodejs.SourcePath {
		t.Fatalf("resolved = %+v, want the release discovered on PATH", resolved)
	}
	f.wantNodeBinDir(t, resolved.BinDir)
	if !strings.Contains(f.out.String(), "已将 Node "+config.TestedNodeVersion+" 写入配置") {
		t.Fatalf("stdout = %q, want the recorded release reported", f.out.String())
	}
	// The record explains which runtime this instance is running, which is the
	// only place that answers it once --node can differ from the document.
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no runtime record was written")
	}
	if record.NodeVersion != config.TestedNodeVersion || record.NodePath != resolved.NodePath {
		t.Fatalf("record = %+v, want the runtime it was started with", record)
	}
}

// testWriteBackW2RecordsNothingWhenTheStartFails pins the precondition: only a
// runtime that demonstrably served is written down. A failed start must not
// leave a release behind for the next attempt to inherit.
func testWriteBackW2RecordsNothingWhenTheStartFails(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.host.spawnErr = errors.New("resource temporarily unavailable")

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Failure)
	f.wantNoRecordedNodeVersion(t)
}

// testWriteBackW3RecordsNoReleaseWhenTheServiceIsAlreadyRunning pins the early
// return: a start that finds the server up does not resolve a runtime at all, so
// there is no release to record.
//
// The document is not left entirely alone in that case — the checkout the
// running instance was started from is recorded, because it is the one fact
// every later command needs — and that half is pinned by the repoDir rows
// (repodir_test.go). What this row pins is that no release is invented for a run
// that never resolved one.
func testWriteBackW3RecordsNoReleaseWhenTheServiceIsAlreadyRunning(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)

	// Model an installation whose document does not name a release yet.
	writeFile(t, f.Settings.ConfigPath, "{}\n")
	f.Settings.ConfiguredNodeVersion = ""

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatal("the second start must report the server as already running")
	}
	if got, present := f.configDocument(t)["nodeVersion"]; present {
		t.Fatalf("document nodeVersion = %v, want none: no runtime was resolved", got)
	}
}

// testWriteBackW4RecordsAReleaseNamedByAFlag pins the rule for --node on an
// installation that has not determined a release: the flag decides this run and,
// because the document names none, it is written down.
func testWriteBackW4RecordsAReleaseNamedByAFlag(t *testing.T) {
	f := newFixture(t)
	f.installNodeTree(t, "24.19.0")
	// The document names nothing: the flag is what decides, and what is recorded.
	f.Settings.NodeVersion = "24.19.0"
	f.Settings.ConfiguredNodeVersion = ""
	f.Settings.Sources.NodeVersion = "flag"

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, "24.19.0")
	f.wantNodeBinDir(t, f.seedNodeBinDir(t, "24.19.0"))
}

// testWriteBackW5RecordsAReleaseNamedByTheEnvironment pins that the environment
// variable is the same knob as the flag, including this rule.
func testWriteBackW5RecordsAReleaseNamedByTheEnvironment(t *testing.T) {
	f := newFixture(t)
	f.installNodeTree(t, "24.19.0")
	f.Settings.NodeVersion = "24.19.0"
	f.Settings.ConfiguredNodeVersion = ""
	f.Settings.Sources.NodeVersion = "env"

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, "24.19.0")
}

// testWriteBackW6KeepsAConfiguredReleaseWhenAFlagOverridesIt pins the rule that
// makes --node safe to use: it changes this run and nothing else. The document
// keeps what it named, the child gets the flag's runtime, and the operator is
// told how to make the change permanent.
func testWriteBackW6KeepsAConfiguredReleaseWhenAFlagOverridesIt(t *testing.T) {
	f := newFixture(t)
	f.pinNode(t, config.TestedNodeVersion)
	f.installNodeTree(t, "24.19.0")
	f.Settings.NodeVersion = "24.19.0"
	f.Settings.Sources.NodeVersion = "flag"

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, config.TestedNodeVersion)
	f.wantNodeBinDir(t, f.seedNodeBinDir(t, "24.19.0"))
	stdout := f.out.String()
	for _, want := range []string{"本次使用 Node 24.19.0", config.TestedNodeVersion, f.Settings.ConfigPath} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
}

// testWriteBackW7UsesTheEnvironmentAndKeepsTheConfiguredRelease pins the two
// halves of an environment override: the variable decides this run, and the
// document — which belongs to the operator — is neither rewritten nor silently
// ignored. The warning is the half that cannot be seen otherwise, because an
// exported variable leaves no trace in the command that was typed.
func testWriteBackW7UsesTheEnvironmentAndKeepsTheConfiguredRelease(t *testing.T) {
	f := newFixture(t)
	f.pinNode(t, config.TestedNodeVersion)
	f.installNodeTree(t, "26.1.0")
	f.Settings.NodeVersion = "26.1.0"
	f.Settings.Sources.NodeVersion = "env"
	f.Getenv = func(key string) string {
		if key == paths.EnvNodeVersion {
			return "26.1.0"
		}
		return ""
	}

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, config.TestedNodeVersion)
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no runtime record was written")
	}
	if record.NodeVersion != "26.1.0" {
		t.Fatalf("record.NodeVersion = %q, want the release the environment named", record.NodeVersion)
	}
	stderr := f.errOut.String()
	for _, want := range []string{paths.EnvNodeVersion + "=26.1.0", "nodeVersion=" + config.TestedNodeVersion} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// testWriteBackW8RecordsAnUnverifiedMajorVersion pins the middle ground end to
// end: a release dshctl has not been verified against is used and reported, and
// — because it demonstrably started — it is the release recorded.
func testWriteBackW8RecordsAnUnverifiedMajorVersion(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, "26.1.0")

	f.startSucceeds(t)

	f.wantRecordedNodeVersion(t, "26.1.0")
	if !strings.Contains(f.errOut.String(), "不在 dshctl 的验证范围内") {
		t.Fatalf("stderr = %q, want the unverified release reported", f.errOut.String())
	}
}

// testWriteBackW9RecordsDuringAnUpdate pins that the rule is attached to a
// successful start rather than to the start command: the restart an update
// performs records the release the same way.
func testWriteBackW9RecordsDuringAnUpdate(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	f.startSucceeds(t)

	// Model an installation that has not determined a release yet, with the
	// server nevertheless running.
	if err := os.Remove(f.Settings.ConfigPath); err != nil {
		t.Fatalf("remove the settings document: %v", err)
	}
	f.Settings.ConfiguredNodeVersion = ""

	if err := f.RunUpdate(context.Background(), "latest"); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	f.wantRecordedNodeVersion(t, config.TestedNodeVersion)
}

// testWriteBackW10NeverWritesFromAReadOnlyCommand pins the promise the reporting
// commands make: they read, they do not write. The document is compared byte for
// byte, so a stray rewrite of any field fails here.
//
// A build is deliberately not part of this row: it is a mutating command that
// runs in the checkout, and recording the checkout it built is what keeps the
// next plain start on the tree that was just built. What must never write is the
// set of commands whose whole job is to report — status, doctor, the logs, the
// URL and the version.
func testWriteBackW10NeverWritesFromAReadOnlyCommand(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)
	if err := f.Settings.Provision(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	before, err := os.ReadFile(f.Settings.ConfigPath)
	if err != nil {
		t.Fatalf("read the settings document: %v", err)
	}

	if _, err := f.Status(context.Background(), f.Settings.Port); err != nil {
		t.Fatalf("Status: %v", err)
	}
	_ = f.Doctor(context.Background())
	// The address is unavailable while nothing serves; the call is made for its
	// side effects, which must be none.
	_, _ = f.WebURL(context.Background())
	// A missing log is an error for the command to report, not a write; the call
	// is made because reading is all it may do.
	_ = f.Logs(context.Background(), LogsOptions{Lines: 5})

	after, err := os.ReadFile(f.Settings.ConfigPath)
	if err != nil {
		t.Fatalf("read the settings document: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the settings document changed:\nbefore: %s\nafter:  %s", before, after)
	}
}

// testWriteBackW11ReportsADocumentItCannotWrite pins the failure handling: the
// server is already running by the time the release is recorded, so a document
// that cannot be written is a warning, not a failed start.
func testWriteBackW11ReportsADocumentItCannotWrite(t *testing.T) {
	f := newFixture(t)
	f.host.spontaneouslyServed = true

	spawn := f.Spawn
	f.Spawn = func(path string, args []string, dir string, env []string, log *os.File) (int, func(context.Context) bool, error) {
		// The document becomes unwritable while the child is starting: a
		// directory where the file was is the shape of a state directory being
		// replaced underneath the operation.
		if err := os.Remove(f.Settings.ConfigPath); err != nil {
			t.Errorf("remove the settings document: %v", err)
		}
		if err := os.MkdirAll(f.Settings.ConfigPath, 0o700); err != nil {
			t.Errorf("mkdir at the settings path: %v", err)
		}
		return spawn(path, args, dir, env, log)
	}

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if result.SpawnedPID == 0 {
		t.Fatal("the server must be running even though the document could not be written")
	}
	if !strings.Contains(f.errOut.String(), "无法") {
		t.Fatalf("stderr = %q, want the failure to write the document reported", f.errOut.String())
	}
	if strings.Contains(f.out.String(), "已将 Node") {
		t.Fatalf("stdout = %q, want no success claim about the document", f.out.String())
	}
}

// gateRows is the E section of the decision table: the gate at every entry point.
var gateRows = map[string]func(*testing.T){
	"E1": testGateE1RefusesTooOldAReleaseForStart,
	"E2": testGateE2RefusesTooOldAReleaseForUpdate,
	"E3": testGateE3RefusesTooOldAReleaseForBuild,
	"E4": testGateE4ReportsTooOldAReleaseInDoctor,
	"E5": testGateE5ReportsAnUnverifiedMajorVersionEverywhere,
	"E6": testGateE6UsesTheSameJudgmentInDoctorAndStart,
}

// TestGateMatrix runs every row of the entry-point gate table.
func TestGateMatrix(t *testing.T) {
	for id, run := range gateRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testGateE1RefusesTooOldAReleaseForStart pins the hard floor at the command an
// operator runs: below the minimum there is no start, whatever named the release.
func testGateE1RefusesTooOldAReleaseForStart(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, "22.14.0")

	_, err := f.Start(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "低于最低要求 "+config.MinNodeVersion)
	wantContains(t, err, "nvm install 24")
	f.wantNoSpawn(t)
	f.wantNoRecordedNodeVersion(t)
}

// testGateE2RefusesTooOldAReleaseForUpdate pins the same floor for update, which
// resolves a runtime of its own before it touches the checkout.
func testGateE2RefusesTooOldAReleaseForUpdate(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, "22.14.0")

	err := f.RunUpdate(context.Background(), "latest")
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "低于最低要求")
	f.wantNoCheckoutUpdate(t)
}

// testGateE3RefusesTooOldAReleaseForBuild pins the same floor for build.
func testGateE3RefusesTooOldAReleaseForBuild(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, "22.14.0")

	err := f.RunBuild(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "低于最低要求")
}

// testGateE4ReportsTooOldAReleaseInDoctor pins that the diagnostic reports the
// same refusal instead of suggesting the machine is healthy.
func testGateE4ReportsTooOldAReleaseInDoctor(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, "22.14.0")

	row := checkNamed(t, f.Doctor(context.Background()), "Node")
	if row.Status != CheckFail {
		t.Fatalf("Node row = %+v, want a failure", row)
	}
	if !strings.Contains(row.Detail, "低于最低要求") || !strings.Contains(row.Detail, "nvm install 24") {
		t.Fatalf("Node row detail = %q, want the reason and the remedy", row.Detail)
	}
}

// testGateE5ReportsAnUnverifiedMajorVersionEverywhere pins that the middle
// ground is a warning at every entry point: it does not block, and it is not
// silent.
func testGateE5ReportsAnUnverifiedMajorVersionEverywhere(t *testing.T) {
	for _, entry := range []struct {
		name string
		run  func(*fixture) error
	}{
		{"start", func(f *fixture) error {
			f.host.spontaneouslyServed = true
			_, err := f.Start(context.Background())
			return err
		}},
		{"build", func(f *fixture) error { return f.RunBuild(context.Background()) }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			f := newFixture(t)
			f.servePATHNode(t, "26.1.0")

			if err := entry.run(f); err != nil {
				t.Fatalf("%s: %v", entry.name, err)
			}
			if !strings.Contains(f.errOut.String(), "不在 dshctl 的验证范围内") {
				t.Fatalf("stderr = %q, want the unverified release reported", f.errOut.String())
			}
		})
	}

	f := newFixture(t)
	f.servePATHNode(t, "26.1.0")
	row := checkNamed(t, f.Doctor(context.Background()), "Node")
	if row.Status != CheckWarn {
		t.Fatalf("Node row = %+v, want a warning", row)
	}
	if !strings.Contains(row.Detail, "26.1.0") {
		t.Fatalf("Node row detail = %q, want the release named", row.Detail)
	}
}

// testGateE6UsesTheSameJudgmentInDoctorAndStart pins the invariant that makes
// the diagnostic worth reading: one machine, one verdict. A doctor that called a
// runtime healthy while a start refused it would send the operator in circles.
func testGateE6UsesTheSameJudgmentInDoctorAndStart(t *testing.T) {
	for _, version := range []string{"24.12.0", "24.20.0", "26.1.0", "22.14.0"} {
		t.Run(version, func(t *testing.T) {
			f := newFixture(t)
			f.servePATHNode(t, version)

			row := checkNamed(t, f.Doctor(context.Background()), "Node")
			f.host.spontaneouslyServed = true
			_, err := f.Start(context.Background())

			switch row.Status {
			case CheckFail:
				if err == nil {
					t.Fatalf("doctor refused Node %s but the start accepted it", version)
				}
			case CheckOK, CheckWarn:
				if err != nil {
					t.Fatalf("doctor accepted Node %s but the start refused it: %v", version, err)
				}
			default:
				t.Fatalf("unexpected status %q", row.Status)
			}
		})
	}
}

// TestServiceNodeTablesAreComplete fails when a decision-table row has no case.
func TestServiceNodeTablesAreComplete(t *testing.T) {
	assertTableRows(t, "W", []string{"W1", "W2", "W3", "W4", "W5", "W6", "W7", "W8", "W9", "W10", "W11"}, writeBackRows)
	assertTableRows(t, "E", []string{"E1", "E2", "E3", "E4", "E5", "E6"}, gateRows)
}

// assertTableRows compares a frozen row list with the cases implementing it.
func assertTableRows(t *testing.T, prefix string, frozen []string, cases map[string]func(*testing.T)) {
	t.Helper()
	if len(frozen) != len(cases) {
		t.Fatalf("%s 行数 = %d(冻结)%d(用例), want the same", prefix, len(frozen), len(cases))
	}
	seen := map[string]bool{}
	for _, id := range frozen {
		if seen[id] {
			t.Fatalf("冻结列表里 %s 重复", id)
		}
		seen[id] = true
		if fn, ok := cases[id]; !ok {
			t.Errorf("决策表第 %s 行没有对应用例", id)
		} else if fn == nil {
			t.Errorf("决策表第 %s 行的用例是空的", id)
		}
	}
	for id := range cases {
		if !seen[id] {
			t.Errorf("用例 %s 没有登记在冻结的决策表里", id)
		}
	}
}

// seedNodeBinDir reports the bin directory of a release the fixture installed
// under its own nvm tree, so a test can assert on the directory the child is
// handed without repeating the layout.
func (f *fixture) seedNodeBinDir(t *testing.T, version string) string {
	t.Helper()
	return filepath.Join(f.root, ".nvm", "versions", "node", "v"+version, "bin")
}

// TestResolvingAConfiguredReleaseStartsNoProcess pins what a configured release
// costs: the managers carry it in their directory names, so a start that finds it
// there runs no probe at all. The check matters because a probe is a process
// launch on the path of every command.
func TestResolvingAConfiguredReleaseStartsNoProcess(t *testing.T) {
	f := newFixture(t)
	f.seedNodeInstallation(t, config.TestedNodeVersion)

	f.startSucceeds(t)

	if probes := f.host.nodeProbes(); len(probes) != 0 {
		t.Fatalf("probes = %v, want none: the release is implied by the directory name", probes)
	}
}

// TestResolvingADiscoveredReleaseProbesTheBinary pins the other half: a release
// nobody named can only be learned from the binary, and it is asked twice — once
// for the release and once for the interpreter that actually runs.
func TestResolvingADiscoveredReleaseProbesTheBinary(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)

	f.startSucceeds(t)

	probes := f.host.nodeProbes()
	if len(probes) != 2 || probes[0] != "-v" || probes[1] != "-p process.execPath" {
		t.Fatalf("probes = %v, want the release question and the interpreter question", probes)
	}
}

// TestTheRecordNamesTheRuntimeTheInstanceUses pins what a running server leaves
// behind: the release and binary it was started with. Once --node can differ from
// the document, the record is the only thing that answers "what is this instance
// running".
func TestTheRecordNamesTheRuntimeTheInstanceUses(t *testing.T) {
	f := newFixture(t)
	f.pinNode(t, config.TestedNodeVersion)
	f.installNodeTree(t, "24.19.0")
	f.Settings.NodeVersion = "24.19.0"
	f.Settings.Sources.NodeVersion = "flag"

	f.startSucceeds(t)

	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no runtime record was written")
	}
	if record.NodeVersion != "24.19.0" {
		t.Fatalf("record.NodeVersion = %q, want the release this run used", record.NodeVersion)
	}
	if record.NodePath != filepath.Join(f.seedNodeBinDir(t, "24.19.0"), fixtureNodeName()) {
		t.Fatalf("record.NodePath = %q, want the binary this run used", record.NodePath)
	}

	// The diagnostic repeats it, so an operator reading the running record sees
	// the runtime without opening the JSON.
	row := checkNamed(t, f.Doctor(context.Background()), "运行记录")
	if !strings.Contains(row.Detail, "node=24.19.0") {
		t.Fatalf("runtime record row = %q, want the release of the running server", row.Detail)
	}

	// The structured status carries the same fact, so a script that reads
	// `status --json` is not left guessing which runtime is serving.
	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.RecordedNodeVersion != "24.19.0" || status.RecordedNodePath != record.NodePath {
		t.Fatalf("status = %+v, want the recorded runtime", status)
	}
}

// TestAnAdoptedSurvivorKeepsTheRuntimeItWasStartedWith pins that recovering a
// server from an interrupted start does not lose what it is running: the record
// is rewritten from the process table, and the runtime has to survive that.
func TestAnAdoptedSurvivorKeepsTheRuntimeItWasStartedWith(t *testing.T) {
	f := newFixture(t)
	f.servePATHNode(t, config.TestedNodeVersion)

	// The shape an interrupted start leaves behind: the record names the wrapper,
	// and the server that descends from it is the process holding the port.
	f.host.add(4241, "pnpm --dir repo dsh web", fixtureStartTime)
	listener := f.host.add(4242, "node apps/cli/src/bin.ts web", fixtureStartTime)
	listener.group = 4241
	f.host.listener = 4242
	if err := f.Record.Save(state.Record{
		PID:         4241,
		SpawnedPID:  4241,
		StartedAt:   fixtureStartTime,
		Port:        f.Settings.Port,
		Phase:       state.PhaseRunning,
		NodeVersion: config.TestedNodeVersion,
		NodePath:    "/opt/node/bin/node",
	}); err != nil {
		t.Fatalf("seed the record: %v", err)
	}

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !result.AlreadyRunning {
		t.Fatal("the surviving server must be adopted rather than started again")
	}

	adopted, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the adopted record is missing")
	}
	if adopted.PID != 4242 {
		t.Fatalf("adopted pid = %d, want the listener", adopted.PID)
	}
	if adopted.NodeVersion != config.TestedNodeVersion || adopted.NodePath != "/opt/node/bin/node" {
		t.Fatalf("adopted record = %+v, want the runtime of the surviving server", adopted)
	}
}

// TestResolvingWithoutAPlatformHomeIsRefused pins the boundary of the
// resolution: it needs a home directory to find the version managers in, and a
// machine that cannot provide one is a reported failure rather than a search of
// whatever directory dshctl was started in.
//
// The resolution is asked directly rather than through Start: a command reaches
// the settings first, and a machine with no home directory fails there — which
// config's own test pins. What is pinned here is the classification of the
// resolution's own answer.
func TestResolvingWithoutAPlatformHomeIsRefused(t *testing.T) {
	f := newFixture(t)
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	_, err := f.resolveNode(context.Background())
	wantCode(t, err, exitcode.Preflight)
	wantContains(t, err, "主目录")
}

// TestDoctorReportsARuntimeResolvedThroughAForwarder pins what the row says when
// PATH names a forwarding entry: the interpreter that actually runs, and that a
// forwarder was involved. Reporting the shim as the runtime would hide the very
// thing the resolution went out of its way to look through.
func TestDoctorReportsARuntimeResolvedThroughAForwarder(t *testing.T) {
	f := newFixture(t)
	shim, real := f.serveNodeThroughShim(t, config.TestedNodeVersion)

	row := checkNamed(t, f.Doctor(context.Background()), "Node")
	if row.Status != CheckOK {
		t.Fatalf("Node row = %+v, want it healthy", row)
	}
	for _, want := range []string{real, config.TestedNodeVersion, "经转发条目解析"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("Node row detail = %q, want it to contain %q", row.Detail, want)
		}
	}
	if strings.Contains(row.Detail, shim) {
		t.Fatalf("Node row detail = %q, want the interpreter rather than the forwarder %q", row.Detail, shim)
	}
}

// TestARuntimeBehindAForwarderIsWhatTheServerGets pins the whole point of
// looking through the forwarder: the child's PATH is put in front of the real
// interpreter, and the record names it, so nothing resolves the shim again at
// exec time and lands on a different runtime.
func TestARuntimeBehindAForwarderIsWhatTheServerGets(t *testing.T) {
	f := newFixture(t)
	_, real := f.serveNodeThroughShim(t, config.TestedNodeVersion)

	f.startSucceeds(t)

	f.wantNodeBinDir(t, filepath.Dir(real))
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("no runtime record was written")
	}
	if record.NodePath != real {
		t.Fatalf("record.NodePath = %q, want the interpreter %q", record.NodePath, real)
	}
}
