package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// The checkout is the one setting whose built-in default is a guess about this
// machine rather than a value that means the same thing everywhere. Two rules
// follow from that, and the tables below are their decision rows:
//
//   - R (resolution): a document that merely repeats the guess decides nothing,
//     so `--repo`/DSH_REPO_DIR still win and the next successful run can still
//     record the checkout that ran. A document that names any other path is a
//     decision and is honoured as one.
//   - W (write-back): a successful run records the checkout it used, and never
//     replaces a checkout the document decided.
//
// Both exist because a *wrong* guess written into the document is
// indistinguishable from an operator's choice, and the file layer beats the
// built-in default: it could never be corrected by anything but hand-editing,
// which is how `dshctl --repo /real/checkout start` ended up leaving every later
// command — doctor, update, a plain start — on a directory that did not exist.
//
// The world is shared with the Node tables (config_nodeversion_test.go): one
// state directory, one home directory, one settings document, one environment.

// resolutionRows is the R section of the decision table.
var resolutionRows = map[string]func(*testing.T){
	"R1":  testResolutionR1ADecidedCheckoutComesFromTheFile,
	"R2":  testResolutionR2ARepeatedGuessIsNotADecision,
	"R3":  testResolutionR3TheRepeatedGuessIsComparedAfterResolution,
	"R4":  testResolutionR4AFlagBeatsTheRepeatedGuess,
	"R5":  testResolutionR5TheEnvironmentBeatsTheRepeatedGuess,
	"R6":  testResolutionR6AnOverrideKeepsTheDecidedCheckout,
	"R7":  testResolutionR7NullAndAbsentMeanTheDefault,
	"R8":  testResolutionR8ARelativeCheckoutIsAUsageError,
	"R9":  testResolutionR9TheGuessFollowsThePlatformHome,
	"R10": testResolutionR10TheDocumentStillFollowsTheDefault,
	"R11": testResolutionR11ProvisionDecidesNoCheckout,
	"R12": testResolutionR12ProvisionFreezesNoOverride,
}

// TestRepodirResolutionMatrix runs every row of the resolution table.
func TestRepodirResolutionMatrix(t *testing.T) {
	for id, run := range resolutionRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testResolutionR1ADecidedCheckoutComesFromTheFile pins the ordinary case: a
// path that is nobody's default is the operator's choice, and it is what the
// settings and the write-back gate report.
func testResolutionR1ADecidedCheckoutComesFromTheFile(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.checkout()) + `}`)
	settings := w.load(Overrides{})
	if settings.RepoDir != w.checkout() {
		t.Fatalf("RepoDir = %q, want the checkout the document names", settings.RepoDir)
	}
	if settings.ConfiguredRepoDir != w.checkout() {
		t.Fatalf("ConfiguredRepoDir = %q, want the document's decision", settings.ConfiguredRepoDir)
	}
	if settings.Sources.RepoDir != "file" {
		t.Fatalf("source = %q, want file", settings.Sources.RepoDir)
	}
}

// testResolutionR2ARepeatedGuessIsNotADecision is the rule that repairs a
// document an older build wrote: the path is the built-in default spelled out,
// so the default layer is still in charge — the value is the same either way,
// but the write-back gate stays open and the source says what happened.
func testResolutionR2ARepeatedGuessIsNotADecision(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.guess()) + `}`)
	settings := w.load(Overrides{})
	if settings.RepoDir != w.guess() {
		t.Fatalf("RepoDir = %q, want the built-in default %q", settings.RepoDir, w.guess())
	}
	if settings.ConfiguredRepoDir != "" {
		t.Fatalf("ConfiguredRepoDir = %q, want empty: the document repeated the default", settings.ConfiguredRepoDir)
	}
	if settings.Sources.RepoDir != SourceRepoDirRepeatsDefault {
		t.Fatalf("source = %q, want %q", settings.Sources.RepoDir, SourceRepoDirRepeatsDefault)
	}
}

