package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// The Node release is the one setting whose layers are ordered differently from
// every other setting — flag, then the settings file, then the environment — and
// the one setting a successful start writes back. The tables below are the P
// (precedence) and V (write-back) sections of the decision table; the frozen
// lists live beside them so a row cannot disappear quietly.

// testWorld is a throwaway installation: a state directory that may hold a
// settings document, a home directory, and an environment that may name a
// release or a checkout.
type testWorld struct {
	t          *testing.T
	root       string
	home       string
	stateDir   string
	configPath string
	vars       env
}

// newTestWorld returns a world with a state directory and no settings document.
//
// The platform home is redirected into the temporary root as well. Two built-in
// defaults are derived from it — the checkout guess (see DefaultRepoDir) and the
// harness home — so a test that left it pointing at the developer's own home
// would compare against a path no fixture can spell, and the developer's
// ~/deepseek-harness would decide whether a row passes.
func newTestWorld(t *testing.T) *testWorld {
	t.Helper()
	root := t.TempDir()
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &testWorld{
		t:          t,
		root:       root,
		home:       root,
		stateDir:   stateDir,
		configPath: filepath.Join(stateDir, "config.json"),
		vars: env{
			paths.EnvHarnessHome: filepath.Join(root, "harness"),
			paths.EnvStateDir:    stateDir,
		},
	}
}

// guess is the built-in checkout default for this world's home.
func (w *testWorld) guess() string { return DefaultRepoDir(w.home) }

// checkout is a checkout that is not the built-in guess: the guess is
// <home>/deepseek-harness, and this is a sibling of it.
func (w *testWorld) checkout() string {
	return filepath.Join(w.home, "projects", "deepseek-harness")
}

// writeDocument writes the settings document the world holds.
func (w *testWorld) writeDocument(document string) *testWorld {
	w.t.Helper()
	if err := os.WriteFile(w.configPath, []byte(document), 0o600); err != nil {
		w.t.Fatalf("write the settings document: %v", err)
	}
	return w
}

// withVariable sets one environment variable for this world.
func (w *testWorld) withVariable(key, value string) *testWorld {
	w.vars[key] = value
	return w
}

// load resolves the settings the way the program does.
func (w *testWorld) load(overrides Overrides) Settings {
	w.t.Helper()
	settings, err := Load(w.vars.Getenv, overrides)
	if err != nil {
		w.t.Fatalf("Load: %v", err)
	}
	return settings
}

// settings returns settings for this world without reading the document.
//
// The write-back rows are about what RecordRuntime does with whatever is on
// disk, including the documents the loader refuses, so they must not go through
// Load to reach it.
func (w *testWorld) settings() Settings {
	w.t.Helper()
	settings := Default(w.home)
	settings.StateDir = w.stateDir
	settings.ConfigPath = w.configPath
	settings.LogPath = filepath.Join(w.stateDir, DefaultLogFileName)
	return settings
}

// document returns the settings document as it is on disk.
func (w *testWorld) document() string {
	w.t.Helper()
	data, err := os.ReadFile(w.configPath)
	if err != nil {
		w.t.Fatalf("read the settings document: %v", err)
	}
	return string(data)
}

// fields decodes the settings document into a generic map.
func (w *testWorld) fields() map[string]any {
	w.t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(w.document()), &decoded); err != nil {
		w.t.Fatalf("decode the settings document: %v", err)
	}
	return decoded
}

// precedenceRows is the P section of the decision table.
var precedenceRows = map[string]func(*testing.T){
	"P1": testPrecedenceP1TheFlagWins,
	"P2": testPrecedenceP2TheEnvironmentBeatsTheFile,
	"P3": testPrecedenceP3TheEnvironmentAppliesWhenTheFileIsSilent,
	"P4": testPrecedenceP4NothingIsDeterminedByDefault,
	"P5": testPrecedenceP5WhitespaceIsNotAValue,
	"P6": testPrecedenceP6ValuesAreTrimmed,
	"P7": testPrecedenceP7NullAndEmptyMeanUndetermined,
	"P8": testPrecedenceP8TheConfiguredReleaseTracksTheFile,
}

