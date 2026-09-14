package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordJSON is a complete record that Load accepts, used where a test needs a
// file the loader would otherwise be happy with.
const recordJSON = `{"pid": 4242, "startedAt": 1700000000, "port": 3080, "phase": "running"}`

// TestLoadRejectsASymlinkAtTheRecordPath pins that the strict file rules cover
// links as well as directories.
//
// The record is written and read only by dshctl, so a symlink here is residue:
// following it would let a disposable state directory reach a document the
// operator keeps elsewhere, and the next `dshctl stop` would then delete or
// rewrite whatever it points at. Reporting it as corrupt is what lets the
// caller clear the link instead.
func TestLoadRejectsASymlinkAtTheRecordPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere.json")
	if err := os.WriteFile(target, []byte(recordJSON), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	box := Store{Path: filepath.Join(root, "dsh-web.state.json")}
	if err := os.Symlink(target, box.Path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	_, ok, err := box.Load()
	if ok {
		t.Fatal("a symlink at the record path is not a record")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
	// The link was not followed: the target still holds exactly what it did.
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != recordJSON {
		t.Fatalf("the symlink target changed: %q (%v)", data, readErr)
	}
}

// TestRemoveUnlinksASymlinkInsteadOfItsTarget pins that Remove deletes the
// entry at the record path, not what it resolves to.
//
// The symlink is the residue; unlinking it clears the path. Deleting the target
// instead would let a command whose whole contract is "the state directory is
// disposable" remove a file the operator keeps somewhere else.
func TestRemoveUnlinksASymlinkInsteadOfItsTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere.json")
	if err := os.WriteFile(target, []byte(recordJSON), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	box := Store{Path: filepath.Join(root, "dsh-web.state.json")}
	if err := os.Symlink(target, box.Path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if err := box.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(box.Path); !os.IsNotExist(err) {
		t.Fatalf("the symlink survived Remove: %v", err)
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != recordJSON {
		t.Fatalf("Remove deleted the symlink target: %q (%v)", data, readErr)
	}
}

// TestLoadReportsAnUnreadableRecordAsAnEnvironmentFailure pins that a record
// which exists but cannot be read is reported as such, and not as corruption.
//
// The distinction decides what the caller does next: corruption justifies
// deleting the record, while a permission problem is a property of the
// environment that deleting the file would hide — and the operator would then
// lose the only pointer to a server that is still running.
func TestLoadReportsAnUnreadableRecordAsAnEnvironmentFailure(t *testing.T) {
	if os.Geteuid() <= 0 {
		// Root ignores file permissions (and Windows has no euid: Geteuid
		// returns -1), so the failure this test pins cannot exist there.
		t.Skip("file permissions are not enforceable here")
	}
	box := store(t)
	if err := os.WriteFile(box.Path, []byte(recordJSON), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(box.Path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(box.Path, 0o600) })

	_, ok, err := box.Load()
	if ok {
		t.Fatal("an unreadable file must not be reported as a usable record")
	}
	if err == nil {
		t.Fatal("an unreadable record must be reported")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Load error = %v, want a permission error", err)
	}
	if errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want it distinguishable from a corrupt record", err)
	}
}

// TestSaveReportsAFileInPlaceOfItsDirectory pins the ENOTDIR failure of a save:
// the parent path exists, but as a regular file.
//
// Save must name the directory it could not create and leave the occupying file
// alone; silently writing nowhere would make a later status command report a
// server that was never recorded.
func TestSaveReportsAFileInPlaceOfItsDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	box := Store{Path: filepath.Join(parent, "dsh-web.state.json")}

	err := box.Save(Record{PID: 4242, Port: 3080, Phase: PhaseRunning})
	if err == nil {
		t.Fatal("Save must fail when its directory is a file")
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("error = %v, want it to name %s", err, parent)
	}
	if data, readErr := os.ReadFile(parent); readErr != nil || string(data) != "not a directory" {
		t.Fatalf("the occupying file changed: %q (%v)", data, readErr)
	}
}

// TestRemoveReportsARecordItCannotDelete pins that a removal which does not
// happen is reported.
//
// The caller uses Remove to say "there is no server any more"; returning nil
// while the record is still on disk leaves the next command acting on a pid
// that dshctl believes it has forgotten.
func TestRemoveReportsARecordItCannotDelete(t *testing.T) {
	if os.Geteuid() <= 0 {
		// Root ignores directory permissions, and Windows does not enforce
		// Unix directory modes, so removal always succeeds there.
		t.Skip("directory permissions are not enforceable here")
	}
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	box := Store{Path: filepath.Join(dir, "dsh-web.state.json")}
	if err := os.WriteFile(box.Path, []byte(recordJSON), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := box.Remove(); err == nil {
		t.Fatal("Remove must report a record it could not delete")
	}
	if _, err := os.Lstat(box.Path); err != nil {
		t.Fatalf("the record disappeared anyway: %v", err)
	}
}

// TestMatchWithASubSecondTolerance pins that a tolerance below one second is
// truncated to whole seconds rather than rounded up.
//
// The tolerance exists to absorb the jitter between the recorded and the
// observed start time, and it is compared in seconds because that is the
// resolution of both values. Rounding 500ms up to a full second would let a
// recycled pid look like the recorded server, which is the one thing the
// fingerprint is there to prevent.
func TestMatchWithASubSecondTolerance(t *testing.T) {
	const startedAt = int64(1_700_000_000)
	cases := []struct {
		name      string
		tolerance time.Duration
		delta     int64
		want      bool
	}{
		{"zero tolerance, exact", 0, 0, true},
		{"zero tolerance, one second off", 0, 1, false},
		{"zero tolerance, one second off backwards", 0, -1, false},
		{"half a second, exact", 500 * time.Millisecond, 0, true},
		{"just under a second, one second off", 999 * time.Millisecond, 1, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := Record{PID: 42, StartedAt: startedAt}
			if got := Match(record, startedAt+testCase.delta, testCase.tolerance); got != testCase.want {
				t.Fatalf("Match(tolerance=%s, delta=%ds) = %v, want %v",
					testCase.tolerance, testCase.delta, got, testCase.want)
			}
		})
	}
}

// TestMatchWithANegativeToleranceStillMatchesAnExactFingerprint pins the one
// case a negative tolerance must not change: the same pid with the same start
// time is the recorded process, whatever the caller passed as a tolerance.
//
// Match documents that a record matches "when the pid is the same and the start
// times agree", and that only a *known mismatch* never matches. A negative
// tolerance is nonsense as a budget, but turning it into "nothing matches"
// makes dshctl treat its own running server as a foreign process — status
// reports it as recycled and stop refuses to end it.
func TestMatchWithANegativeToleranceStillMatchesAnExactFingerprint(t *testing.T) {
	record := Record{PID: 42, StartedAt: 1_700_000_000}
	for _, tolerance := range []time.Duration{-time.Second, -time.Minute} {
		if !Match(record, record.StartedAt, tolerance) {
			t.Fatalf("Match with tolerance %s rejected an exact fingerprint", tolerance)
		}
	}
}

// TestDescribeAnEmptyRecord pins the diagnostic line for a record that was
// never filled in.
//
// Describe is what the operator reads when something is already wrong, so it
// must render rather than panic, and it must not invent a URL: a link printed
// for a record without one would be clicked against a server that is not there.
// The start time of an empty record is the Unix epoch, which is visibly not a
// real start time.
func TestDescribeAnEmptyRecord(t *testing.T) {
	// The rendering of the epoch depends on the process time zone, so it is
	// pinned here instead of depending on the machine's.
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })

	text := Record{PID: 7}.Describe()
	for _, want := range []string{"pid=7", "started=1970-01-01T00:00:00Z", "port=0", "phase="} {
		if !strings.Contains(text, want) {
			t.Fatalf("Describe = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "url=") {
		t.Fatalf("Describe = %q, want no url for a record without one", text)
	}
}

// TestLoadAcceptsPartialRecords pins which fields the loader insists on.
//
// Only the pid is validated, because it is the one field that can name a
// process: a record without it is unusable by definition. Every other field is
// accepted as it was stored, and these cases are deliberately the ones a caller
// must still handle defensively — port 0 is not a port any server bound, and a
// missing start time disables the PID-reuse check entirely, because Match
// treats an unknown time as a match.
func TestLoadAcceptsPartialRecords(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    Record
	}{
		{
			"port zero",
			`{"pid": 7, "startedAt": 1700000000, "port": 0, "phase": "running"}`,
			Record{PID: 7, StartedAt: 1_700_000_000, Phase: PhaseRunning},
		},
		{
			"negative port",
			`{"pid": 7, "startedAt": 1700000000, "port": -1, "phase": "running"}`,
			Record{PID: 7, StartedAt: 1_700_000_000, Port: -1, Phase: PhaseRunning},
		},
		{
			"missing phase",
			`{"pid": 7, "startedAt": 1700000000, "port": 3080}`,
			Record{PID: 7, StartedAt: 1_700_000_000, Port: 3080},
		},
		{
			"missing start time",
			`{"pid": 7, "port": 3080, "phase": "running"}`,
			Record{PID: 7, Port: 3080, Phase: PhaseRunning},
		},
		{
			"pid only",
			`{"pid": 7}`,
			Record{PID: 7},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			box := store(t)
			if err := os.WriteFile(box.Path, []byte(testCase.content), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}
			loaded, ok, err := box.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !ok {
				t.Fatal("the loader refused a record it is expected to accept")
			}
			if loaded != testCase.want {
				t.Fatalf("loaded = %+v, want %+v", loaded, testCase.want)
			}
		})
	}
}