// testResolutionR3TheRepeatedGuessIsComparedAfterResolution pins that the
// comparison happens on resolved paths: a document that spells the guess with a
// leading ~, or with redundant separators, names the same directory, and a
// comparison of raw strings would read it as a decision and pin the guess for
// good.
func testResolutionR3TheRepeatedGuessIsComparedAfterResolution(t *testing.T) {
	w := newTestWorld(t)
	separator := string(filepath.Separator)
	spellings := []string{
		"~/deepseek-harness",
		filepath.Join(w.guess(), "."),
		strings.ReplaceAll(w.guess(), separator, separator+separator),
	}
	for _, spelling := range spellings {
		w.writeDocument(`{"repoDir": ` + quote(spelling) + `}`)
		settings := w.load(Overrides{})
		if settings.RepoDir != w.guess() {
			t.Fatalf("%q resolved to %q, want the default %q", spelling, settings.RepoDir, w.guess())
		}
		if settings.ConfiguredRepoDir != "" {
			t.Fatalf("%q: ConfiguredRepoDir = %q, want empty: it resolves to the default",
				spelling, settings.ConfiguredRepoDir)
		}
		if settings.Sources.RepoDir != SourceRepoDirRepeatsDefault {
			t.Fatalf("%q: source = %q, want %q", spelling, settings.Sources.RepoDir, SourceRepoDirRepeatsDefault)
		}
	}
}

// testResolutionR4AFlagBeatsTheRepeatedGuess pins that the top of the order is
// untouched by the rule: a repeated guess is only ever *not* a decision, it is
// never a value that outranks a flag.
func testResolutionR4AFlagBeatsTheRepeatedGuess(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.guess()) + `}`)
	flag := filepath.Join(w.home, "from-flag")
	settings := w.load(Overrides{RepoDir: &flag})
	if settings.RepoDir != flag {
		t.Fatalf("RepoDir = %q, want the flag's checkout", settings.RepoDir)
	}
	if settings.Sources.RepoDir != "flag" {
		t.Fatalf("source = %q, want flag", settings.Sources.RepoDir)
	}
	if settings.ConfiguredRepoDir != "" {
		t.Fatalf("ConfiguredRepoDir = %q, want empty: the document still decides nothing", settings.ConfiguredRepoDir)
	}
}

// testResolutionR5TheEnvironmentBeatsTheRepeatedGuess pins the same for the
// environment, which is the layer a worker shell carries invisibly.
func testResolutionR5TheEnvironmentBeatsTheRepeatedGuess(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.guess()) + `}`)
	w.withVariable(paths.EnvRepoDir, w.checkout())
	settings := w.load(Overrides{})
	if settings.RepoDir != w.checkout() || settings.Sources.RepoDir != "env" {
		t.Fatalf("settings = %+v, want the environment's checkout", settings)
	}
	if settings.ConfiguredRepoDir != "" {
		t.Fatalf("ConfiguredRepoDir = %q, want empty: the document still decides nothing", settings.ConfiguredRepoDir)
	}
}

// testResolutionR6AnOverrideKeepsTheDecidedCheckout pins the half that protects
// the operator: an override changes this run, never what the document decided.
func testResolutionR6AnOverrideKeepsTheDecidedCheckout(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.checkout()) + `}`)
	w.withVariable(paths.EnvRepoDir, filepath.Join(w.home, "elsewhere"))
	settings := w.load(Overrides{})
	if settings.RepoDir != filepath.Join(w.home, "elsewhere") || settings.Sources.RepoDir != "env" {
		t.Fatalf("settings = %+v, want the environment's checkout", settings)
	}
	if settings.ConfiguredRepoDir != w.checkout() {
		t.Fatalf("ConfiguredRepoDir = %q, want the document's own checkout %q",
			settings.ConfiguredRepoDir, w.checkout())
	}
}

// testResolutionR7NullAndAbsentMeanTheDefault pins that the rule adds no new
// spelling: an explicit null, a missing key and an empty document all mean
// "nothing decided".
func testResolutionR7NullAndAbsentMeanTheDefault(t *testing.T) {
	for _, document := range []string{`{}`, `{"repoDir": null}`, ``, "  \n"} {
		w := newTestWorld(t)
		w.writeDocument(document)
		settings := w.load(Overrides{})
		if settings.RepoDir != w.guess() || settings.ConfiguredRepoDir != "" {
			t.Fatalf("document %q resolved to %+v, want the built-in default and no decision", document, settings)
		}
		if settings.Sources.RepoDir != "default" {
			t.Fatalf("document %q: source = %q, want default", document, settings.Sources.RepoDir)
		}
	}
}