// TestPrecedenceMatrix runs every row of the precedence table.
func TestPrecedenceMatrix(t *testing.T) {
	for id, run := range precedenceRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testPrecedenceP1TheFlagWins pins the top of the order.
func testPrecedenceP1TheFlagWins(t *testing.T) {
	w := newTestWorld(t).
		writeDocument(`{"nodeVersion": "24.19.0"}`).
		withVariable(paths.EnvNodeVersion, "24.18.0")
	version := "24.21.0"

	settings := w.load(Overrides{NodeVersion: &version})
	if settings.NodeVersion != "24.21.0" {
		t.Fatalf("NodeVersion = %q, want the flag's value", settings.NodeVersion)
	}
	if settings.Sources.NodeVersion != "flag" {
		t.Fatalf("source = %q, want flag", settings.Sources.NodeVersion)
	}
}

// testPrecedenceP2TheEnvironmentBeatsTheFile pins that this setting is layered
// like every other one: an exported variable is a decision about this run, and
// the document is what a run without one falls back to.
func testPrecedenceP2TheEnvironmentBeatsTheFile(t *testing.T) {
	w := newTestWorld(t).
		writeDocument(`{"nodeVersion": "24.19.0"}`).
		withVariable(paths.EnvNodeVersion, "24.21.0")

	settings := w.load(Overrides{})
	if settings.NodeVersion != "24.21.0" {
		t.Fatalf("NodeVersion = %q, want the environment's value to beat the file", settings.NodeVersion)
	}
	if settings.Sources.NodeVersion != "env" {
		t.Fatalf("source = %q, want env", settings.Sources.NodeVersion)
	}
	if settings.ConfiguredNodeVersion != "24.19.0" {
		t.Fatalf("ConfiguredNodeVersion = %q, want the file's value kept for the write-back rule",
			settings.ConfiguredNodeVersion)
	}
}

// testPrecedenceP3TheEnvironmentAppliesWhenTheFileIsSilent pins that the
// environment needs no cooperation from the document: a file that names a
// release is overridden above, and one that names none is simply the layer
// below.
func testPrecedenceP3TheEnvironmentAppliesWhenTheFileIsSilent(t *testing.T) {
	w := newTestWorld(t).
		writeDocument(`{"port": 3081}`).
		withVariable(paths.EnvNodeVersion, "24.21.0")

	settings := w.load(Overrides{})
	if settings.NodeVersion != "24.21.0" {
		t.Fatalf("NodeVersion = %q, want the environment's value", settings.NodeVersion)
	}
	if settings.Sources.NodeVersion != "env" {
		t.Fatalf("source = %q, want env", settings.Sources.NodeVersion)
	}
	if settings.ConfiguredNodeVersion != "" {
		t.Fatalf("ConfiguredNodeVersion = %q, want empty: the file names none", settings.ConfiguredNodeVersion)
	}
	if settings.Port != 3081 {
		t.Fatalf("port = %d, want the file's value", settings.Port)
	}
}

// testPrecedenceP4NothingIsDeterminedByDefault pins the fourth layer: with
// nothing configured the release is undetermined, which is what makes a
// successful start discover one and write it back.
func testPrecedenceP4NothingIsDeterminedByDefault(t *testing.T) {
	settings := newTestWorld(t).load(Overrides{})
	if settings.NodeVersion != "" {
		t.Fatalf("NodeVersion = %q, want it undetermined", settings.NodeVersion)
	}
	if settings.Sources.NodeVersion != "default" {
		t.Fatalf("source = %q, want default", settings.Sources.NodeVersion)
	}
	if settings.ConfiguredNodeVersion != "" {
		t.Fatalf("ConfiguredNodeVersion = %q, want empty", settings.ConfiguredNodeVersion)
	}
}

// testPrecedenceP5WhitespaceIsNotAValue pins that a blank layer is treated as an
// absent one rather than as a release named " ".
func testPrecedenceP5WhitespaceIsNotAValue(t *testing.T) {
	// An environment that only holds blanks leaves the release undetermined.
	blank := newTestWorld(t).withVariable(paths.EnvNodeVersion, "   ")
	if settings := blank.load(Overrides{}); settings.NodeVersion != "" || settings.Sources.NodeVersion != "default" {
		t.Fatalf("settings = %+v, want whitespace to be ignored", settings)
	}

	// A flag that only holds blanks does not shadow the file either.
	w := newTestWorld(t).writeDocument(`{"nodeVersion": "24.19.0"}`)
	blankFlag := "   "
	if settings := w.load(Overrides{NodeVersion: &blankFlag}); settings.NodeVersion != "24.19.0" {
		t.Fatalf("NodeVersion = %q, want the file's value: a blank flag is not a request", settings.NodeVersion)
	}

	// A document value that only holds blanks is undetermined, not a release.
	blankFile := newTestWorld(t).writeDocument(`{"nodeVersion": " "}`)
	settings := blankFile.load(Overrides{})
	if settings.NodeVersion != "" || settings.ConfiguredNodeVersion != "" {
		t.Fatalf("settings = %+v, want a blank document value to be ignored", settings)
	}
}

// testPrecedenceP6ValuesAreTrimmed pins that padding never reaches the
// comparison, so that a value copied out of a shell prompt still names the
// release it looks like.
func testPrecedenceP6ValuesAreTrimmed(t *testing.T) {
	w := newTestWorld(t).writeDocument(`{"nodeVersion": " 24.19.0 "}`)
	if settings := w.load(Overrides{}); settings.NodeVersion != "24.19.0" {
		t.Fatalf("NodeVersion = %q, want the padded document value trimmed", settings.NodeVersion)
	}

	environ := newTestWorld(t).withVariable(paths.EnvNodeVersion, "  24.21.0  ")
	if settings := environ.load(Overrides{}); settings.NodeVersion != "24.21.0" {
		t.Fatalf("NodeVersion = %q, want the padded environment value trimmed", settings.NodeVersion)
	}

	padded := " 24.22.0 "
	if settings := newTestWorld(t).load(Overrides{NodeVersion: &padded}); settings.NodeVersion != "24.22.0" {
		t.Fatalf("NodeVersion = %q, want the padded flag value trimmed", settings.NodeVersion)
	}
}

// testPrecedenceP7NullAndEmptyMeanUndetermined pins the two ways a document says
// "I do not name a release".
func testPrecedenceP7NullAndEmptyMeanUndetermined(t *testing.T) {
	for _, document := range []string{`{"port": null, "nodeVersion": null}`, `{"nodeVersion": ""}`} {
		w := newTestWorld(t).writeDocument(document)
		settings := w.load(Overrides{})
		if settings.NodeVersion != "" || settings.ConfiguredNodeVersion != "" {
			t.Fatalf("document %s produced %+v, want the release undetermined", document, settings)
		}
		if settings.Port != DefaultPort {
			t.Fatalf("document %s produced port %d, want the default", document, settings.Port)
		}
	}
}

// testPrecedenceP8TheConfiguredReleaseTracksTheFile pins the input the write-back
// rule is built on: what the file names, independently of which layer won. An
// override must not make the file look configured, and it must not make it look
// empty either — the difference decides whether a successful start rewrites it.
func testPrecedenceP8TheConfiguredReleaseTracksTheFile(t *testing.T) {
	version := "24.21.0"

	configured := newTestWorld(t).
		writeDocument(`{"nodeVersion": "24.19.0"}`).
		withVariable(paths.EnvNodeVersion, "24.18.0")
	if settings := configured.load(Overrides{NodeVersion: &version}); settings.ConfiguredNodeVersion != "24.19.0" {
		t.Fatalf("ConfiguredNodeVersion = %q, want the file's value", settings.ConfiguredNodeVersion)
	}

	silent := newTestWorld(t).withVariable(paths.EnvNodeVersion, "24.18.0")
	if settings := silent.load(Overrides{NodeVersion: &version}); settings.ConfiguredNodeVersion != "" {
		t.Fatalf("ConfiguredNodeVersion = %q, want empty: the file names none", settings.ConfiguredNodeVersion)
	}
}

// writeBackRows is the V section of the decision table.
var writeBackRows = map[string]func(*testing.T){
	"V1":  testWriteBackV1CreatesAMissingDocument,
	"V2":  testWriteBackV2AddsTheKeyToAnEmptyDocument,
	"V3":  testWriteBackV3PreservesEveryOtherField,
	"V4":  testWriteBackV4KeepsADecidedRelease,
	"V5":  testWriteBackV5CreatesTheStateDirectory,
	"V6":  testWriteBackV6RefusesAnEmptyVersion,
	"V7":  testWriteBackV7ReportsADocumentItCannotRead,
	"V8":  testWriteBackV8ReportsAnOversizedDocument,
	"V9":  testWriteBackV9ReportsAStateDirectoryItCannotCreate,
	"V10": testWriteBackV10SurvivesALoad,
	"V11": testWriteBackV11ReportsAMissingPlatformHome,
}

// TestWriteBackMatrix runs every row of the write-back table.
func TestWriteBackMatrix(t *testing.T) {
	for id, run := range writeBackRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testWriteBackV1CreatesAMissingDocument pins the first run of a fresh
// installation: there is no settings document, so recording the release creates
// one that holds the built-in defaults plus the release.
func testWriteBackV1CreatesAMissingDocument(t *testing.T) {
	w := newTestWorld(t)
	if _, err := w.settings().RecordRuntime("", "24.20.0"); err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	fields := w.fields()
	if fields["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want the release recorded", fields)
	}
	if fields["port"] != float64(DefaultPort) {
		t.Fatalf("document = %v, want the built-in defaults alongside it", fields)
	}
	settings := w.load(Overrides{})
	if settings.NodeVersion != "24.20.0" || settings.Sources.NodeVersion != "file" {
		t.Fatalf("settings = %+v, want the recorded release read back from the file", settings)
	}
}

// testWriteBackV2AddsTheKeyToAnEmptyDocument pins that an empty document — which
// means "everything at its default" — stays that way: only the release is
// written, no settings are invented.
func testWriteBackV2AddsTheKeyToAnEmptyDocument(t *testing.T) {
	w := newTestWorld(t).writeDocument("")
	if _, err := w.settings().RecordRuntime("", "24.20.0"); err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	fields := w.fields()
	if len(fields) != 1 || fields["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want only the release", fields)
	}
	if settings := w.load(Overrides{}); settings.Port != DefaultPort || settings.NodeVersion != "24.20.0" {
		t.Fatalf("settings = %+v, want the defaults plus the release", settings)
	}
}

