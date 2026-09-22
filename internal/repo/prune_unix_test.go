//go:build unix

package repo

// These tests depend on POSIX semantics that Windows does not share: permission
// bits that stop a deletion, and ENOTDIR for a path below a regular file. They
// live in a Unix-only file so the package still builds and vets on Windows
// instead of carrying assertions that platform can never satisfy.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// skipIfRoot skips a permission test when the process can ignore permissions.
//
// Root bypasses the permission bits every one of these tests relies on, so the
// failure they pin cannot be provoked; asserting it anyway would fail for a
// reason that says nothing about the code.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test needs")
	}
}

// chmodDir changes a directory's mode and restores a removable mode when the
// test ends, so the temporary tree can still be cleaned up.
func chmodDir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// TestPruneReportsARemovalItCouldNotPerform pins the branch that keeps a failed
// deletion from failing a build: the directory is listed as failed, the report
// tells the operator what to do instead, and the directory is left in place
// rather than half-removed.
//
// A read-only parent is how a real checkout produces this: the candidate's own
// contents can be removed, but unlinking the candidate itself needs write
// permission on the directory that holds it.
func TestPruneReportsARemovalItCouldNotPerform(t *testing.T) {
	skipIfRoot(t)
	box := mustCheckout(t)
	box.commit()
	box.write("packages/group/gone/node_modules/dep/index.js", "x")

	parent := filepath.Join(box.dir, "packages", "group")
	chmodDir(t, parent, 0o500)

	var lines []string
	report, err := box.repo.Prune(context.Background(), func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("a directory that cannot be removed must not fail the prune: %v", err)
	}
	candidate := filepath.Join(box.dir, "packages", "group", "gone")
	if len(report.Removed) != 0 {
		t.Fatalf("Removed = %v, want nothing: the deletion failed", report.Removed)
	}
	if len(report.Failed) != 1 || report.Failed[0] != candidate {
		t.Fatalf("Failed = %v, want [%s]", report.Failed, candidate)
	}
	if !box.exists("packages/group/gone") {
		t.Fatal("a candidate reported as failed was removed anyway")
	}
	want := []string{
		fmt.Sprintf("removing residue: %s (only node_modules)", candidate),
		fmt.Sprintf("warning: %s could not be removed; the build continues, run pnpm run clean by hand later", candidate),
	}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("report lines = %#v, want %#v", lines, want)
	}
}

// TestPruneSkipsACandidateItCannotRead pins that a directory the process may not
// inspect is skipped rather than failed: the classify step cannot know whether
// it holds residue, and guessing either way is worse than leaving it alone.
//
// A healthy candidate in the same tree proves the walk kept going instead of
// giving up on the first unreadable directory.
func TestPruneSkipsACandidateItCannotRead(t *testing.T) {
	skipIfRoot(t)
	box := mustCheckout(t)
	box.commit()
	box.write("packages/group/dark/node_modules/dep/index.js", "x")
	box.write("packages/group/plain/lib/index.js", "x")

	dark := filepath.Join(box.dir, "packages", "group", "dark")
	chmodDir(t, dark, 0o000)

	got := box.candidatePaths()
	if len(got) != 1 || got[0] != "packages/group/plain" {
		t.Fatalf("candidates = %v, want only the readable residue", got)
	}
	report, err := box.repo.Prune(context.Background(), nil)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(report.Removed) != 1 || len(report.Failed) != 0 {
		t.Fatalf("report = %+v, want one removal and no failure", report)
	}
	// Reading inside the directory again needs its permission back; the
	// cleanup would restore it only after the test.
	if err := os.Chmod(dark, 0o755); err != nil {
		t.Fatalf("restore mode: %v", err)
	}
	if !box.exists("packages/group/dark/node_modules/dep/index.js") {
		t.Fatal("the contents of an unreadable directory were deleted")
	}
}

// TestResolveExistingPrefixReportsAPathBelowARegularFile pins that a path whose
// parent is not a directory is reported rather than quietly accepted: the
// resolver must not answer with a path it never managed to look at, because the
// caller compares that answer against candidates it is about to delete.
func TestResolveExistingPrefixReportsAPathBelowARegularFile(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}
	file := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := resolveExistingPrefix(filepath.Join(file, "child"))
	if err == nil {
		t.Fatalf("resolveExistingPrefix below a regular file = %q, want the ENOTDIR failure", got)
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %v, want a *fs.PathError from the failing Lstat", err)
	}
}
