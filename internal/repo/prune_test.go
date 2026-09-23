package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/run"
)

// checkout is a temporary git repository shaped like the managed checkout.
type checkout struct {
	t    *testing.T
	dir  string
	repo Repo
}

// requireGit fails the test when the real git binary is missing.
//
// The fixture is a real repository and every prune decision is git's answer
// about it, so a machine without git has nothing to assert here. Skipping
// instead would leave the destructive half of dshctl unverified behind a green
// build, which is the outcome mustCheckout documents in full.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("prune tests need a real git executable: %v", err)
	}
}

// newCheckout creates a git repository, commits what has been staged, and
// returns it wrapped in a Repo.
func newCheckout(t *testing.T) *checkout {
	t.Helper()
	requireGit(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	box := &checkout{t: t, dir: dir}
	box.git("init", "-q", ".")
	box.git("config", "user.email", "fixture@example.com")
	box.git("config", "user.name", "fixture")
	box.git("config", "commit.gpgsign", "false")
	// The layout the tests exercise is stated here, the way a product would
	// state its own: the mechanism no longer knows it.
	box.repo = Repo{
		Dir:                  dir,
		Ex:                   fixtureRunner{runner: run.NewRunner()},
		ManifestRel:          "package.json",
		WorkspaceManifestRel: "pnpm-workspace.yaml",
		BuildRecordRel:       ".dsh-build/client-build-environment.json",
		Remote:               "origin",
		Branch:               "master",
		Residue: map[string]struct{}{
			"node_modules": {},
			"lib":          {},
			".typecheck":   {},
		},
		Areas: []PruneArea{{Name: "packages", Depth: 2}, {Name: "vendor", Depth: 1}},
	}
	box.write("package.json", "{}")
	box.write("pnpm-workspace.yaml", "packages:\n  - packages/*\n")
	return box
}

// fixtureGitEnv returns the environment the fixture's git commands run with.
//
// The fixture creates a real repository in a temporary directory, so nothing
// about the developer's or the CI machine's git environment may decide where
// that repository is: an exported GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE,
// GIT_COMMON_DIR or GIT_OBJECT_DIRECTORY redirects these commands into whatever
// repository those variables name, so the fixture would inspect — and commit
// into — a real checkout instead of its own temp directory. Global and system
// configuration are neutralised as well, so an installed hook cannot run and a
// machine-wide setting cannot change what is staged. The environment's config
// injection pair is dropped for the same reason: GIT_CONFIG_KEY_*/VALUE_* can
// set core.hooksPath without any file existing at all.
func fixtureGitEnv() []string {
	blocked := map[string]struct{}{
		"GIT_DIR":                          {},
		"GIT_WORK_TREE":                    {},
		"GIT_INDEX_FILE":                   {},
		"GIT_COMMON_DIR":                   {},
		"GIT_OBJECT_DIRECTORY":             {},
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
	}
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if _, skip := blocked[upper]; skip {
			continue
		}
		if upper == "GIT_CONFIG_COUNT" || strings.HasPrefix(upper, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(upper, "GIT_CONFIG_VALUE_") {
			continue
		}
		environment = append(environment, entry)
	}
	// os.DevNull is /dev/null on Unix and NUL on Windows: either way the path
	// names an empty configuration file rather than the operator's own.
	return append(environment, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// fixtureRunner runs the fixture's git queries with the same sanitised
// environment as its setup commands.
//
// The package's queries go through Repo.Ex, which would otherwise inherit the
// process environment: a GIT_DIR exported by the shell would make a query answer
// about a different repository than the one the fixture built, and the test
// would then assert the right thing about the wrong tree.
type fixtureRunner struct{ runner *run.Runner }

// Run implements run.Executor.
func (r fixtureRunner) Run(ctx context.Context, cmd run.Command) error {
	if cmd.Env == nil {
		cmd.Env = fixtureGitEnv()
	}
	return r.runner.Run(ctx, cmd)
}

// Capture implements run.Capturer, which run.Collector prefers over a plain
// Executor, so the environment set here is the one a query really runs with.
func (r fixtureRunner) Capture(ctx context.Context, cmd run.Command) run.Result {
	if cmd.Env == nil {
		cmd.Env = fixtureGitEnv()
	}
	return r.runner.Capture(ctx, cmd)
}

// write creates a file with its parent directories.
func (c *checkout) write(relative, content string) {
	c.t.Helper()
	path := filepath.Join(c.dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.t.Fatalf("mkdir for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		c.t.Fatalf("write %s: %v", relative, err)
	}
}

// mkdir creates a directory.
func (c *checkout) mkdir(relative string) {
	c.t.Helper()
	if err := os.MkdirAll(filepath.Join(c.dir, filepath.FromSlash(relative)), 0o755); err != nil {
		c.t.Fatalf("mkdir %s: %v", relative, err)
	}
}

// git runs a git command inside the checkout.
//
// The environment is set explicitly (see fixtureGitEnv) and core.hooksPath is
// overridden per invocation: an empty hooks path names no directory, so no hook
// can be found even if a configuration layer still manages to set one. The
// fixture's commands may therefore only ever touch their own temporary
// directory.
//
// Every failure is a fixture failure. requireGit has already established that
// git is installed, so "git is missing" is not an outcome this helper may turn
// into a silent pass.
func (c *checkout) git(args ...string) string {
	c.t.Helper()
	full := append([]string{"-c", "core.hooksPath="}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = c.dir
	cmd.Env = fixtureGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.t.Fatalf("git %v failed (%v): %s", args, err, out)
	}
	return string(out)
}

// commit stages everything and commits it.
func (c *checkout) commit() {
	c.t.Helper()
	c.git("add", "-A")
	c.git("commit", "-qm", "fixture")
}

// exists reports whether a path still exists.
func (c *checkout) exists(relative string) bool {
	_, err := os.Lstat(filepath.Join(c.dir, filepath.FromSlash(relative)))
	return err == nil
}

// candidatePaths returns the relative candidate paths, sorted.
func (c *checkout) candidatePaths() []string {
	c.t.Helper()
	candidates, err := c.repo.PruneCandidates(context.Background())
	if err != nil {
		c.t.Fatalf("PruneCandidates: %v", err)
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		relative, err := filepath.Rel(c.dir, candidate.Path)
		if err != nil {
			c.t.Fatalf("rel: %v", err)
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	sort.Strings(paths)
	return paths
}

// TestPruneRemovesOnlyResidue pins the ordinary case: a package directory that
// git no longer tracks and that holds nothing but build leftovers is removed,
// and a live package is not.
func TestPruneRemovesOnlyResidue(t *testing.T) {
	box := newCheckout(t)
	box.write("packages/live/src/index.ts", "export {}")
	box.write("packages/live/package.json", "{}")
	box.commit()

	// A package that still has sources must survive.
	box.write("packages/group/wip/src/index.ts", "export {}")
	box.write("packages/group/wip/package.json", "{}")
	box.commit()

	// What a deleted package leaves behind: only untracked build residue.
	box.write("packages/group/stale/node_modules/dep/index.js", "x")
	// A directory with an unrecognised untracked entry is never residue.
	box.write("packages/group/mixed/lib/index.js", "x")
	box.write("packages/group/mixed/README.md", "keep me")
	// vendor keeps its packages directly below the area root.
	box.write("vendor/oldpkg/lib/bundle.js", "x")

	got := box.candidatePaths()
	want := []string{"packages/group/stale", "vendor/oldpkg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates = %v, want %v", got, want)
	}

	report, err := box.repo.Prune(context.Background(), nil)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(report.Removed) != 2 || len(report.Failed) != 0 {
		t.Fatalf("report = %+v, want two removals", report)
	}
	for _, gone := range []string{"packages/group/stale", "vendor/oldpkg"} {
		if box.exists(gone) {
			t.Fatalf("%s was not removed", gone)
		}
	}
	for _, keep := range []string{"packages/live", "packages/group/wip", "packages/group/mixed/README.md"} {
		if !box.exists(keep) {
			t.Fatalf("%s was removed", keep)
		}
	}
}

// TestPruneNeverLeavesTheCheckout is the regression test for deleting through a
// symlinked packages directory: the removal followed the link and deleted a
// directory in an unrelated tree.
func TestPruneNeverLeavesTheCheckout(t *testing.T) {
	box := newCheckout(t)
	outside := filepath.Join(t.TempDir(), "outside-packages")
	victim := filepath.Join(outside, "core", "stale", "lib", "important.js")
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatalf("mkdir victim: %v", err)
	}
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	box.commit()
	if err := os.RemoveAll(filepath.Join(box.dir, "packages")); err != nil {
		t.Fatalf("clear packages: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(box.dir, "packages")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none behind a symlinked root", got)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a directory outside the checkout was deleted: %v", err)
	}
}

// TestPruneIgnoresASymlinkedPackageDirectory pins that a symlink inside the
// roots is never treated as residue, whatever it points at.
func TestPruneIgnoresASymlinkedPackageDirectory(t *testing.T) {
	box := newCheckout(t)
	box.mkdir("packages/group")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	box.mkdir("packages/group/real")
	box.write("packages/group/real/src/index.ts", "export {}")
	box.write("packages/group/real/package.json", "{}")
	if err := os.MkdirAll(filepath.Join(outside, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "lib", "keep.js"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	box.commit()
	if err := os.Symlink(outside, filepath.Join(box.dir, "packages", "group", "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a symlinked package", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "lib", "keep.js")); err != nil {
		t.Fatalf("the symlink target was modified: %v", err)
	}
}

// TestPruneKeepsTrackedPathsWithNonASCIICharacters is the regression test for
// reading git's tracked paths as its escaped form: the escaped name never
// matched the real directory, so a committed file was deleted.
//
// The fixture carries no package.json on purpose: the entry scan rejects a
// directory holding an unrecognised entry, so a manifest would save this
// directory even with the tracked lookup deleted, and the test would pin
// nothing.
func TestPruneKeepsTrackedPathsWithNonASCIICharacters(t *testing.T) {
	box := newCheckout(t)
	box.write("vendor/fóo/lib/index.js", "committed source")
	box.commit()
	// Residue appears next to the tracked source, as a cancelled build leaves it.
	box.write("vendor/fóo/node_modules/dep/index.js", "x")

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a tracked directory", got)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !box.exists("vendor/fóo/lib/index.js") {
		t.Fatal("a tracked file with a non-ASCII path was deleted")
	}
}

// TestPruneKeepsPathsWithSpacesAndQuotes pins the same literal comparison for
// the characters a shell or a quoted listing mangles: the candidate's directory
// name holds a space, a single quote and an ampersand, and a tracked file inside
// it is the only thing that can save it.
//
// The name carries a single quote rather than a double one because a Windows
// file name cannot contain a double quote at all: the platform cannot represent
// the path, so no test can stage it there. The fixture carries no package.json,
// for the reason given in TestPruneKeepsTrackedPathsWithNonASCIICharacters.
func TestPruneKeepsPathsWithSpacesAndQuotes(t *testing.T) {
	const weird = `vendor/weird 'na&me'`
	box := newCheckout(t)
	box.write(weird+"/lib/index.js", "committed source")
	box.commit()
	box.write(weird+"/node_modules/dep/index.js", "x")

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a tracked directory", got)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !box.exists(weird + "/lib/index.js") {
		t.Fatal("a tracked file whose directory name carries spaces and quotes was deleted")
	}
}

// TestPruneSurvivesGlobMetacharactersInTheRepositoryPath is the regression test
// for feeding the repository path to filepath.Glob: a repository whose own path
// contains a pattern character scanned a different tree.
func TestPruneSurvivesGlobMetacharactersInTheRepositoryPath(t *testing.T) {
	base := t.TempDir()
	// A sibling directory whose name the pattern would also match.
	// The temporary root itself is reached through a symlink on macOS, so
	// resolve it first: the point of this test is the bracket in the path, not
	// the platform's own aliasing.
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}
	sibling := filepath.Join(resolvedBase, "ga")
	box := newCheckoutIn(t, filepath.Join(resolvedBase, "g[a]"))
	if err := os.MkdirAll(filepath.Join(sibling, "packages", "victim", "lib"), 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	siblingVictim := filepath.Join(sibling, "packages", "victim", "lib", "keep.js")
	if err := os.WriteFile(siblingVictim, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	box.commit()
	box.write("packages/group/stale/node_modules/dep/index.js", "x")

	candidates := box.candidatePaths()
	if len(candidates) != 1 || candidates[0] != "packages/group/stale" {
		t.Fatalf("candidates = %v, want the repository's own residue", candidates)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(siblingVictim); err != nil {
		t.Fatalf("a sibling directory was pruned: %v", err)
	}
	if box.exists("packages/group/stale") {
		t.Fatal("the repository's own residue was not removed")
	}
}

// newCheckoutIn creates a checkout at a caller-chosen path.
func newCheckoutIn(t *testing.T, dir string) *checkout {
	t.Helper()
	requireGit(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	box := &checkout{t: t, dir: resolved}
	box.git("init", "-q", ".")
	box.git("config", "user.email", "fixture@example.com")
	box.git("config", "user.name", "fixture")
	box.git("config", "commit.gpgsign", "false")
	// The layout the tests exercise is stated here, the way a product would
	// state its own: the mechanism no longer knows it.
	box.repo = Repo{
		Dir:                  dir,
		Ex:                   fixtureRunner{runner: run.NewRunner()},
		ManifestRel:          "package.json",
		WorkspaceManifestRel: "pnpm-workspace.yaml",
		BuildRecordRel:       ".dsh-build/client-build-environment.json",
		Remote:               "origin",
		Branch:               "master",
		Residue: map[string]struct{}{
			"node_modules": {},
			"lib":          {},
			".typecheck":   {},
		},
		Areas: []PruneArea{{Name: "packages", Depth: 2}, {Name: "vendor", Depth: 1}},
	}
	box.write("package.json", "{}")
	box.write("pnpm-workspace.yaml", "packages:\n  - packages/*\n")
	return box
}

// TestPruneKeepsTheManifestOfAVendorArchive pins that a directory shipped by its
// own package.json is never residue, however little else it holds.
func TestPruneKeepsTheManifestOfAVendorArchive(t *testing.T) {
	box := newCheckout(t)
	box.write("vendor/archive/package.json", "{}")
	box.write("vendor/archive/lib/index.js", "shipped code")
	box.commit()
	// The archive also carries build output; the manifest still protects it.
	box.write("vendor/archive/node_modules/dep/index.js", "x")

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a directory with its own manifest", got)
	}
	if _, err := box.repo.Prune(context.Background(), nil); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !box.exists("vendor/archive/lib/index.js") {
		t.Fatal("a vendor archive was deleted")
	}
}

// TestPruneKeepsIgnoredSourceWithOnlyLib pins the boundary of the residue rule:
// a package whose lib is deliberately ignored still ships sources, so it is not
// residue even though git tracks nothing inside it.
func TestPruneKeepsIgnoredSourceWithOnlyLib(t *testing.T) {
	box := newCheckout(t)
	box.write("packages/group/ignored/lib/keep.js", "generated but intended")
	box.write("packages/group/ignored/.gitignore", "lib/\n")
	box.commit()
	// .gitignore is tracked, so the directory is tracked as well.
	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none for a tracked package", got)
	}
}

// TestPruneCandidatesRejectAnEmptyDirectory pins that an empty directory is not
// residue: there is nothing to reclaim, and removing it would be surprising.
func TestPruneCandidatesRejectAnEmptyDirectory(t *testing.T) {
	box := newCheckout(t)
	box.commit()
	box.mkdir("packages/group/empty/inner")

	if got := box.candidatePaths(); len(got) != 0 {
		t.Fatalf("candidates = %v, want none", got)
	}
}

// TestPruneCandidatesCoverTypecheckAndBuildInfo pins the residue entries a
// cancelled build leaves behind.
func TestPruneCandidatesCoverTypecheckAndBuildInfo(t *testing.T) {
	box := newCheckout(t)
	box.commit()
	box.write("packages/group/gone/.typecheck/state.json", "{}")
	box.write("packages/group/gone/tsconfig.tsbuildinfo", "{}")

	got := box.candidatePaths()
	if len(got) != 1 || got[0] != "packages/group/gone" {
		t.Fatalf("candidates = %v, want packages/group/gone", got)
	}
}

// TestIsServerCheckout pins the guard that keeps the destructive steps away from
// a directory that is not the managed checkout.
func TestIsServerCheckout(t *testing.T) {
	box := newCheckout(t)
	if !box.repo.IsServerCheckout() {
		t.Fatal("a checkout with both manifests must be accepted")
	}
	if err := os.Remove(filepath.Join(box.dir, "pnpm-workspace.yaml")); err != nil {
		t.Fatalf("remove workspace manifest: %v", err)
	}
	if box.repo.IsServerCheckout() {
		t.Fatal("a directory without the workspace manifest must be rejected")
	}
	if err := os.Remove(filepath.Join(box.dir, "package.json")); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	if box.repo.IsServerCheckout() {
		t.Fatal("a directory without a manifest must be rejected")
	}
}

// TestBuildReady pins the built-tree marker.
func TestBuildReady(t *testing.T) {
	box := newCheckout(t)
	if box.repo.BuildReady() {
		t.Fatal("an empty checkout is not built")
	}
	box.mkdir("node_modules")
	if box.repo.BuildReady() {
		t.Fatal("dependencies without the build record are not built")
	}
	box.write(".dsh-build/client-build-environment.json", "{}")
	if !box.repo.BuildReady() {
		t.Fatal("the record plus node_modules means built")
	}
	if box.repo.BuildRecordPath() != filepath.Join(box.dir, ".dsh-build", "client-build-environment.json") {
		t.Fatalf("BuildRecordPath = %q", box.repo.BuildRecordPath())
	}
}

// TestExistsIsGitAndNodeModules pins the small probes.
func TestExistsIsGitAndNodeModules(t *testing.T) {
	box := newCheckout(t)
	if !box.repo.Exists() || !box.repo.IsGit() {
		t.Fatalf("Exists=%v IsGit=%v, want both true", box.repo.Exists(), box.repo.IsGit())
	}
	if box.repo.NodeModulesPresent() {
		t.Fatal("node_modules should be absent")
	}
	box.mkdir("node_modules")
	if !box.repo.NodeModulesPresent() {
		t.Fatal("node_modules should be present")
	}
	missing := Repo{Dir: filepath.Join(box.dir, "nope"), Ex: run.NewRunner()}
	if missing.Exists() || missing.IsGit() || missing.IsServerCheckout() {
		t.Fatal("a missing directory must not report itself as present")
	}
}

// TestPruneFailsWhenGitCannotBeConsulted pins that an unanswered query is an
// error rather than an empty candidate list.
func TestPruneFailsWhenGitCannotBeConsulted(t *testing.T) {
	box := newCheckout(t)
	box.repo.Ex = failingExecutor{err: &run.ExitError{Command: "git ls-files", Code: 128}}
	if _, err := box.repo.PruneCandidates(context.Background()); err == nil {
		t.Fatal("a failing git must fail the candidate query")
	}
}

// failingExecutor reports a failing command for every invocation.
//
// It stands in for a git that cannot be consulted, which is a different failure
// from a git that answers with an empty list.
type failingExecutor struct{ err error }

// Run implements run.Executor.
func (f failingExecutor) Run(context.Context, run.Command) error { return f.err }

// Capture implements run.Capturer.
func (f failingExecutor) Capture(context.Context, run.Command) run.Result {
	return run.Result{Err: f.err}
}