// testWriteBackV3PreservesEveryOtherField pins that the write-back is a change to
// one key, not a rewrite of the file: everything the operator edited survives,
// byte for byte.
func testWriteBackV3PreservesEveryOtherField(t *testing.T) {
	document := fmt.Sprintf(`{
  "repoDir": %q,
  "port": 3999,
  "startTimeoutSeconds": 12,
  "stopTimeoutSeconds": 13,
  "lockTimeoutSeconds": 14,
  "logRotateBytes": 131072
}`, filepath.Join(fixtureHome(), "custom-checkout"))
	w := newTestWorld(t).writeDocument(document)
	before := w.fields()

	if _, err := w.settings().RecordRuntime("", "24.20.0"); err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	after := w.fields()
	if after["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want the release recorded", after)
	}
	for key, want := range before {
		if after[key] != want {
			t.Fatalf("field %q = %v, want %v: the write-back must not touch it", key, after[key], want)
		}
	}
	if len(after) != len(before)+1 {
		t.Fatalf("document = %v, want exactly one key added", after)
	}
}

// testWriteBackV4KeepsADecidedRelease pins where the write-back rule lives.
//
// Recording is the only write dshctl makes on its own initiative, so the rule
// that protects the operator's document — a decided key is never replaced — is
// enforced by the writer itself rather than by its callers. A rule that lives
// only at the call sites is the kind that gets forgotten when a new call site
// appears, and what it protects here is a document the operator wrote: silently
// replacing it is the one outcome no caller should be able to cause by accident.
// The caller-side half of the rule (a start that must not even try) is pinned in
// internal/app.
func testWriteBackV4KeepsADecidedRelease(t *testing.T) {
	w := newTestWorld(t).writeDocument(`{"nodeVersion": "24.19.0"}`)
	wrote, err := w.settings().RecordRuntime("", "24.21.0")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if wrote.NodeVersion {
		t.Fatalf("wrote = %+v, want it to report that nothing was written", wrote)
	}
	if got := w.load(Overrides{}).NodeVersion; got != "24.19.0" {
		t.Fatalf("NodeVersion = %q, want the release the operator wrote", got)
	}
}

