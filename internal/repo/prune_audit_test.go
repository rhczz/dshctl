package repo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustCheckout builds the temporary git checkout and fails, rather than skips,
// when the real git binary cannot create it.
//
// The prune tests assert what a real repository makes of a real tree; silently
// skipping them on a machine without git would leave the destructive half of
// dshctl unverified while the build stays green, which is exactly the outcome a
// test is supposed to prevent. newCheckout's requireGit is the one place that
// asks whether git is there, so every fixture answers that question the same
// way; this helper adds the check that the fixture really is a worktree.
func mustCheckout(t *testing.T) *checkout {
	t.Helper()
	box := newCheckout(t)
	if !box.repo.IsGit() {
		t.Fatalf("the fixture at %s is not a git worktree", box.dir)
	}
	return box
}

// manifestFixture builds a tree holding the named root-relative files and
// returns it as an fs.FS, which is what hasSiblingManifest walks.
func manifestFixture(t *testing.T, files ...string) fs.FS {
	t.Helper()
	root := t.TempDir()
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return os.DirFS(root)
}

// TestHasSiblingManifestChecksTheCandidatesParent pins the guard exactly as its
// doc comment describes it: it reports whether the directory that *contains* the
// candidate ships a package.json, so a candidate is left alone when its own
// parent is a package rather than a deleted one.
//
// The two areas of the walk reach their candidates at different depths —
// vendor/<name> and packages/<group>/<name> — and the guard has to find the
// parent of each. Getting the depth wrong either protects nothing (the vendor
// case, where the computed parent is "." and the function always answers false)
// or protects the wrong directory (the packages case, where the grandparent
// packages/ is consulted instead of packages/<group>/). Both mistakes make
// `dshctl build` delete, or refuse to delete, a directory for reasons the
// operator cannot see from the message.
func TestHasSiblingManifestChecksTheCandidatesParent(t *testing.T) {
	cases := []struct {
		name     string
		files    []string
		relative string
		want     bool
	}{
		{
			name:     "vendor candidate with no manifest anywhere",
			relative: "vendor/x",
			want:     false,
		},
		{
			name:     "vendor candidate whose parent is the vendor area itself",
			files:    []string{"vendor/package.json"},
			relative: "vendor/x",
			want:     true,
		},
		{
			name:     "packages candidate with no manifest anywhere",
			relative: "packages/g/x",
			want:     false,
		},
		{
			name:     "packages candidate when only the area root has a manifest",
			files:    []string{"packages/package.json"},
			relative: "packages/g/x",
			want:     false,
		},
		{
			name:     "packages candidate whose own parent has the manifest",
			files:    []string{"packages/g/package.json"},
			relative: "packages/g/x",
			want:     true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rootFS := manifestFixture(t, testCase.files...)
			if got := hasSiblingManifest(rootFS, testCase.relative); got != testCase.want {
				t.Fatalf("hasSiblingManifest(%q) = %v, want %v: the guard must ask whether the candidate's own parent, %q, holds a package.json",
					testCase.relative, got, testCase.want, filepath.ToSlash(filepath.Dir(filepath.FromSlash(testCase.relative))))
			}
		})
	}
}