// testResolutionR8ARelativeCheckoutIsAUsageError pins that the new rule did not
// loosen path validation: a relative path is still refused with the field named,
// so a typo fails at load rather than in whichever directory the next command
// happens to run from.
func testResolutionR8ARelativeCheckoutIsAUsageError(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": "relative/deepseek-harness"}`)
	_, err := Load(w.vars.Getenv, Overrides{})
	if err == nil {
		t.Fatal("a relative repoDir must be refused")
	}
	if !strings.Contains(err.Error(), "repoDir") {
		t.Fatalf("error = %v, want it to name the field", err)
	}
}

// testResolutionR9TheGuessFollowsThePlatformHome pins where the guess comes
// from: the operating system's home, not $DSH_HOME. The harness home holds
// dshctl's own state, so a checkout placed there would be found only by
// accident, and a guess derived from it would be wrong on every machine that
// keeps its state elsewhere.
func testResolutionR9TheGuessFollowsThePlatformHome(t *testing.T) {
	w := newTestWorld(t)
	// The world redirects the platform home into its own root, so this is the
	// home every later resolution sees.
	home, err := paths.Home()
	if err != nil {
		t.Skipf("no platform home: %v", err)
	}
	w.withVariable(paths.EnvHarnessHome, filepath.Join(w.home, "elsewhere-home"))
	settings := w.load(Overrides{})
	if settings.RepoDir != filepath.Join(home, DefaultRepoDirName) {
		t.Fatalf("RepoDir = %q, want the checkout under the platform home %q", settings.RepoDir, home)
	}
	if settings.Sources.RepoDir != "default" {
		t.Fatalf("source = %q, want default", settings.Sources.RepoDir)
	}
}

// testResolutionR10TheDocumentStillFollowsTheDefault pins the other half of the
// rule: a document that repeats the guess keeps following the default, so the
// guess is not pinned into the settings either. And because the document decides
// nothing, the write-back gate is open — which is what makes the rule able to
// repair a machine whose layout was guessed wrong.
func testResolutionR10TheDocumentStillFollowsTheDefault(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.guess()) + `}`)
	if err := os.MkdirAll(w.guess(), 0o700); err != nil {
		t.Fatalf("mkdir the guessed checkout: %v", err)
	}
	settings := w.load(Overrides{})
	if settings.RepoDir != w.guess() {
		t.Fatalf("RepoDir = %q, want the guess %q", settings.RepoDir, w.guess())
	}
	wrote, err := w.settings().RecordRuntime(w.checkout(), "")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if !wrote.RepoDir {
		t.Fatalf("wrote = %+v, want the checkout recorded: the document decided nothing", wrote)
	}
}

// testResolutionR11ProvisionDecidesNoCheckout pins the document a first run
// writes: every tunable default is recorded — the file is how the surface is
// discovered — except the one value that is a guess about this machine.
//
// Writing the guess down is what made the reported bug permanent: the document
// is the layer that beats the default, so a guess recorded there is honoured
// forever, and nothing in the file says the path was never chosen.
func testResolutionR11ProvisionDecidesNoCheckout(t *testing.T) {
	w := newTestWorld(t)
	settings := w.load(Overrides{})
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	fields := w.fields()
	if _, present := fields["repoDir"]; present {
		t.Fatalf("document = %v, want no repoDir: a machine-derived guess is not a decision", fields)
	}
	if _, present := fields["nodeVersion"]; present {
		t.Fatalf("document = %v, want no nodeVersion while it is undetermined", fields)
	}
	for _, key := range []string{"port", "startTimeoutSeconds", "stopTimeoutSeconds", "lockTimeoutSeconds", "logRotateBytes"} {
		if _, present := fields[key]; !present {
			t.Fatalf("document = %v, want it to record %q", fields, key)
		}
	}
	reloaded := w.load(Overrides{})
	if reloaded.RepoDir != w.guess() || reloaded.Sources.RepoDir != "default" {
		t.Fatalf("reloaded = %+v, want the default checkout after provisioning", reloaded)
	}
	if reloaded.ConfiguredRepoDir != "" {
		t.Fatalf("ConfiguredRepoDir = %q, want empty", reloaded.ConfiguredRepoDir)
	}
}