// testWriteBackV5CreatesTheStateDirectory pins that recording does not depend on
// something else having provisioned the state directory first.
func testWriteBackV5CreatesTheStateDirectory(t *testing.T) {
	w := newTestWorld(t)
	if err := os.RemoveAll(w.stateDir); err != nil {
		t.Fatalf("remove the state directory: %v", err)
	}
	if _, err := w.settings().RecordRuntime("", "24.20.0"); err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if _, err := os.Stat(w.configPath); err != nil {
		t.Fatalf("the settings document must exist: %v", err)
	}
}

// testWriteBackV6RefusesAnEmptyVersion pins the guard: an empty release is a
// state the configuration must never hold, because it would mean "discover
// again" written down as if it were a decision.
func testWriteBackV6RefusesAnEmptyVersion(t *testing.T) {
	w := newTestWorld(t)
	for _, version := range []string{"", "   "} {
		if _, err := w.settings().RecordRuntime("", version); err == nil {
			t.Fatalf("RecordRuntime with %q succeeded, want a refusal", version)
		}
	}
	if _, err := os.Stat(w.configPath); !os.IsNotExist(err) {
		t.Fatalf("a refused write must leave no document behind: %v", err)
	}
}

// testWriteBackV7ReportsADocumentItCannotRead pins that a settings path holding
// something that is not a document is reported rather than overwritten: the file
// belongs to the operator.
func testWriteBackV7ReportsADocumentItCannotRead(t *testing.T) {
	w := newTestWorld(t)
	if err := os.MkdirAll(w.configPath, 0o700); err != nil {
		t.Fatalf("mkdir at the settings path: %v", err)
	}
	if _, err := w.settings().RecordRuntime("", "24.20.0"); err == nil {
		t.Fatal("a directory at the settings path must be reported")
	}
}