// TestHasSiblingManifestIgnoresADirectoryNamedPackageJSON pins the second half
// of the guard: a directory called package.json is not a manifest, so a
// candidate beside one is still residue — and deleting the candidate's parent
// tree is exactly what the guard exists to prevent.
//
// The fixture places the directory where the guard consults the filesystem for
// the candidate "packages/g/x", which is its parent (pinned by
// TestHasSiblingManifestChecksTheCandidatesParent): a directory at that path
// must not be mistaken for the manifest of a real package.
func TestHasSiblingManifestIgnoresADirectoryNamedPackageJSON(t *testing.T) {
	root := t.TempDir()
	checked := filepath.Join(root, filepath.FromSlash("packages/g/package.json"))
	if err := os.MkdirAll(checked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if hasSiblingManifest(os.DirFS(root), "packages/g/x") {
		t.Fatal("a directory named package.json is not a manifest")
	}
}

// TestPruneKeepsASubtreeUnderAManifestedParent is the end-to-end half of the
// same guard: when the directory that *contains* a candidate ships a
// package.json — a group that is a package itself, a vendored tree that keeps
// its manifest one level up — the candidate belongs to the project and is not
// residue, however little it holds.
//
// The controls are the other half of the depth rule. A manifest at the area root
// is not the candidate's parent, so it must not shield the whole area; and a
// candidate under a group without a manifest is still residue, so this test
// cannot pass merely because the walk stopped classifying.
func TestPruneKeepsASubtreeUnderAManifestedParent(t *testing.T) {
	box := mustCheckout(t)
	// The manifested parent, and the area-root manifest that used to be
	// consulted instead of it.
	box.write("packages/grp/package.json", "{}")
	box.write("packages/package.json", "{}")
	box.commit()

	// A deleted package whose own parent is a package.
	box.write("packages/grp/gone/node_modules/dep/index.js", "x")
	// The control inside the packages area: same depth, unmanifested parent.
	box.write("packages/other/gone/node_modules/dep/index.js", "x")
	// The control in the vendor area, where no manifest sits above the
	// candidate at all.
	box.write("vendor/control/node_modules/dep/index.js", "x")

	got := box.candidatePaths()
	if len(got) != 2 || got[0] != "packages/other/gone" || got[1] != "vendor/control" {
		t.Fatalf("candidates = %v, want only the two controls: a manifested parent protects its subtree, an area-root manifest must not protect the whole area", got)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !box.exists("packages/grp/gone/node_modules/dep/index.js") {
		t.Fatal("a subtree of the manifested parent was pruned")
	}
	if box.exists("packages/other/gone") {
		t.Fatal("the control residue in the packages area was not pruned")
	}
	if box.exists("vendor/control") {
		t.Fatal("the control residue in the vendor area was not pruned")
	}
}

// TestPruneReportsEachRemovalWithItsResidue pins the success line an operator
// reads after a build: it names the directory and, in a stable order, the
// entries that identified it as residue. The line is the only evidence that the
// deletion was the intended one, so its shape is part of the interface.
func TestPruneReportsEachRemovalWithItsResidue(t *testing.T) {
	box := mustCheckout(t)
	box.commit()
	// Written out of sorted order on purpose: the report is sorted, not the
	// order the filesystem happens to return.
	box.write("packages/group/gone/node_modules/dep/index.js", "x")
	box.write("packages/group/gone/tsconfig.tsbuildinfo", "{}")
	box.write("packages/group/gone/.typecheck/state.json", "{}")

	var lines []string
	report, err := box.repo.Prune(context.Background(), func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	candidate := filepath.Join(box.dir, "packages", "group", "gone")
	want := fmt.Sprintf("removing residue: %s (only .typecheck, node_modules, tsconfig.tsbuildinfo)", candidate)
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("report lines = %#v, want [%q]", lines, want)
	}
	if len(report.Removed) != 1 || report.Removed[0] != candidate || len(report.Failed) != 0 {
		t.Fatalf("report = %+v, want exactly one removal of %s", report, candidate)
	}
	if box.exists("packages/group/gone") {
		t.Fatal("the reported directory still exists")
	}
}

// TestPruneWithNoCandidatesReportsNothing pins the quiet case: a tree with no
// residue produces no output at all, because every line the callback receives is
// printed to the operator.
func TestPruneWithNoCandidatesReportsNothing(t *testing.T) {
	box := mustCheckout(t)
	box.commit()
	called := false
	report, err := box.repo.Prune(context.Background(), func(string) { called = true })
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if called || len(report.Removed) != 0 || len(report.Failed) != 0 {
		t.Fatalf("report = %+v (callback called: %v), want a silent no-op", report, called)
	}
}

// TestStrictlyInsideRejectsEverythingNotBelowTheRoot pins the boundary check the
// destructive walk depends on. It compares path *elements*, so a directory whose
// name merely starts with the root's name ("/a/bc" beside "/a/b") is outside,
// and a path that climbs out through ".." is outside however it is written.
func TestStrictlyInsideRejectsEverythingNotBelowTheRoot(t *testing.T) {
	cases := []struct {
		name   string
		root   string
		target string
		want   bool
	}{
		{"empty root and a dot target", "", ".", false},
		{"empty root and a parent target", "", "..", false},
		{"the root itself", "/a/b", "/a/b", false},
		{"a sibling whose name extends the root's name", "/a/b", "/a/bc", false},
		{"a direct child", "/a/b", "/a/b/c", true},
		{"a grandchild", "/a/b", "/a/b/c/d", true},
		{"a sibling reached through a parent step", "/a/b", "/a/b/../c", false},
		{"an ancestor of the root", "/a/b/c", "/a/b", false},
		{"an unrelated path", "/a/b", "/x/y", false},
		{"a child of an ancestor reached through ..", "/a/b/c", "/a/b/../../x", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := strictlyInside(testCase.root, testCase.target); got != testCase.want {
				t.Fatalf("strictlyInside(%q, %q) = %v, want %v", testCase.root, testCase.target, got, testCase.want)
			}
		})
	}
}

// TestResolveExistingPrefixResolvesOnlyTheExistingPart pins the helper the
// candidate walk uses to compare real paths.
//
// A path that does not exist yet must come back unchanged — this runs during
// diagnostics, where a missing directory is reported separately — while a path
// whose existing prefix is a symlink must come back resolved, because that is
// what makes a symlinked packages/ detectable at all.
func TestResolveExistingPrefixResolvesOnlyTheExistingPart(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}

	// Nothing exists below base: the path is absolute and untouched.
	missing := filepath.Join(base, "not", "created", "yet")
	got, err := resolveExistingPrefix(missing)
	if err != nil {
		t.Fatalf("resolveExistingPrefix(%q): %v", missing, err)
	}
	if got != missing {
		t.Fatalf("resolveExistingPrefix(%q) = %q, want the path unchanged", missing, got)
	}

	// The existing prefix is a symlink: the missing tail is appended to the
	// resolved target.
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	want := filepath.Join(real, "sub", "leaf")
	got, err = resolveExistingPrefix(filepath.Join(link, "sub", "leaf"))
	if err != nil {
		t.Fatalf("resolveExistingPrefix through a symlink: %v", err)
	}
	if got != want {
		t.Fatalf("resolveExistingPrefix(link/sub/leaf) = %q, want %q", got, want)
	}

	// An existing path is resolved as a whole.
	if got, err := resolveExistingPrefix(link); err != nil || got != real {
		t.Fatalf("resolveExistingPrefix(%q) = (%q, %v), want %q", link, got, err, real)
	}
}

