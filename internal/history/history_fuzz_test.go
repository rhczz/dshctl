package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzLoadNeverInventsAPosition pins the two answers Load may give for bytes
// that are not a history: ErrCorrupt, or a document every part of which is
// usable. It may never panic, and it may never report success for a file whose
// records could not be rolled back to.
func FuzzLoadNeverInventsAPosition(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("{}"))
	f.Add([]byte(`{"repos":[{"repo":"/a","records":[{"commit":"c1","at":1}]}]}`))
	f.Add([]byte("null"))
	f.Add([]byte("[]"))
	f.Add([]byte(`{"repos":[{"repo":"/a","records":[{"commit":"","at":1}]}]}`))
	f.Add([]byte(`{"repos":[{"repo":"","records":[{"commit":"c1","at":1}]}]}`))
	f.Add([]byte(`{"repos":[{"repo":"/a","records":[{"commit":"c1","at":1}]},{"repo":"/a","records":[]}]}`))
	f.Add([]byte("{\"repos\":[{\"repo\":\"/a\",\"records\":[{\"commit\":\"c1\",\"at\":1,\"future\":true}]}]}"))

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "updates.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		file, ok, err := Store{Path: path}.Load()
		if err != nil {
			if ok {
				t.Fatalf("Load reported success together with an error: %v", err)
			}
			return
		}
		// The file was just written, so a nil error must mean a history was
		// read: "absent" is only for a path that does not exist.
		if !ok {
			t.Fatal("Load reported no file for a path that exists")
		}
		for _, group := range file.Repos {
			if group.Repo == "" {
				t.Fatalf("a loaded group has no checkout: %+v", file)
			}
			for _, record := range group.Records {
				if record.Commit == "" {
					t.Fatalf("a loaded record has no commit: %+v", file)
				}
				if record.At <= 0 {
					t.Fatalf("a loaded record has no time: %+v", file)
				}
			}
		}
	})
}

// FuzzVisitKeepsTheStackASubsequence pins what Visit may do to a stack: the
// result is bounded, starts with the new position, holds no duplicates, and
// keeps the surviving input positions in their original order.
func FuzzVisitKeepsTheStackASubsequence(f *testing.F) {
	f.Add("abc", "b", int64(3))
	f.Add("", "a", int64(0))
	f.Add("aaaa", "a", int64(1))
	f.Add("abcdefghij", "j", int64(9))

	f.Fuzz(func(t *testing.T, commits, top string, at int64) {
		records := make([]Record, 0, len(commits))
		for _, commit := range commits {
			records = append(records, Record{Commit: string(commit), At: at})
		}
		position := Record{Commit: top, Selector: "latest", At: at}
		got := Visit(records, position)

		if len(got) > MaxRecords {
			t.Fatalf("stack length = %d, want at most %d", len(got), MaxRecords)
		}
		if len(got) == 0 || got[0] != position {
			t.Fatalf("stack top = %+v, want the new position", got)
		}
		seen := map[string]bool{}
		for _, record := range got {
			if seen[record.Commit] {
				t.Fatalf("duplicate commit %q in %+v", record.Commit, got)
			}
			seen[record.Commit] = true
		}
		// Every surviving input position must still be in input order.
		index := 0
		for _, record := range got[1:] {
			for index < len(records) && records[index].Commit != record.Commit {
				index++
			}
			if index == len(records) {
				t.Fatalf("position %q is not in input order in %+v", record.Commit, got)
			}
			index++
		}
	})
}

// FuzzStepNeverReturnsTheCurrentPosition pins the step arithmetic: the current
// position is step zero, so any accepted positive step names a different
// position, and a step is accepted only when every shorter step is too.
func FuzzStepNeverReturnsTheCurrentPosition(f *testing.F) {
	f.Add("abc", "b", 1)
	f.Add("abc", "z", 2)
	f.Add("", "a", 1)
	f.Add("abc", "b", 0)

	f.Fuzz(func(t *testing.T, commits, current string, steps int) {
		records := make([]Record, 0, len(commits))
		for _, commit := range commits {
			records = append(records, Record{Commit: string(commit), At: 1})
		}
		now := Record{Commit: current, At: 2}
		got, ok := Step(records, now, steps)
		if steps < 1 {
			if ok {
				t.Fatalf("Step accepted %d steps", steps)
			}
			return
		}
		if !ok {
			return
		}
		if got.Commit == current {
			t.Fatalf("Step(%d) returned the current position", steps)
		}
		known := map[string]bool{current: true}
		for _, commit := range commits {
			known[string(commit)] = true
		}
		if !known[got.Commit] {
			t.Fatalf("Step returned %q, which was never in the history", got.Commit)
		}
		if steps > 1 {
			shorter, ok := Step(records, now, steps-1)
			if !ok {
				t.Fatalf("Step(%d) was accepted but Step(%d) was not", steps, steps-1)
			}
			if shorter.Commit == got.Commit {
				t.Fatalf("Step(%d) and Step(%d) both returned %q", steps, steps-1, got.Commit)
			}
		}
	})
}

// FuzzSaveLoadRoundTrips pins the file format as a bijection for every document
// a caller can build: whatever Save writes, Load reads back as the same value.
func FuzzSaveLoadRoundTrips(f *testing.F) {
	f.Add("ab", "/checkouts/a")
	f.Add("", "/checkouts/b")
	f.Add("c1c2c3", "/checkouts/c")
	f.Add(strings.Repeat("x", 1500), "/checkouts/big")

	f.Fuzz(func(t *testing.T, commits, repo string) {
		// JSON cannot carry arbitrary bytes: an invalid UTF-8 checkout path is
		// replaced by U+FFFD on the way out, so it is outside the property.
		if repo == "" || commits == "" || !utf8.ValidString(repo) {
			return
		}
		records := make([]Record, 0, len(commits))
		for index, commit := range commits {
			records = append(records, Record{
				Commit:   fmt.Sprintf("%c-%d", commit, index),
				Selector: "latest",
				At:       int64(index + 1),
			})
		}
		file := File{Repos: []Group{{Repo: repo, Records: records}}}
		box := Store{Path: filepath.Join(t.TempDir(), "updates.json")}
		if err := box.Save(file); err != nil {
			// Save may refuse a document its reader would reject — too large,
			// or carrying a position without a commit or a time. Any other
			// error is a bug.
			if !strings.Contains(err.Error(), "过大") && !strings.Contains(err.Error(), "拒绝写入") {
				t.Fatalf("Save: %v", err)
			}
			return
		}
		loaded, ok, err := box.Load()
		if err != nil || !ok {
			t.Fatalf("Load = (%+v, %v, %v), want the saved file", loaded, ok, err)
		}
		if len(loaded.Records(repo)) != len(records) {
			t.Fatalf("records = %d, want %d", len(loaded.Records(repo)), len(records))
		}
		for index, record := range loaded.Records(repo) {
			if record != records[index] {
				t.Fatalf("record %d = %+v, want %+v", index, record, records[index])
			}
		}
	})
}
