package state

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// The runtime record is the only handle on a server dshctl started: it decides
// what a stop is allowed to end. A record that loads but describes nothing — a
// pid of zero, a phase nobody wrote — would make every later command act on a
// fiction, so the loader's contract is pinned by fuzzing it.

// FuzzLoadRecordIsEitherCorruptOrUsable pins that contract for arbitrary file
// content: no panic, and a record that loads successfully names a real process.
func FuzzLoadRecordIsEitherCorruptOrUsable(f *testing.F) {
	for _, seed := range []string{
		`{"pid":1,"spawnedPid":1,"startedAt":2,"port":3080,"phase":"running"}`,
		`{"pid":0}`,
		`{"pid":-1}`,
		`{"pid":"abc"}`,
		`{"pid":1,"port":-5}`,
		`{"pid":1,"port":0}`,
		`{"pid":1,"startedAt":0}`,
		`{"pid":1,"phase":42}`,
		`{"pid":1,"url":"http://127.0.0.1:3080/?token=x"}`,
		`{}`,
		`null`,
		`[]`,
		`not json at all`,
		``,
		"\xff\xfe\x00",
		`{"pid":1}{"pid":2}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		path := filepath.Join(t.TempDir(), "dsh-web-3080.state.json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("seed the record: %v", err)
		}

		record, ok, err := Store{Path: path}.Load()
		if err != nil {
			// A rejection must say what is wrong; the class is what callers
			// branch on, and a corrupt record must be recognizable as such.
			if err.Error() == "" {
				t.Fatalf("record %q was rejected with an empty message", content)
			}
			return
		}
		if !ok {
			// "No record" is only allowed when there is nothing usable in the
			// file at all.
			if len(content) != 0 {
				t.Fatalf("record %q loaded as absent rather than as corrupt", content)
			}
			return
		}
		if record.PID <= 0 {
			t.Fatalf("record %q loaded as usable but names pid %d", content, record.PID)
		}
	})
}

// FuzzSaveLoadRoundTrip pins that a record survives the file it is written to.
//
// The record is read back by a later process, so a field the writer drops is a
// field the next command silently loses — the port, the phase and the start-time
// fingerprint decide whether a server can still be managed, and the runtime and
// the checkout are the only place a later command can learn what the running
// instance is. Every field is part of the tuple on purpose: a field that is
// added to the struct but not to this property is a field nothing checks.
func FuzzSaveLoadRoundTrip(f *testing.F) {
	f.Add(1, 2, int64(1_700_000_000), 3080, "running", "http://127.0.0.1:3080/?token=abc", "24.20.0", "/opt/node/bin/node", "/srv/deepseek-harness")
	f.Add(48737, 48736, int64(0), 65535, "", "", "", "", "")
	f.Add(2, 0, int64(-1), 1, "stopping", "\n", "24.12.0", "/usr/bin/node", "/tmp/repo")
	f.Add(3, 3, int64(9_999_999_999), -1, "running", "标题", "25.0.0", "C:\\node\\node.exe", "C:\\repo")

	f.Fuzz(func(t *testing.T, pid, spawnedPID int, startedAt int64, port int, phase, url, nodeVersion, nodePath, repoDir string) {
		// JSON strings are Unicode: encoding/json replaces an invalid byte
		// sequence with U+FFFD, so an ill-formed string cannot survive the file
		// by construction. That is the standard library's documented behaviour,
		// not a property of this package, so those inputs are out of scope.
		for _, text := range []string{phase, url, nodeVersion, nodePath, repoDir} {
			if !utf8.ValidString(text) {
				return
			}
		}
		store := Store{Path: filepath.Join(t.TempDir(), "dsh-web-3080.state.json")}
		saved := Record{
			PID:         pid,
			SpawnedPID:  spawnedPID,
			StartedAt:   startedAt,
			Port:        port,
			Phase:       Phase(phase),
			URL:         url,
			NodeVersion: nodeVersion,
			NodePath:    nodePath,
			RepoDir:     repoDir,
		}
		if err := store.Save(saved); err != nil {
			// A record the writer refuses is reported to the caller and never
			// written, so there is nothing to read back.
			return
		}
		loaded, ok, err := store.Load()
		if err != nil {
			t.Fatalf("a saved record must load back: %v", err)
		}
		if !ok {
			t.Fatalf("a saved record loaded as absent")
		}
		saved.UpdatedAt = loaded.UpdatedAt
		if loaded != saved {
			t.Fatalf("round trip changed the record:\n saved  %+v\n loaded %+v", saved, loaded)
		}
		if loaded.UpdatedAt <= 0 {
			t.Fatalf("round trip lost the write timestamp: %+v", loaded)
		}
	})
}
