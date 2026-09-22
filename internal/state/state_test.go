package state

import (
	"encoding/json"
	"errors"
	"github.com/rhczz/dshctl/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// store returns a store over a fresh file.
func store(t *testing.T) Store[testDoc] {
	t.Helper()
	return testStore(filepath.Join(t.TempDir(), "dsh-web.state.json"))
}

// TestRoundTrip pins that a saved record reads back unchanged.
//
// Every field is compared, not a hand-picked few: a field that is written but
// never read back — or read back into the wrong one — is how a record silently
// loses the fact it exists for. The identity of a running server is more than
// its pid and port: the checkout it serves and the runtime it runs are what let
// a later command describe the instance instead of the configuration.
func TestRoundTrip(t *testing.T) {
	box := store(t)
	record := testDoc{
		PID:         4242,
		StartedAt:   1_700_000_000,
		Port:        3080,
		URL:         "http://127.0.0.1:3080/?token=abc",
		SpawnedPID:  4241,
		NodeVersion: "24.20.0",
		NodePath:    "/opt/node/bin/node",
		RepoDir:     "/srv/deepseek-harness",
		Phase:       testPhaseRunning,
	}
	if err := box.Save(record); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load reported no record")
	}
	record.UpdatedAt = loaded.UpdatedAt
	if loaded != record {
		t.Fatalf("loaded = %+v, want %+v", loaded, record)
	}
	if loaded.UpdatedAt == 0 {
		t.Fatal("Save must stamp UpdatedAt")
	}
}

// TestARecordFromOlderBuildOmitsTheCheckout pins the read half of the upgrade
// path: a record written before the checkout was recorded carries no such field,
// and it must load as "unknown" rather than as an empty or invented path. Every
// decision that needs the checkout has to be able to tell the difference, and a
// zero value that looked like a decision would move a command onto a directory
// nobody named.
func TestARecordFromOlderBuildOmitsTheCheckout(t *testing.T) {
	box := store(t)
	document := `{"pid": 4242, "startedAt": 1700000000, "port": 3080, "phase": "running"}`
	if err := os.WriteFile(box.Path, []byte(document), 0o600); err != nil {
		t.Fatalf("write the record: %v", err)
	}
	loaded, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("an older record must load")
	}
	if loaded.RepoDir != "" || loaded.NodeVersion != "" || loaded.NodePath != "" {
		t.Fatalf("loaded = %+v, want the fields the older build never wrote to read as unknown", loaded)
	}
	if loaded.PID != 4242 || loaded.Port != 3080 || loaded.Phase != testPhaseRunning {
		t.Fatalf("loaded = %+v, want every field it does carry", loaded)
	}
}