// testWriteBackV8ReportsAnOversizedDocument pins the same boundary the loader
// applies: a file too large to be a settings document is not silently replaced.
func testWriteBackV8ReportsAnOversizedDocument(t *testing.T) {
	w := newTestWorld(t).writeDocument(strings.Repeat(" ", maxConfigBytes+1))
	if _, err := w.settings().RecordRuntime("", "24.20.0"); err == nil {
		t.Fatal("an oversized document must be reported")
	}
}

// testWriteBackV9ReportsAStateDirectoryItCannotCreate pins the write failure:
// a path that cannot become a directory is reported instead of being ignored, so
// a start can warn about it. The document itself is placed elsewhere, because a
// state directory that cannot be created must be the failure under test.
func testWriteBackV9ReportsAStateDirectoryItCannotCreate(t *testing.T) {
	w := newTestWorld(t)
	if err := os.RemoveAll(w.stateDir); err != nil {
		t.Fatalf("remove the state directory: %v", err)
	}
	if err := os.WriteFile(w.stateDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write a file at the state directory path: %v", err)
	}
	settings := w.settings()
	settings.ConfigPath = filepath.Join(w.root, "elsewhere", "config.json")
	if _, err := settings.RecordRuntime("", "24.20.0"); err == nil {
		t.Fatal("a state directory that cannot be created must be reported")
	}
}