// testResolutionR12ProvisionFreezesNoOverride pins the promise the provisioning
// comment makes: a document generated while an override is in effect records the
// built-in defaults, never the values this run happened to use. A frozen
// override is the same defect as a frozen guess — a one-off decision becomes
// permanent, and the layer that was supposed to be a default silently wins over
// the environment.
func testResolutionR12ProvisionFreezesNoOverride(t *testing.T) {
	w := newTestWorld(t)
	w.withVariable(paths.EnvRepoDir, w.checkout())
	w.withVariable(paths.EnvPort, "4444")
	w.withVariable(paths.EnvNodeVersion, "24.21.0")
	flag := filepath.Join(w.home, "from-flag")
	settings := w.load(Overrides{RepoDir: &flag})
	if settings.RepoDir != flag {
		t.Fatalf("RepoDir = %q, want the flag's checkout", settings.RepoDir)
	}
	if err := settings.Provision(); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	document := w.document()
	for _, frozen := range []string{w.checkout(), flag, "4444", "24.21.0"} {
		if strings.Contains(document, frozen) {
			t.Fatalf("the provisioned document froze %q:\n%s", frozen, document)
		}
	}
}

// writeBackRepoRows is the W section of the checkout decision table.
var writeBackRepoRows = map[string]func(*testing.T){
	"W1":  testRepodirW1RecordsTheCheckoutWhenTheDocumentDecidesNone,
	"W2":  testRepodirW2KeepsADecidedCheckout,
	"W3":  testRepodirW3WritesBothFactsInOneWrite,
	"W4":  testRepodirW4EachKeyStandsAlone,
	"W5":  testRepodirW5RefusesNothingToRecord,
	"W6":  testRepodirW6RefusesAnUnusablePath,
	"W7":  testRepodirW7ReportsADocumentItCannotRead,
	"W8":  testRepodirW8SurvivesALoad,
	"W9":  testRepodirW9LeavesTheKeyAloneWithoutAPlatformHome,
	"W10": testRepodirW10TheDocumentStaysValidAndPrivate,
	"W11": testRepodirW11LeavesAnUnreadableCheckoutAlone,
}

