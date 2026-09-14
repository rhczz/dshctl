package atomically

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWriteFileCreatesTheDocument pins the ordinary path.
func TestWriteFileCreatesTheDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.json")
	if err := WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The permission bits are a Unix concept: Windows reports 0666 for a
	// writable file whatever mode was asked for. The comprehensive mode matrix
	// lives in the unix-tagged test file.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestWriteFileReplacesInOneStep pins that a reader never sees a partial
// document: the replacement is a rename, not a truncate-and-write.
//
// The reader runs freely and is never parked while a write is in flight, or it
// would be blind exactly when it matters. Two things make the result mean
// something: the test waits until the reader has really looked at the file
// before the replacements start, and it requires further observations to arrive
// while they are running. An earlier version only checked a slice of
// observations, so a reader that observed nothing at all — because it never
// overlapped the writer — passed the test without proving anything.
func TestWriteFileReplacesInOneStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	first := strings.Repeat("a", 64<<10)
	if err := WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	second := "short\n"

	var (
		mu       sync.Mutex
		observed []string
		readErr  error
	)
	stop := make(chan struct{})
	done := make(chan struct{})
	// Deferred rather than closed at the end: a t.Fatalf below must not leave
	// the reader running past the failure.
	defer func() {
		close(stop)
		<-done
	}()
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			mu.Lock()
			if err != nil {
				if readErr == nil {
					readErr = err
				}
			} else {
				observed = append(observed, string(data))
			}
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// observations returns how many complete documents the reader has recorded.
	observations := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(observed)
	}

	// Wait for the first observation: without it, the assertion at the end would
	// be satisfied by an empty list, which is the vacuous pass this test must
	// not allow.
	deadline := time.Now().Add(30 * time.Second)
	for observations() == 0 {
		mu.Lock()
		failed := readErr
		mu.Unlock()
		if failed != nil {
			t.Fatalf("the reader failed: %v", failed)
		}
		if time.Now().After(deadline) {
			t.Fatal("the reader never observed the document")
		}
		time.Sleep(time.Millisecond)
	}
	before := observations()

	// Replace repeatedly with a differently sized document while the reader
	// runs, until the reader has recorded at least one observation during the
	// replacements. A fixed number of rounds could end before the reader was
	// scheduled once on a loaded machine, and the test would fail for the
	// machine instead of for the code. Every observed version must be one of
	// the two complete documents.
	deadline = time.Now().Add(30 * time.Second)
	for observations() <= before {
		if err := WriteFile(path, []byte(second), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := WriteFile(path, []byte(first), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the reader never observed a replacement while they were running")
		}
	}

	mu.Lock()
	versions := append([]string(nil), observed...)
	failed := readErr
	mu.Unlock()
	if failed != nil {
		t.Fatalf("the reader failed: %v", failed)
	}
	if len(versions) <= before {
		t.Fatalf("the reader recorded %d observations before the replacements and none during them",
			before)
	}
	for _, version := range versions {
		if version != first && version != second {
			t.Fatalf("a reader saw a partial document of %d bytes", len(version))
		}
	}
}

// TestWriteFileRefusesADirectoryDestination pins that a destination that is a
// directory fails loudly and changes nothing.
//
// A rename cannot replace a directory, and the earlier Windows branch that
// tried to delete the destination first is gone on purpose: the destination is
// the operator's, so an empty directory must not be removed to make room for a
// state file. Whatever the platform reports, the caller gets an error, the
// directory keeps its contents, and the temporary file is cleaned up rather
// than left in the state directory forever.
func TestWriteFileRefusesADirectoryDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}
	inside := filepath.Join(path, "keep")
	if err := os.WriteFile(inside, []byte("keep"), 0o600); err != nil {
		t.Fatalf("seed directory: %v", err)
	}

	err := WriteFile(path, []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("writing onto a directory must fail loudly")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v, want it to name %s", err, path)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("the destination directory did not survive: %v (%v)", info, statErr)
	}
	if data, readErr := os.ReadFile(inside); readErr != nil || string(data) != "keep" {
		t.Fatalf("the directory was emptied: %q (%v)", data, readErr)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			t.Fatalf("a failed write left %s behind", entry.Name())
		}
	}
}