// testWriteBackV11ReportsAMissingPlatformHome pins the last fallback: with no
// document and no home directory to render a first one from, the write is
// reported rather than done somewhere arbitrary.
//
// The settings carry the home they were resolved with, so the check is on the
// settings rather than on the environment now: a home that disappeared after the
// command resolved its settings says nothing about whether this run had one, and
// treating it as absent would refuse a write the operator's own machine supports.
func testWriteBackV11ReportsAMissingPlatformHome(t *testing.T) {
	w := newTestWorld(t)
	settings := w.settings()
	settings.Home = ""
	settings.RepoDir = ""

	if _, err := settings.RecordRuntime("", "24.20.0"); err == nil {
		t.Fatal("with no settings document and no home directory the write must fail")
	}
}

// testWriteBackV10SurvivesALoad pins the round trip for a spread of
// configurations: what is recorded is what the next load reports, and nothing
// else moves.
func testWriteBackV10SurvivesALoad(t *testing.T) {
	documents := []string{
		`{}`,
		`{"port": 3999}`,
		fmt.Sprintf(`{"repoDir": %q, "port": 3999, "logRotateBytes": 131072}`, filepath.Join(fixtureHome(), "checkout")),
		`{"startTimeoutSeconds": 12, "stopTimeoutSeconds": 13, "lockTimeoutSeconds": 14}`,
	}
	for _, document := range documents {
		w := newTestWorld(t).writeDocument(document)
		before := w.load(Overrides{})
		if _, err := before.RecordRuntime("", "24.20.0"); err != nil {
			t.Fatalf("RecordRuntime with %s: %v", document, err)
		}
		after := w.load(Overrides{})
		if after.NodeVersion != "24.20.0" || after.ConfiguredNodeVersion != "24.20.0" {
			t.Fatalf("document %s reloaded as %+v, want the recorded release", document, after)
		}
		if after.Port != before.Port || after.RepoDir != before.RepoDir ||
			after.StartTimeout != before.StartTimeout || after.StopTimeout != before.StopTimeout ||
			after.LockTimeout != before.LockTimeout || after.LogRotateBytes != before.LogRotateBytes {
			t.Fatalf("document %s reloaded as %+v, want every other setting unchanged (%+v)", document, after, before)
		}
	}
}

// TestNodeVersionTablesAreComplete fails when a decision-table row has no case.
func TestNodeVersionTablesAreComplete(t *testing.T) {
	assertTableRows(t, "P", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}, precedenceRows)
	assertTableRows(t, "V", []string{"V1", "V2", "V3", "V4", "V5", "V6", "V7", "V8", "V9", "V10", "V11"}, writeBackRows)
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