// TestIsGitAcceptsAWorktreePointerFile pins the two shapes a checkout's .git
// takes: a directory in an ordinary clone, and a regular file holding a
// "gitdir:" pointer in a linked worktree or a submodule. Reading only the first
// shape makes dshctl treat a perfectly good linked worktree as not a repository.
func TestIsGitAcceptsAWorktreePointerFile(t *testing.T) {
	clone := t.TempDir()
	checkout := Repo{Dir: clone}
	if checkout.IsGit() {
		t.Fatal("a directory without .git must not be reported as a repository")
	}
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if !checkout.IsGit() {
		t.Fatal("a .git directory must be accepted")
	}

	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/wt\n"), 0o600); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	if !(Repo{Dir: worktree}).IsGit() {
		t.Fatal("a linked worktree's .git pointer file must be accepted")
	}
}

// TestBuildReadyRejectsNonDirectories pins that both halves of the build state
// are the right kind of thing: dependencies are a directory, and the build
// record is a regular file. A file named node_modules or a directory named like
// the record would otherwise be read as a finished build.
func TestBuildReadyRejectsNonDirectories(t *testing.T) {
	dir := t.TempDir()
	checkout := Repo{Dir: dir, BuildRecordRel: ".dsh-build/client-build-environment.json"}
	record := filepath.Join(dir, ".dsh-build", "client-build-environment.json")

	// node_modules as a regular file is not an installed tree.
	if err := os.WriteFile(filepath.Join(dir, "node_modules"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write node_modules: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatalf("mkdir build record dir: %v", err)
	}
	if err := os.WriteFile(record, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if checkout.NodeModulesPresent() {
		t.Fatal("a file named node_modules is not installed dependencies")
	}
	if checkout.BuildReady() {
		t.Fatal("a file named node_modules must not satisfy BuildReady")
	}

	// A real node_modules, but the record is a directory.
	if err := os.Remove(filepath.Join(dir, "node_modules")); err != nil {
		t.Fatalf("remove node_modules file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.Remove(record); err != nil {
		t.Fatalf("remove record: %v", err)
	}
	if err := os.MkdirAll(record, 0o755); err != nil {
		t.Fatalf("mkdir record: %v", err)
	}
	if checkout.BuildReady() {
		t.Fatal("a directory at the build record path must not satisfy BuildReady")
	}

	// The positive control: a regular record beside a real node_modules.
	if err := os.Remove(record); err != nil {
		t.Fatalf("remove record directory: %v", err)
	}
	if err := os.WriteFile(record, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if !checkout.BuildReady() {
		t.Fatal("node_modules plus a regular build record means the tree is built")
	}
}

// TestPruneKeepsUntrackedTreesWithoutKnownResidue pins the other side of the
// safety rule, so a future loosening of the guard cannot silently turn every
// untracked directory into a deletion: a directory holding one unrecognised
// entry is never a candidate.
func TestPruneKeepsUntrackedTreesWithoutKnownResidue(t *testing.T) {
	box := mustCheckout(t)
	box.commit()
	box.write("packages/group/unknown/src/index.ts", "export {}")

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a directory holding sources", got)
	}
}

// TestPruneCandidatesAreSortedByPath pins the order the report and the removal
// follow, so two runs over the same tree produce the same output.
func TestPruneCandidatesAreSortedByPath(t *testing.T) {
	box := mustCheckout(t)
	box.commit()
	for _, name := range []string{"zeta", "alpha", "middle"} {
		box.write("packages/group/"+name+"/node_modules/dep/index.js", "x")
	}
	box.write("vendor/vendorpkg/lib/index.js", "x")

	candidates, err := box.repo.PruneCandidates(context.Background())
	if err != nil {
		t.Fatalf("PruneCandidates: %v", err)
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, filepath.Base(candidate.Path))
	}
	want := "alpha,middle,zeta,vendorpkg"
	if strings.Join(paths, ",") != want {
		t.Fatalf("candidate order = %v, want %s", paths, want)
	}
}