// TestWriteFileRefusesAFileInPlaceOfItsDirectory pins the ENOTDIR case: the
// parent path exists, but it is a regular file rather than a directory.
//
// The failure must name the directory it could not create. Reporting only the
// leaf would send the operator looking at a path that does not exist at all,
// while the real problem is the file sitting where the state directory belongs.
func TestWriteFileRefusesAFileInPlaceOfItsDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := WriteFile(filepath.Join(parent, "file.json"), []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("a file where the directory belongs must fail")
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("error = %v, want it to name %s", err, parent)
	}
	// Nothing was created and the occupying file was not modified: this is a
	// reported failure, not a repair that deletes the operator's file.
	//
	// The classification of the failure is platform-specific and is pinned where
	// the platform can express it: Unix says "not a directory", while Windows
	// reports the same condition as "the path was not found", which Go maps to
	// fs.ErrNotExist (see atomically_windows_test.go).
	if data, readErr := os.ReadFile(parent); readErr != nil || string(data) != "not a directory" {
		t.Fatalf("the occupying file changed: %q (%v)", data, readErr)
	}
}

// TestSweepReportsAPathThatIsNotADirectory pins that a state directory that is
// actually a file is an error rather than a silent success: sweeping is
// best-effort, but "there was nothing to sweep" must not be reported for a path
// that was never read.
func TestSweepReportsAPathThatIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	notADir := filepath.Join(root, "state")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := Sweep(notADir)
	if err == nil {
		t.Fatal("sweeping a path that is not a directory must fail")
	}
	if !strings.Contains(err.Error(), notADir) {
		t.Fatalf("error = %v, want it to name %s", err, notADir)
	}
	if data, readErr := os.ReadFile(notADir); readErr != nil || string(data) != "not a directory" {
		t.Fatalf("Sweep modified the file: %q (%v)", data, readErr)
	}
}

// TestWriteFileLeavesNoTemporaryFiles pins that a successful write cleans up.
func TestWriteFileLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	if err := WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "file.json" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the document", names)
	}
}

// TestWriteFileReportsAnUnwritableDirectory pins the failure path.
func TestWriteFileReportsAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() <= 0 {
		// Root ignores directory permissions, and Windows has no euid
		// (Geteuid returns -1) and does not enforce Unix directory modes, so
		// the failure this test pins cannot exist on either.
		t.Skip("directory permissions are not enforceable here")
	}
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readOnly, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })

	err := WriteFile(filepath.Join(readOnly, "file.json"), []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("expected an error for an unwritable directory")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want a permission error", err)
	}
}

// TestWriteFileReplacesASymlinkInsteadOfFollowingIt pins that the destination is
// replaced, so nothing outside the directory is written.
func TestWriteFileReplacesASymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.json")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	path := filepath.Join(dir, "file.json")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("the symlink target was overwritten with %q", data)
	}
	replaced, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path: %v", err)
	}
	if string(replaced) != "replacement" {
		t.Fatalf("path holds %q, want the replacement", replaced)
	}
}

// TestSweepRemovesOrphanedTemporaryFiles pins the cleanup of a process killed
// between creating its temporary file and renaming it. Nothing else collects
// those, so a state directory written repeatedly would accumulate them.
func TestSweepRemovesOrphanedTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	// What a killed writer leaves behind, plus files that must survive.
	orphan := filepath.Join(dir, ".dsh-web-3080.state.json.tmp2743496574")
	if err := os.WriteFile(orphan, bytes.Repeat([]byte("x"), 1024), 0o600); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	keep := []string{
		filepath.Join(dir, "dsh-web-3080.state.json"),
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, ".gitignore"),      // dotfile, but not a temporary file
		filepath.Join(dir, "notes.tmp"),       // not a dotfile
		filepath.Join(dir, ".backup.tmp.txt"), // hidden and contains ".tmp", but not this package's shape
	}
	for _, path := range keep {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".keepdir.tmp"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	if err := Sweep(dir); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphaned temporary file survived: %v", err)
	}
	for _, path := range keep {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("Sweep removed %s: %v", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".keepdir.tmp")); err != nil {
		t.Fatalf("Sweep removed a directory: %v", err)
	}
}

// TestSweepIgnoresAMissingDirectory pins that provisioning a fresh state
// directory does not fail because it is not there yet.
func TestSweepIgnoresAMissingDirectory(t *testing.T) {
	if err := Sweep(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
}

// TestWriteFileIsDurableOverAnExistingFile pins that replacing a document leaves
// exactly the new content, with no temporary file and no leftover of the old.
func TestWriteFileIsDurableOverAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	if err := WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("content = %q, want the replacement", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the document", names)
	}
}