// TestTheNodeMinimumIsNotConfigurable pins the decision that the floor is code,
// not configuration: no document key, no environment variable and no field of
// the settings can move it, so a machine cannot be talked into a runtime dshctl
// refuses.
func TestTheNodeMinimumIsNotConfigurable(t *testing.T) {
	// A document that tries to set it is rejected as unknown.
	w := newTestWorld(t).writeDocument(`{"minNodeVersion": "1.0.0"}`)
	if _, err := Load(w.vars.Getenv, Overrides{}); err == nil {
		t.Fatal("a document naming the minimum must be rejected")
	} else if !strings.Contains(err.Error(), "minNodeVersion") {
		t.Fatalf("error = %v, want it to name the unknown field", err)
	}

	// An environment that tries to set it changes nothing.
	w = newTestWorld(t).
		withVariable("DSH_MIN_NODE_VERSION", "1.0.0").
		withVariable("DSHCTL_MIN_NODE_VERSION", "1.0.0")
	if settings := w.load(Overrides{}); settings.NodeVersion != "" {
		t.Fatalf("settings = %+v, want the environment to have no effect", settings)
	}

	// No field of the settings, the document or the overrides carries it.
	for _, value := range []any{File{}, Settings{}, Overrides{}, Sources{}} {
		kind := reflect.TypeOf(value)
		for index := 0; index < kind.NumField(); index++ {
			field := kind.Field(index)
			name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
			if strings.Contains(name, "min") {
				t.Fatalf("%s.%s carries the minimum, which must stay a constant", kind.Name(), field.Name)
			}
		}
	}
}

// TestNodeVersionConstantsAreFixed pins the two releases dshctl commits to: the
// floor it refuses below, and the release its judgments are verified against.
func TestNodeVersionConstantsAreFixed(t *testing.T) {
	if MinNodeVersion != "24.12.0" {
		t.Fatalf("MinNodeVersion = %q, want the pinned floor", MinNodeVersion)
	}
	if TestedNodeVersion != "24.20.0" {
		t.Fatalf("TestedNodeVersion = %q, want the pinned verified release", TestedNodeVersion)
	}
}

// TestEncodeWritesTheReleaseOnlyWhenItIsDetermined pins the generated document:
// a fresh installation records nothing about a runtime it has not chosen yet,
// and a determined one records the release it runs.
func TestEncodeWritesTheReleaseOnlyWhenItIsDetermined(t *testing.T) {
	undetermined, err := Encode(Default(fixtureHome()))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(undetermined), "nodeVersion") {
		t.Fatalf("an undetermined release was written to the document:\n%s", undetermined)
	}

	settings := Default(fixtureHome())
	settings.NodeVersion = "24.20.0"
	determined, err := Encode(settings)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(determined, &document); err != nil {
		t.Fatalf("Encode produced invalid JSON: %v\n%s", err, determined)
	}
	if document["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want the release recorded", document)
	}
}

// TestDescribeSaysWhenTheReleaseIsUndetermined pins the verbose line in both
// states: what the next start will do about a release that is not determined
// yet, and the release itself once it is.
func TestDescribeSaysWhenTheReleaseIsUndetermined(t *testing.T) {
	settings := Default(fixtureHome())
	settings.StateDir = "/state"
	settings.ConfigPath = "/state/config.json"
	settings.LogPath = "/state/dsh-web.log"

	undetermined := strings.Join(settings.Describe(), "\n")
	if !strings.Contains(undetermined, "(未确定") {
		t.Fatalf("verbose output =\n%s\nwant it to say the release is undetermined", undetermined)
	}

	settings.NodeVersion = "24.20.0"
	settings.Sources.NodeVersion = "file"
	determined := strings.Join(settings.Describe(), "\n")
	if !strings.Contains(determined, "Node 版本: 24.20.0 (file)") {
		t.Fatalf("verbose output =\n%s\nwant the release and its source", determined)
	}
}

// TestLoadWithoutAPlatformHomeIsReported pins the boundary of the loader: it
// needs a home directory to name the default checkout, and a machine that cannot
// provide one is a failure rather than a configuration that points at the
// working directory.
func TestLoadWithoutAPlatformHomeIsReported(t *testing.T) {
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if _, err := Load(env{}.Getenv, Overrides{}); err == nil {
		t.Fatal("Load without a home directory must fail")
	}
}