// TestRepodirWriteBackMatrix runs every row of the write-back table.
func TestRepodirWriteBackMatrix(t *testing.T) {
	for id, run := range writeBackRepoRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// testRepodirW1RecordsTheCheckoutWhenTheDocumentDecidesNone pins the write-back
// for every shape of "nothing decided": no document, an empty one, one that
// holds other settings, and one that carries a copy of the guess. Each of them
// has to end up naming the checkout that ran, or the next command reads a path
// nobody chose.
func testRepodirW1RecordsTheCheckoutWhenTheDocumentDecidesNone(t *testing.T) {
	shapes := map[string]func(w *testWorld) string{
		"no document":            func(*testWorld) string { return "" },
		"an empty document":      func(*testWorld) string { return "" },
		"a whitespace file":      func(*testWorld) string { return "  \n" },
		"an empty object":        func(*testWorld) string { return `{}` },
		"another setting":        func(*testWorld) string { return `{"port": 3999}` },
		"every other setting":    func(*testWorld) string { return `{"port": 3999, "startTimeoutSeconds": 12, "logRotateBytes": 131072}` },
		"a copy of the guess":    func(w *testWorld) string { return `{"repoDir": ` + quote(w.guess()) + `}` },
		"a null checkout":        func(*testWorld) string { return `{"repoDir": null, "port": 3999}` },
		"a tilde copy of guess":  func(*testWorld) string { return `{"repoDir": "~/deepseek-harness"}` },
		"a stop timeout setting": func(*testWorld) string { return `{"stopTimeoutSeconds": 13}` },
		"a lock timeout setting": func(*testWorld) string { return `{"lockTimeoutSeconds": 14}` },
	}
	for name, render := range shapes {
		t.Run(name, func(t *testing.T) {
			w := newTestWorld(t)
			document := render(w)
			if strings.TrimSpace(document) != "" {
				w.writeDocument(document)
			}
			before := map[string]any{}
			if _, err := os.Stat(w.configPath); err == nil {
				before = w.fields()
			}
			wrote, err := w.settings().RecordRuntime(w.checkout(), "")
			if err != nil {
				t.Fatalf("RecordRuntime: %v", err)
			}
			if !wrote.RepoDir || wrote.NodeVersion {
				t.Fatalf("wrote = %+v, want the checkout alone", wrote)
			}
			if got := w.fields()["repoDir"]; got != w.checkout() {
				t.Fatalf("document = %v, want repoDir %q", w.fields(), w.checkout())
			}
			// Nothing the operator wrote may move.
			for key, want := range before {
				if key == "repoDir" {
					continue
				}
				if w.fields()[key] != want {
					t.Fatalf("field %q = %v, want %v: the write-back must not touch it",
						key, w.fields()[key], want)
				}
			}
		})
	}
}

// testRepodirW2KeepsADecidedCheckout pins the limit of the write-back: a
// document that names its own checkout is the operator's, and a run from another
// tree must not rewrite it. The disagreement is reported instead, which is the
// caller's job (internal/service).
func testRepodirW2KeepsADecidedCheckout(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.checkout()) + `, "port": 3999}`)
	wrote, err := w.settings().RecordRuntime(filepath.Join(w.home, "elsewhere"), "")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if wrote.RepoDir {
		t.Fatalf("wrote = %+v, want nothing written", wrote)
	}
	if got := w.document(); !strings.Contains(got, w.checkout()) {
		t.Fatalf("document = %s, want it untouched", got)
	}
}

// testRepodirW3WritesBothFactsInOneWrite pins that a start records the checkout
// and the release together: one document, one write, every other key preserved.
func testRepodirW3WritesBothFactsInOneWrite(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"port": 3999, "logRotateBytes": 131072}`)
	before := w.fields()
	wrote, err := w.settings().RecordRuntime(w.checkout(), "24.20.0")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if !wrote.RepoDir || !wrote.NodeVersion {
		t.Fatalf("wrote = %+v, want both facts", wrote)
	}
	after := w.fields()
	if after["repoDir"] != w.checkout() || after["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want both facts recorded", after)
	}
	if len(after) != len(before)+2 {
		t.Fatalf("document = %v, want exactly two keys added to %v", after, before)
	}
	for key, want := range before {
		if after[key] != want {
			t.Fatalf("field %q = %v, want %v", key, after[key], want)
		}
	}
}

// testRepodirW4EachKeyStandsAlone pins the two single-key calls: a build records
// the checkout without touching the release, and a start whose document already
// decided a checkout records the release without touching the checkout.
func testRepodirW4EachKeyStandsAlone(t *testing.T) {
	w := newTestWorld(t)
	wrote, err := w.settings().RecordRuntime(w.checkout(), "")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if !wrote.RepoDir || wrote.NodeVersion {
		t.Fatalf("wrote = %+v, want the checkout alone", wrote)
	}
	wrote, err = w.settings().RecordRuntime("", "24.20.0")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if wrote.RepoDir || !wrote.NodeVersion {
		t.Fatalf("wrote = %+v, want the release alone", wrote)
	}
	fields := w.fields()
	if fields["repoDir"] != w.checkout() || fields["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want both facts", fields)
	}
}

// testRepodirW5RefusesNothingToRecord pins the guard: a call that names neither
// fact is a mistake at the call site, and it must not create or rewrite a
// document to say nothing.
func testRepodirW5RefusesNothingToRecord(t *testing.T) {
	w := newTestWorld(t)
	for _, values := range [][2]string{{"", ""}, {"", "   "}, {"  ", ""}} {
		wrote, err := w.settings().RecordRuntime(values[0], values[1])
		if err == nil {
			t.Fatalf("RecordRuntime(%q, %q) succeeded, want a refusal", values[0], values[1])
		}
		if wrote.RepoDir || wrote.NodeVersion {
			t.Fatalf("wrote = %+v, want nothing written", wrote)
		}
	}
	if _, err := os.Stat(w.configPath); !os.IsNotExist(err) {
		t.Fatalf("a refused write must leave no document behind: %v", err)
	}
}

// testRepodirW6RefusesAnUnusablePath pins that the write-back cannot poison the
// document: a relative checkout would fail the next load, which is a worse
// failure than not recording it. The document that decided nothing is left
// alone, and the operator's file is not the place to discover a caller's bug.
func testRepodirW6RefusesAnUnusablePath(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"port": 3999}`)
	for _, candidate := range []string{"relative/repo", "   ./repo   "} {
		if _, err := w.settings().RecordRuntime(candidate, ""); err == nil {
			t.Fatalf("RecordRuntime(%q) succeeded, want a refusal", candidate)
		}
	}
	fields := w.fields()
	if _, present := fields["repoDir"]; present {
		t.Fatalf("document = %v, want no repoDir", fields)
	}
	if fields["port"] != float64(3999) {
		t.Fatalf("document = %v, want the operator's setting preserved", fields)
	}
}