// TestSaveKeepsEveryOptionalFieldOutWhenEmpty pins the other half: a record that
// carries nothing optional stays a small document, so a reader cannot mistake an
// absent fact for an empty string that was saved.
func TestSaveKeepsEveryOptionalFieldOutWhenEmpty(t *testing.T) {
	box := store(t)
	if err := box.Save(testDoc{PID: 4242, StartedAt: 1_700_000_000, Port: 3080, Phase: testPhaseRunning}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(box.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, key := range []string{"repoDir", "nodeVersion", "nodePath", "url", "spawnedPid"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("record = %s, want no %q for an empty %s", data, key, key)
		}
	}
}

// TestLoadReportsAMissingRecord pins that "never started" is not an error.
func TestLoadReportsAMissingRecord(t *testing.T) {
	box := store(t)
	_, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok {
		t.Fatal("Load reported a record for a missing file")
	}
}

// TestLoadRejectsCorruptRecords pins every way a record can be unusable, so the
// caller can remove it instead of trusting it.
//
// Both a syntax error and a type error count. A record whose pid is the string
// "abc", or whose start time is a string, is a document this build cannot act
// on: decoding must fail at Load, where the caller still knows the file is the
// problem, rather than silently leaving a zero behind that later reads as "no
// fingerprint" and disables the PID-reuse check.
func TestLoadRejectsCorruptRecords(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"whitespace", "   \n"},
		{"not json", "not json at all"},
		{"two documents", `{"pid": 1, "port": 1, "phase": "running"}{"pid": 2, "port": 1, "phase": "running"}`},
		{"trailing garbage", `{"pid": 1, "port": 1, "phase": "running"} not json`},
		{"truncated", `{"pid":`},
		{"missing pid", `{"port": 1, "phase": "running"}`},
		{"zero pid", `{"pid": 0, "port": 1, "phase": "running"}`},
		{"negative pid", `{"pid": -5, "port": 1, "phase": "running"}`},
		{"pid is a string", `{"pid": "abc", "port": 1, "phase": "running"}`},
		{"pid is an object", `{"pid": {"value": 1}, "port": 1, "phase": "running"}`},
		{"startedAt is a string", `{"pid": 1, "startedAt": "x", "port": 1, "phase": "running"}`},
		{"port is a string", `{"pid": 1, "port": "3080", "phase": "running"}`},
		{"phase is a number", `{"pid": 1, "port": 1, "phase": 42}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			box := store(t)
			if err := os.WriteFile(box.Path, []byte(testCase.content), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}
			_, ok, err := box.Load()
			if ok {
				t.Fatal("a corrupt record must not be reported as usable")
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Load error = %v, want ErrCorrupt", err)
			}
		})
	}
}

// TestLoadAcceptsUnknownFields is the regression test for a record that a
// different build of dshctl wrote.
//
// The record is written by one version and read by another — after an upgrade, a
// downgrade, or when two installations share a home directory — so a field this
// build does not know is normal. Treating it as corruption made `stop` delete the
// only pointer to a running server, report success, and leave the server
// serving with nobody able to manage it.
func TestLoadAcceptsUnknownFields(t *testing.T) {
	box := store(t)
	content := `{"pid": 4242, "startedAt": 1700000000, "port": 3080, "phase": "running",` +
		`"url": "http://127.0.0.1:3080/?token=x", "generation": 7, "future": {"nested": true}}`
	if err := os.WriteFile(box.Path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	record, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("a record with unknown fields must still be readable")
	}
	if record.PID != 4242 || record.Port != 3080 || record.Phase != testPhaseRunning {
		t.Fatalf("record = %+v, want the fields this build knows", record)
	}
	if record.URL != "http://127.0.0.1:3080/?token=x" {
		t.Fatalf("URL = %q, want the known field preserved", record.URL)
	}
}

// TestLoadRejectsAnOversizedRecord pins the read bound.
func TestLoadRejectsAnOversizedRecord(t *testing.T) {
	box := store(t)
	padding := strings.Repeat("x", maxTestBytes+1)
	content := `{"pid": 1, "port": 1, "phase": "running", "note": "` + padding + `"}`
	if err := os.WriteFile(box.Path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, ok, err := box.Load()
	if ok {
		t.Fatal("an oversized file must not be reported as a usable record")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
}

// TestLoadAndRemoveHandleResidueAtTheRecordPath pins that a directory where the
// record belongs is reported as corrupt and then cleared, instead of failing
// every later command forever.
func TestLoadAndRemoveHandleResidueAtTheRecordPath(t *testing.T) {
	box := store(t)
	if err := os.MkdirAll(filepath.Join(box.Path, "residue"), 0o700); err != nil {
		t.Fatalf("seed residue: %v", err)
	}
	_, ok, err := box.Load()
	if ok {
		t.Fatal("a directory at the record path is not a record")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
	if err := box.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(box.Path); !os.IsNotExist(err) {
		t.Fatalf("residue survived Remove: %v", err)
	}
}

// TestSaveRefusesAUsableButMeaninglessRecord pins that a zero pid never reaches
// the disk, where a later stop could read it.
func TestSaveRefusesAUsableButMeaninglessRecord(t *testing.T) {
	box := store(t)
	if err := box.Save(testDoc{PID: 0, Port: 3080}); err == nil {
		t.Fatal("saving a zero pid must fail")
	}
	if err := box.Save(testDoc{PID: -1, Port: 3080}); err == nil {
		t.Fatal("saving a negative pid must fail")
	}
	_, ok, err := box.Load()
	if err != nil || ok {
		t.Fatalf("Load = (_, %v, %v), want no record", ok, err)
	}
}

// TestSaveReplacesAtomically pins that a reader never sees a partial document.
func TestSaveReplacesAtomically(t *testing.T) {
	box := store(t)
	if err := box.Save(testDoc{PID: 1, Port: 1, Phase: testPhaseRunning}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := box.Save(testDoc{PID: 2, Port: 1, Phase: testPhaseRunning, URL: "http://x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(box.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded domain.Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("the file is not a complete document: %v\n%s", err, data)
	}
	if decoded.PID != 2 || decoded.Phase != testPhaseRunning || decoded.URL != "http://x" {
		t.Fatalf("decoded = %+v, want the second record", decoded)
	}
	// No temporary files may be left behind.
	entries, err := os.ReadDir(filepath.Dir(box.Path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(box.Path) {
			t.Fatalf("leftover file after an atomic write: %s", entry.Name())
		}
	}
}

// TestRemove pins that removal is idempotent.
func TestRemove(t *testing.T) {
	box := store(t)
	if err := box.Remove(); err != nil {
		t.Fatalf("removing a missing record must not fail: %v", err)
	}
	if err := box.Save(testDoc{PID: 7, Port: 1, Phase: testPhaseRunning}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := box.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := box.Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

// contains reports whether haystack holds needle.
