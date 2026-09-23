package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// store returns a Store inside a throwaway directory.
func store(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "updates.json")}
}

// record builds one position.
func record(commit, selector string, at int64) Record {
	return Record{Commit: commit, Selector: selector, At: at}
}

// TestStoreRoundTripsTwoCheckouts pins that a file written by Save is read back
// as the same value: the format is the contract between two builds of dshctl
// that share a state directory.
func TestStoreRoundTripsTwoCheckouts(t *testing.T) {
	box := store(t)
	file := File{Repos: []Group{
		{Repo: "/checkouts/a", Records: []Record{
			record("bbbb", "latest", 200),
			record("aaaa", "", 100),
		}},
		{Repo: "/checkouts/b", Records: []Record{record("dddd", "dsh-v0.1.0", 400)}},
	}}
	if err := box.Save(file); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load reported no file after Save")
	}
	if !reflect.DeepEqual(loaded, file) {
		t.Fatalf("Load = %+v, want %+v", loaded, file)
	}
}

// TestLoadReportsAMissingFileAsAbsent pins the difference between "no history
// yet" and "a history that cannot be read": only the second is an error.
func TestLoadReportsAMissingFileAsAbsent(t *testing.T) {
	box := store(t)
	file, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok {
		t.Fatalf("Load reported a file: %+v", file)
	}
}

// TestLoadToleratesUnknownFields pins forward compatibility: a file written by
// a newer build is not corruption, because refusing it would take rollback away
// exactly when a downgrade needs it most.
func TestLoadToleratesUnknownFields(t *testing.T) {
	box := store(t)
	document := `{
	  "future": true,
	  "repos": [
	    {"repo": "/a", "future": 1,
	     "records": [{"commit": "c1", "selector": "latest", "at": 7, "future": 2}]}
	  ]
	}`
	if err := os.WriteFile(box.Path, []byte(document), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	file, ok, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load reported no file")
	}
	records := file.Records("/a")
	if len(records) != 1 || records[0].Commit != "c1" || records[0].At != 7 {
		t.Fatalf("records = %+v, want the c1 record", records)
	}
}