// testRepodirW7ReportsADocumentItCannotRead pins that the same boundaries the
// loader applies are applied before a write: a directory at the settings path or
// an oversized file is reported, never replaced.
func testRepodirW7ReportsADocumentItCannotRead(t *testing.T) {
	directory := newTestWorld(t)
	if err := os.MkdirAll(directory.configPath, 0o700); err != nil {
		t.Fatalf("mkdir at the settings path: %v", err)
	}
	if _, err := directory.settings().RecordRuntime(directory.checkout(), "24.20.0"); err == nil {
		t.Fatal("a directory at the settings path must be reported")
	}

	oversized := newTestWorld(t)
	oversized.writeDocument(strings.Repeat(" ", maxConfigBytes+1))
	if _, err := oversized.settings().RecordRuntime(oversized.checkout(), "24.20.0"); err == nil {
		t.Fatal("an oversized document must be reported")
	}
}

// testRepodirW8SurvivesALoad is the invariant the whole rule exists for: once a
// run has been recorded, the next command resolves the same checkout from the
// document — with the source saying the file decided it.
func testRepodirW8SurvivesALoad(t *testing.T) {
	shapes := map[string]func(w *testWorld) string{
		"no document":       func(*testWorld) string { return "" },
		"the guess written": func(w *testWorld) string { return `{"repoDir": ` + quote(w.guess()) + `}` },
		"another setting":   func(*testWorld) string { return `{"port": 3999}` },
	}
	for name, render := range shapes {
		t.Run(name, func(t *testing.T) {
			w := newTestWorld(t)
			if document := render(w); strings.TrimSpace(document) != "" {
				w.writeDocument(document)
			}
			before := w.load(Overrides{})
			if err := before.Provision(); err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if _, err := w.settings().RecordRuntime(w.checkout(), "24.20.0"); err != nil {
				t.Fatalf("RecordRuntime: %v", err)
			}
			after := w.load(Overrides{})
			if after.RepoDir != w.checkout() {
				t.Fatalf("RepoDir = %q, want the recorded checkout %q", after.RepoDir, w.checkout())
			}
			if after.ConfiguredRepoDir != w.checkout() || after.Sources.RepoDir != "file" {
				t.Fatalf("settings = %+v, want the checkout read back from the file", after)
			}
			if after.Port != before.Port || after.StartTimeout != before.StartTimeout ||
				after.LogRotateBytes != before.LogRotateBytes {
				t.Fatalf("reloaded = %+v, want every other setting unchanged (%+v)", after, before)
			}
		})
	}
}

// testRepodirW9LeavesTheKeyAloneWithoutAPlatformHome pins the conservative
// branch: with no platform home the built-in guess cannot be spelled, so a
// document that carries a checkout cannot be recognised as repeating it. The key
// is then left exactly as it is — overwriting it on the chance that it decided
// nothing would rewrite an operator's choice — while the release is still
// recorded, because that decision needs no comparison.
func testRepodirW9LeavesTheKeyAloneWithoutAPlatformHome(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"repoDir": ` + quote(w.checkout()) + `}`)
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	wrote, err := w.settings().RecordRuntime(filepath.Join(w.home, "elsewhere"), "24.20.0")
	if err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	if wrote.RepoDir {
		t.Fatalf("wrote = %+v, want the checkout left alone", wrote)
	}
	if !wrote.NodeVersion {
		t.Fatalf("wrote = %+v, want the release recorded", wrote)
	}
	fields := w.fields()
	if fields["repoDir"] != w.checkout() {
		t.Fatalf("document = %v, want the checkout untouched", fields)
	}
	if fields["nodeVersion"] != "24.20.0" {
		t.Fatalf("document = %v, want the release recorded", fields)
	}
}

// testRepodirW10TheDocumentStaysValidAndPrivate pins the shape of what is left
// on disk: valid JSON, owner-only permissions, no temporary residue, and a
// document the loader accepts.
func testRepodirW10TheDocumentStaysValidAndPrivate(t *testing.T) {
	w := newTestWorld(t)
	w.writeDocument(`{"port": 3999}`)
	// A process killed mid-write leaves a temporary file behind; the next write
	// must collect it rather than leave it in the state directory forever.
	stale := filepath.Join(w.stateDir, "."+ConfigFileName+".tmp123456")
	if err := os.WriteFile(stale, []byte("residue"), 0o600); err != nil {
		t.Fatalf("write residue: %v", err)
	}
	if _, err := w.settings().RecordRuntime(w.checkout(), "24.20.0"); err != nil {
		t.Fatalf("RecordRuntime: %v", err)
	}
	data, err := os.ReadFile(w.configPath)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("the document is not valid JSON:\n%s", data)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info, err := os.Stat(w.configPath); err != nil {
		t.Fatalf("stat: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
	if w.load(Overrides{}).RepoDir != w.checkout() {
		t.Fatal("the written document must load back")
	}
}

// testRepodirW11LeavesAnUnreadableCheckoutAlone pins the conservative reading of
// a value the package cannot interpret: a document whose checkout is relative —
// or otherwise unusable — still counts as having decided one.
//
// Such a document is refused by Load, so no run of this build can be operating
// under it; the writer must not treat "I cannot read this" as "the operator
// decided nothing" and replace what they wrote. The file is theirs, and a
// rewrite is the one thing that cannot be undone.
func testRepodirW11LeavesAnUnreadableCheckoutAlone(t *testing.T) {
	for _, value := range []string{"relative/path", "   ", "~/../sibling"} {
		w := newTestWorld(t)
		w.writeDocument(`{"repoDir": ` + quote(value) + `, "port": 3999}`)
		wrote, err := w.settings().RecordRuntime(w.checkout(), "24.20.0")
		if err != nil {
			t.Fatalf("RecordRuntime with repoDir %q: %v", value, err)
		}
		if wrote.RepoDir {
			t.Fatalf("repoDir %q: wrote = %+v, want the key left alone", value, wrote)
		}
		fields := w.fields()
		if fields["repoDir"] != value {
			t.Fatalf("repoDir %q was rewritten to %v", value, fields["repoDir"])
		}
		// The release is still recorded: its decision needs no comparison.
		if !wrote.NodeVersion || fields["nodeVersion"] != "24.20.0" {
			t.Fatalf("repoDir %q: document = %v, wrote = %+v, want the release recorded", value, fields, wrote)
		}
	}
}

// TestRepodirTablesAreComplete fails when a decision-table row has no case.
func TestRepodirTablesAreComplete(t *testing.T) {
	assertTableRows(t, "R", []string{
		"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10", "R11", "R12",
	}, resolutionRows)
	assertTableRows(t, "W", []string{
		"W1", "W2", "W3", "W4", "W5", "W6", "W7", "W8", "W9", "W10", "W11",
	}, writeBackRepoRows)
}

// quote renders a path as a JSON string.
func quote(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