// TestLoadRejectsCorruption pins every shape that is not a history document.
// Callers treat a corrupt file as "refuse to guess", so the classification has
// to be ErrCorrupt rather than a silent empty history.
func TestLoadRejectsCorruption(t *testing.T) {
	cases := []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{"empty file", func(t *testing.T, path string) {
			writeFile(t, path, "")
		}},
		{"whitespace only", func(t *testing.T, path string) {
			writeFile(t, path, "  \n")
		}},
		{"not json", func(t *testing.T, path string) {
			writeFile(t, path, "{not json")
		}},
		{"trailing content", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[]} {"repos":[]}`)
		}},
		{"empty repo", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[{"repo":"","records":[{"commit":"c1","at":1}]}]}`)
		}},
		{"empty commit", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[{"repo":"/a","records":[{"commit":"","at":1}]}]}`)
		}},
		{"no time", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[{"repo":"/a","records":[{"commit":"c1"}]}]}`)
		}},
		{"negative time", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[{"repo":"/a","records":[{"commit":"c1","at":-1}]}]}`)
		}},
		{"empty group", func(t *testing.T, path string) {
			writeFile(t, path, `{"repos":[{"repo":"/a","records":[]}]}`)
		}},
		{"a directory", func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}},
		{"oversized", func(t *testing.T, path string) {
			writeFile(t, path, strings.Repeat("x", maxFileBytes+1))
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			box := store(t)
			testCase.write(t, box.Path)
			_, ok, err := box.Load()
			if ok {
				t.Fatal("a corrupt file is not a history")
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Load error = %v, want ErrCorrupt", err)
			}
		})
	}
}

// TestVisitTruncatesToAnExistingPosition pins the undo-stack rule: returning to
// a position that is already in the stack drops everything newer than it, so a
// second rollback keeps walking backwards instead of bouncing forward.
func TestVisitTruncatesToAnExistingPosition(t *testing.T) {
	records := []Record{record("b", "latest", 3), record("a", "", 2), record("x", "", 1)}
	got := Visit(records, record("a", "-n 2", 4), MaxRecords())
	want := []Record{record("a", "-n 2", 4), record("x", "", 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestVisitPrependsANewPosition pins the other half: a position the stack has
// never seen becomes the new top and the old positions are kept below it.
func TestVisitPrependsANewPosition(t *testing.T) {
	records := []Record{record("a", "", 2), record("x", "", 1)}
	got := Visit(records, record("b", "latest", 3), MaxRecords())
	want := []Record{record("b", "latest", 3), record("a", "", 2), record("x", "", 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestVisitCapsTheStack pins the retention bound: the newest positions are what
// rollback can reach, and the oldest fall off.
func TestVisitCapsTheStack(t *testing.T) {
	var records []Record
	for index := 0; index < MaxRecords()+10; index++ {
		records = Visit(records, record(fmt.Sprintf("c%03d", index), "", int64(index)), MaxRecords())
	}
	if len(records) != MaxRecords() {
		t.Fatalf("stack length = %d, want %d", len(records), MaxRecords())
	}
	if records[0].Commit != fmt.Sprintf("c%03d", MaxRecords()+9) {
		t.Fatalf("stack top = %q, want the newest position", records[0].Commit)
	}
	if records[len(records)-1].Commit != "c010" {
		t.Fatalf("stack bottom = %q, want the oldest retained position", records[len(records)-1].Commit)
	}
}

// TestStepWalksTheVirtualStack pins what each rollback argument means. The
// stack top is always the current position, so the first step is the position
// the previous deployment left, and the current position never wins.
func TestStepWalksTheVirtualStack(t *testing.T) {
	records := []Record{record("b", "latest", 3), record("a", "", 2), record("x", "", 1)}
	cases := []struct {
		name    string
		current Record
		steps   int
		want    string
		ok      bool
	}{
		{"first step", record("b", "", 9), 1, "a", true},
		{"second step", record("b", "", 9), 2, "x", true},
		{"past the end", record("b", "", 9), 3, "", false},
		{"zero steps", record("b", "", 9), 0, "", false},
		{"negative steps", record("b", "", 9), -1, "", false},
		{"a manual position first", record("c", "", 9), 1, "b", true},
		{"a manual position second", record("c", "", 9), 2, "a", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := Step(records, testCase.current, testCase.steps)
			if ok != testCase.ok {
				t.Fatalf("Step ok = %v, want %v", ok, testCase.ok)
			}
			if ok && got.Commit != testCase.want {
				t.Fatalf("Step = %q, want %q", got.Commit, testCase.want)
			}
		})
	}
}

// TestStepStartsFromWhereTheCurrentPositionSitsInTheStack pins the case a
// hand-made checkout creates: when the current commit is already in the stack,
// the positions newer than it are no longer reachable, because they are not
// "before" anything the operator is at now.
func TestStepStartsFromWhereTheCurrentPositionSitsInTheStack(t *testing.T) {
	records := []Record{record("b", "latest", 3), record("a", "", 2), record("x", "", 1)}
	got, ok := Step(records, record("a", "", 9), 1)
	if !ok || got.Commit != "x" {
		t.Fatalf("Step = %+v (ok=%v), want x", got, ok)
	}
}

// TestRecordsForAnUnknownCheckoutIsEmpty pins that the answer for a checkout
// this state directory has never deployed is "nothing", not a neighbouring
// checkout's stack.
func TestRecordsForAnUnknownCheckoutIsEmpty(t *testing.T) {
	file := File{Repos: []Group{{Repo: "/a", Records: []Record{record("c1", "", 1)}}}}
	if got := file.Records("/b"); len(got) != 0 {
		t.Fatalf("Records(/b) = %+v, want none", got)
	}
	if got := file.Records(""); len(got) != 0 {
		t.Fatalf("Records(\"\") = %+v, want none", got)
	}
}

// TestWithAddsANewCheckout pins that recording a checkout the file has never
// seen appends its group without touching the others.
func TestWithAddsANewCheckout(t *testing.T) {
	file := File{Repos: []Group{{Repo: "/a", Records: []Record{record("a1", "", 1)}}}}
	updated := file.With("/b", []Record{record("b1", "latest", 2)})
	if got := updated.Records("/a"); len(got) != 1 || got[0].Commit != "a1" {
		t.Fatalf("checkout a = %+v, want it untouched", got)
	}
	if got := updated.Records("/b"); len(got) != 1 || got[0].Commit != "b1" {
		t.Fatalf("checkout b = %+v, want the new group", got)
	}
	if len(updated.Repos) != 2 {
		t.Fatalf("groups = %+v, want two", updated.Repos)
	}
}

// TestWithKeepsCheckoutsIndependent pins that a write for one checkout never
// rewrites another one's history, so two checkouts sharing a state directory
// cannot truncate each other's stack.
func TestWithKeepsCheckoutsIndependent(t *testing.T) {
	file := File{Repos: []Group{
		{Repo: "/a", Records: []Record{record("a1", "", 1)}},
		{Repo: "/b", Records: []Record{record("b1", "", 1)}},
	}}
	updated := file.With("/a", []Record{record("a2", "latest", 2)})
	if got := updated.Records("/a"); len(got) != 1 || got[0].Commit != "a2" {
		t.Fatalf("checkout a = %+v, want the a2 record", got)
	}
	if got := updated.Records("/b"); len(got) != 1 || got[0].Commit != "b1" {
		t.Fatalf("checkout b = %+v, want it untouched", got)
	}
	removed := updated.With("/a", nil)
	if got := removed.Records("/a"); len(got) != 0 {
		t.Fatalf("checkout a = %+v, want the group removed", got)
	}
}

// TestSaveWritesExactlyOneFinalNewline pins the text shape: the file is read by
// humans during an incident, and a missing final newline turns every later diff
// into noise.
func TestSaveWritesExactlyOneFinalNewline(t *testing.T) {
	box := store(t)
	if err := box.Save(File{Repos: []Group{{Repo: "/a", Records: []Record{record("c1", "", 1)}}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(box.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") || strings.HasSuffix(string(data), "\n\n") {
		t.Fatalf("file = %q, want exactly one final newline", data)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("file is not JSON: %v", err)
	}
}

// TestLoadRejectsANonObjectDocument pins that only the one document shape Save
// writes is accepted. A top-level null decodes into an empty value without an
// error, which would turn a truncated or hand-mangled file into "no history" —
// exactly the answer that lets a rollback guess.
func TestLoadRejectsANonObjectDocument(t *testing.T) {
	for _, document := range []string{"null", "[]", `"repos"`, "42", "true"} {
		t.Run(document, func(t *testing.T) {
			box := store(t)
			writeFile(t, box.Path, document)
			_, ok, err := box.Load()
			if ok {
				t.Fatal("a non-object document is not a history")
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Load error = %v, want ErrCorrupt", err)
			}
		})
	}
}

// TestLoadRejectsDuplicateCheckoutGroups pins that one checkout has exactly one
// stack: with two groups the answer to "where can this checkout roll back to"
// would depend on which one a caller happened to read.
func TestLoadRejectsDuplicateCheckoutGroups(t *testing.T) {
	box := store(t)
	document := `{"repos":[
	  {"repo":"/a","records":[{"commit":"c1","at":1}]},
	  {"repo":"/a","records":[{"commit":"c2","at":2}]}
	]}`
	writeFile(t, box.Path, document)
	_, ok, err := box.Load()
	if ok {
		t.Fatal("a file with two groups for one checkout is not a history")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
}

// TestFileRecordsReturnsACopy pins that a caller cannot reach into a loaded
// value and change what a later read of it sees.
func TestFileRecordsReturnsACopy(t *testing.T) {
	file := File{Repos: []Group{{Repo: "/a", Records: []Record{record("c1", "", 1)}}}}
	records := file.Records("/a")
	records[0].Commit = "mutated"
	if again := file.Records("/a"); again[0].Commit != "c1" {
		t.Fatalf("the file changed through a returned slice: %+v", again)
	}
}

// TestVisitRefreshesAnExistingTop pins that returning to the position that is
// already on top updates what is recorded about it: the move just happened, so
// its selector and time are the new ones.
func TestVisitRefreshesAnExistingTop(t *testing.T) {
	records := []Record{record("b", "", 1), record("a", "", 1)}
	got := Visit(records, record("b", "-n 1", 9), MaxRecords())
	want := []Record{record("b", "-n 1", 9), record("a", "", 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestVisitOnAnEmptyStack pins the first deployment: the position is the whole
// stack.
func TestVisitOnAnEmptyStack(t *testing.T) {
	got := Visit(nil, record("a", "latest", 1), MaxRecords())
	want := []Record{record("a", "latest", 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestStepOnAnEmptyStack pins that a checkout with no recorded move has nothing
// to roll back to, even though the current position exists.
func TestStepOnAnEmptyStack(t *testing.T) {
	if _, ok := Step(nil, record("a", "", 1), 1); ok {
		t.Fatal("an empty history offered a step")
	}
}

// TestSaveWritesTheRecordsItIsGiven pins that Save is a writer, not a second
// place that caps: a silent truncation here would drop positions a caller
// explicitly built.
func TestSaveWritesTheRecordsItIsGiven(t *testing.T) {
	box := store(t)
	records := make([]Record, MaxRecords()+5)
	for index := range records {
		records[index] = record(fmt.Sprintf("c%03d", index), "", int64(index+1))
	}
	if err := box.Save(File{Repos: []Group{{Repo: "/a", Records: records}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, _, err := box.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(loaded.Records("/a")); got != len(records) {
		t.Fatalf("records = %d, want all %d", got, len(records))
	}
}

// TestLoadRejectsDuplicatePositions pins that one commit has one entry: two
// entries would make the step arithmetic ambiguous, and a valid stack never
// repeats a commit.
func TestLoadRejectsDuplicatePositions(t *testing.T) {
	box := store(t)
	document := `{"repos":[{"repo":"/a","records":[
	  {"commit":"c1","at":1},
	  {"commit":"c1","at":2}
	]}]}`
	writeFile(t, box.Path, document)
	_, ok, err := box.Load()
	if ok {
		t.Fatal("a file repeating a position is not a history")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
}

// TestVisitDropsARepeatedCommit pins the hand-edited-file case: even if a stack
// repeats a commit, the result holds it once, at the top.
func TestVisitDropsARepeatedCommit(t *testing.T) {
	records := []Record{record("b", "", 3), record("a", "", 2), record("b", "", 1)}
	got := Visit(records, record("b", "latest", 4), MaxRecords())
	want := []Record{record("b", "latest", 4), record("a", "", 2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestVisitDropsDuplicatesAmongTheSurvivors pins that the no-duplicates rule
// holds for every commit, not only for the one on top: the file's invariant is
// what makes the step arithmetic unambiguous. The first occurrence survives,
// because the stack is ordered newest first.
func TestVisitDropsDuplicatesAmongTheSurvivors(t *testing.T) {
	records := []Record{record("b", "", 3), record("a", "", 2), record("a", "", 1), record("x", "", 0)}
	got := Visit(records, record("b", "latest", 4), MaxRecords())
	want := []Record{record("b", "latest", 4), record("a", "", 2), record("x", "", 0)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Visit = %+v, want %+v", got, want)
	}
}

// TestSaveRefusesADocumentTooLargeToRead pins the writer's bound: Save must
// never produce a file its own Load reports as corrupt.
func TestSaveRefusesADocumentTooLargeToRead(t *testing.T) {
	box := store(t)
	records := make([]Record, 0, 4000)
	for index := 0; index < 4000; index++ {
		records = append(records, record(fmt.Sprintf("commit-%06d", index), "latest", int64(index+1)))
	}
	err := box.Save(File{Repos: []Group{{Repo: "/a", Records: records}}})
	if err == nil {
		t.Fatal("Save wrote a document larger than the reader's bound")
	}
	if _, statErr := os.Lstat(box.Path); !os.IsNotExist(statErr) {
		t.Fatalf("the oversize document reached the disk: %v", statErr)
	}
}

// TestSaveRefusesADocumentItsReaderWouldReject pins the writer's other bound:
// every shape Load calls corrupt is refused before it reaches the disk, so a
// caller can never produce a history its own reader cannot use.
func TestSaveRefusesADocumentItsReaderWouldReject(t *testing.T) {
	position := func(commit string, at int64) Record { return Record{Commit: commit, At: at} }
	cases := []struct {
		name string
		file File
	}{
		{"no repo", File{Repos: []Group{{Records: []Record{position("c1", 1)}}}}},
		{"empty group", File{Repos: []Group{{Repo: "/a"}}}},
		{"no commit", File{Repos: []Group{{Repo: "/a", Records: []Record{{At: 1}}}}}},
		{"no time", File{Repos: []Group{{Repo: "/a", Records: []Record{{Commit: "c1"}}}}}},
		{"negative time", File{Repos: []Group{{Repo: "/a", Records: []Record{position("c1", -1)}}}}},
		{"duplicate group", File{Repos: []Group{
			{Repo: "/a", Records: []Record{position("c1", 1)}},
			{Repo: "/a", Records: []Record{position("c2", 2)}},
		}}},
		{"duplicate position", File{Repos: []Group{{Repo: "/a", Records: []Record{
			position("c1", 1), position("c1", 2),
		}}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			box := store(t)
			if err := box.Save(testCase.file); err == nil {
				t.Fatal("Save wrote a document its own Load reports as corrupt")
			}
			if _, err := os.Lstat(box.Path); !os.IsNotExist(err) {
				t.Fatalf("the refused document reached the disk: %v", err)
			}
		})
	}
}

// writeFile writes content, creating parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
