package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// requireGitForCLI fails the test when the real git binary is missing.
//
// The timeline tests run the real binary against a real repository, so a
// machine without git has nothing to assert here. Skipping instead would leave
// the fetch boundary unverified behind a green build.
func requireGitForCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("these tests need a real git executable: %v", err)
	}
}

// cliGit runs a git command with the environment neutralised the same way the
// repo package's fixture does: the test must never inspect, commit into or
// fetch from a repository the developer's environment points at.
func cliGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "core.hooksPath="}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = cliGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed (%v): %s", args, err, out)
	}
	return string(out)
}

// cliGitEnv returns the sanitised environment for the fixture's git commands.
func cliGitEnv() []string {
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
	return append(environment, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// toolPathWithGit returns a PATH that finds the stubbed tools and the real git.
//
// stubToolPath alone is enough for commands that never run git, but the version
// commands do: the stub PATH must carry git's own directory too, or the binary
// under test fails with "executable file not found" instead of the answer the
// test is about.
func toolPathWithGit(t *testing.T, programs ...string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("these tests need a real git executable: %v", err)
	}
	return filepath.Dir(gitPath) + string(os.PathListSeparator) + stubToolPath(t, programs...)
}

// seedGitCheckout builds the checkout the binary will manage: a real repository
// at <root>/repo whose origin/master is one commit and one tag ahead, so every
// timeline run has something to report.
func seedGitCheckout(t *testing.T, root string) {
	t.Helper()
	requireGitForCLI(t)
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	for name, content := range map[string]string{
		"package.json":        `{"name":"deepseek-harness"}`,
		"pnpm-workspace.yaml": "packages:\n  - packages/*/*\n",
	} {
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cliGit(t, repoDir, "init", "-q", ".")
	cliGit(t, repoDir, "symbolic-ref", "HEAD", "refs/heads/master")
	cliGit(t, repoDir, "config", "user.email", "fixture@example.com")
	cliGit(t, repoDir, "config", "user.name", "fixture")
	cliGit(t, repoDir, "config", "commit.gpgsign", "false")
	cliGit(t, repoDir, "add", "-A")
	cliGit(t, repoDir, "commit", "-qm", "the first commit")

	remote := filepath.Join(root, "remote.git")
	cliGit(t, root, "init", "--bare", "-q", remote)
	cliGit(t, repoDir, "remote", "add", "origin", remote)
	cliGit(t, repoDir, "push", "-q", "-u", "origin", "master")

	peer := filepath.Join(root, "peer")
	cliGit(t, root, "clone", "-q", remote, peer)
	cliGit(t, peer, "config", "user.email", "fixture@example.com")
	cliGit(t, peer, "config", "user.name", "fixture")
	cliGit(t, peer, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(peer, "next.txt"), []byte("next"), 0o600); err != nil {
		t.Fatalf("write next.txt: %v", err)
	}
	cliGit(t, peer, "add", "-A")
	cliGit(t, peer, "commit", "-qm", "the upstream commit")
	cliGit(t, peer, "tag", "dsh-v0.1.0")
	cliGit(t, peer, "push", "-q", "origin", "master", "--tags")
}

// worktreeSnapshot renders the checkout without its .git directory: a fetch
// writes remote-tracking refs there, and that is exactly what timeline is
// allowed to change.
func worktreeSnapshot(t *testing.T, repoDir string) []string {
	t.Helper()
	gitDir := filepath.Join(repoDir, ".git")
	var lines []string
	err := filepath.WalkDir(repoDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == gitDir || strings.HasPrefix(path, gitDir+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		line := relative + " " + info.Mode().String()
		if !entry.IsDir() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += " " + digest(contents)
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", repoDir, err)
	}
	sort.Strings(lines)
	return lines
}

// TestTimelineLeavesTheWorktreeAndStateDirUntouched pins the boundary the
// README documents: timeline may write .git's remote-tracking refs and nothing
// else — not one byte of the working tree, not the state directory.
func TestTimelineLeavesTheWorktreeAndStateDirUntouched(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	repoDir := filepath.Join(root, "repo")
	before := worktreeSnapshot(t, repoDir)

	result := runBinaryIn(t, root, nil, "timeline")
	if result.code != 0 {
		t.Fatalf("timeline exit = %d, want 0 (stderr = %s)", result.code, result.stderr)
	}
	if diff := firstTreeDifference(before, worktreeSnapshot(t, repoDir)); diff != "" {
		t.Fatalf("timeline changed the working tree:\n%s", diff)
	}
	if _, err := os.Lstat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatalf("timeline created the state directory: %v", err)
	}
	// The fetch is the documented exception, and it has to have happened:
	// without it the report would describe a stale remote.
	if _, err := os.Stat(filepath.Join(repoDir, ".git", "FETCH_HEAD")); err != nil {
		t.Fatalf("the fetch did not run: %v", err)
	}
	for _, want := range []string{
		"checkout: " + repoDir,
		"gap: 1 commits behind",
		"dsh-v0.1.0",
		"← remote tip",
		"← current",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("stdout = %q, want it to contain %q", result.stdout, want)
		}
	}
}

// TestTimelineJSONIsConsumable pins the machine-readable shape a script reads.
func TestTimelineJSONIsConsumable(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	repoDir := filepath.Join(root, "repo")

	result := runBinaryIn(t, root, nil, "timeline", "--json")
	if result.code != 0 {
		t.Fatalf("timeline --json exit = %d, want 0 (stderr = %s)", result.code, result.stderr)
	}
	var report struct {
		RepoDir string `json:"repoDir"`
		Fetched bool   `json:"fetched"`
		Current struct {
			Commit string `json:"commit"`
			Branch string `json:"branch"`
			Short  string `json:"shortCommit"`
		} `json:"current"`
		Remote struct {
			Commit string `json:"commit"`
			Name   string `json:"name"`
		} `json:"remote"`
		Behind  int `json:"behind"`
		Ahead   int `json:"ahead"`
		Commits []struct {
			Commit  string   `json:"commit"`
			Subject string   `json:"subject"`
			Tags    []string `json:"tags"`
			Current bool     `json:"current"`
			Remote  bool     `json:"remote"`
		} `json:"commits"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatalf("timeline --json is not JSON: %v\n%s", err, result.stdout)
	}
	if report.RepoDir != repoDir || !report.Fetched {
		t.Fatalf("report = %+v, want the fetched checkout", report)
	}
	if report.Current.Commit == "" || report.Current.Short == "" || report.Current.Branch != "master" {
		t.Fatalf("current = %+v, want a named master commit", report.Current)
	}
	if report.Remote.Commit == "" || report.Remote.Name != "origin/master" {
		t.Fatalf("remote = %+v, want origin/master", report.Remote)
	}
	if report.Behind != 1 || report.Ahead != 0 {
		t.Fatalf("gap = (%d behind, %d ahead), want one behind", report.Behind, report.Ahead)
	}
	if len(report.Commits) != 2 {
		t.Fatalf("commits = %+v, want the upstream commit and the current one", report.Commits)
	}
	if !report.Commits[0].Remote || report.Commits[0].Subject != "the upstream commit" {
		t.Fatalf("first row = %+v, want the remote tip", report.Commits[0])
	}
	if !report.Commits[1].Current {
		t.Fatalf("last row = %+v, want the current position", report.Commits[1])
	}
}

// TestUpdateRejectsAnUnknownVersionWithoutStoppingTheService pins the order of
// checks at the command line: a typo is a preflight failure, reported before
// anything is stopped or fetched into place.
func TestUpdateRejectsAnUnknownVersionWithoutStoppingTheService(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	result := runBinaryIn(t, root, map[string]string{
		"PATH": toolPathWithGit(t, "pnpm", "node"),
	}, "update", "no-such-version")

	if result.code != 4 {
		t.Fatalf("update exit = %d, want 4 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "no-such-version") {
		t.Fatalf("stderr = %q, want it to name the selector", result.stderr)
	}
	if strings.Contains(result.stdout, "DSH Web is running; stopping it first") || strings.Contains(result.stdout, "update finished") {
		t.Fatalf("stdout = %q, want no deployment attempt", result.stdout)
	}
}

// TestUpdateRejectsASelectorThatLooksLikeAFlag pins the boundary against
// selectors that would otherwise be read by the flag parser or by git.
func TestUpdateRejectsASelectorThatLooksLikeAFlag(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	result := runBinaryIn(t, root, map[string]string{
		"PATH": toolPathWithGit(t, "pnpm", "node"),
	}, "update", "--", "--latest")

	if result.code != 2 {
		t.Fatalf("update exit = %d, want 2 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "'s version argument is invalid") {
		t.Fatalf("stderr = %q, want the invalid-selector message", result.stderr)
	}
}

// TestUpdateRejectsASecondVersionArgument pins that the command takes one
// version, not a list.
func TestUpdateRejectsASecondVersionArgument(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	result := runBinaryIn(t, root, map[string]string{
		"PATH": toolPathWithGit(t, "pnpm", "node"),
	}, "update", "latest", "dsh-v0.1.0")

	if result.code != 2 {
		t.Fatalf("update exit = %d, want 2 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "takes one version argument") {
		t.Fatalf("stderr = %q, want the extra-argument message", result.stderr)
	}
}

// TestRollbackRejectsBadStepArguments pins the usage boundary of the step
// syntax: a flag cannot be combined with a version, and the step count is a
// positive integer.
func TestRollbackRejectsBadStepArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"zero steps", []string{"rollback", "-n", "0"}, "rollback's -n must be a positive number"},
		{"negative steps", []string{"rollback", "-n", "-2"}, "rollback's -n must be a positive number"},
		{"steps and a version", []string{"rollback", "-n", "2", "dsh-v0.1.0"}, "rollback's -n and a version cannot be used together"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			result := runBinaryIn(t, root, map[string]string{
				"PATH": toolPathWithGit(t, "pnpm", "node"),
			}, testCase.args...)
			if result.code != 2 {
				t.Fatalf("%v exit = %d, want 2 (stderr = %s)", testCase.args, result.code, result.stderr)
			}
			if !strings.Contains(result.stderr, testCase.want) {
				t.Fatalf("stderr = %q, want it to contain %q", result.stderr, testCase.want)
			}
			if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
				t.Fatalf("%v created the state directory", testCase.args)
			}
		})
	}
}

// TestRollbackWithoutHistoryIsAPreflight pins the exit code a script branches
// on when there is nothing recorded to return to.
func TestRollbackWithoutHistoryIsAPreflight(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	result := runBinaryIn(t, root, map[string]string{
		"PATH": toolPathWithGit(t, "pnpm", "node"),
	}, "rollback")

	if result.code != 4 {
		t.Fatalf("rollback exit = %d, want 4 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "nothing to roll back to: dshctl has not recorded a position for this checkout yet") {
		t.Fatalf("stderr = %q, want the missing-history message", result.stderr)
	}
}

// TestUpdateThenRollbackThroughTheRealBinary pins the whole operator workflow
// against a real repository: deploy a tag, roll back one step, and read the
// history back. The service tests exercise the same path against a fictional
// machine; this one proves the command line wires it together.
func TestUpdateThenRollbackThroughTheRealBinary(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	repoDir := filepath.Join(root, "repo")
	before := worktreeSnapshot(t, repoDir)
	original := strings.TrimSpace(cliGit(t, repoDir, "rev-parse", "HEAD"))
	env := map[string]string{"PATH": toolPathWithGit(t, "pnpm", "node")}

	update := runBinaryIn(t, root, env, "update", "dsh-v0.1.0")
	if update.code != 0 {
		t.Fatalf("update exit = %d, want 0 (stderr = %s)", update.code, update.stderr)
	}
	tagged := strings.TrimSpace(cliGit(t, repoDir, "rev-parse", "HEAD"))
	if tagged == original {
		t.Fatal("update did not move the checkout to the tag")
	}
	if !strings.Contains(update.stdout, "update finished") {
		t.Fatalf("update stdout = %q, want the completion report", update.stdout)
	}

	rollback := runBinaryIn(t, root, env, "rollback")
	if rollback.code != 0 {
		t.Fatalf("rollback exit = %d, want 0 (stderr = %s)", rollback.code, rollback.stderr)
	}
	if got := strings.TrimSpace(cliGit(t, repoDir, "rev-parse", "HEAD")); got != original {
		t.Fatalf("head = %q, want the original commit %q", got, original)
	}
	if diff := firstTreeDifference(before, worktreeSnapshot(t, repoDir)); diff != "" {
		t.Fatalf("the cycle changed the working tree:\n%s", diff)
	}
	if !strings.Contains(rollback.stdout, "roll back finished") {
		t.Fatalf("rollback stdout = %q, want the completion report", rollback.stdout)
	}

	timeline := runBinaryIn(t, root, env, "timeline")
	if timeline.code != 0 {
		t.Fatalf("timeline exit = %d, want 0 (stderr = %s)", timeline.code, timeline.stderr)
	}
	for _, want := range []string{"deployment history", "-n 1", "← current"} {
		if !strings.Contains(timeline.stdout, want) {
			t.Fatalf("timeline stdout = %q, want it to contain %q", timeline.stdout, want)
		}
	}
}

// TestUpdateHEADIsANoOpThroughTheRealBinary pins the selector edge at the
// command line: HEAD names the commit the checkout is already at, so nothing is
// installed, built or moved.
func TestUpdateHEADIsANoOpThroughTheRealBinary(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	repoDir := filepath.Join(root, "repo")
	before := worktreeSnapshot(t, repoDir)
	head := strings.TrimSpace(cliGit(t, repoDir, "rev-parse", "HEAD"))

	result := runBinaryIn(t, root, map[string]string{
		"PATH": toolPathWithGit(t, "pnpm", "node"),
	}, "update", "HEAD")
	if result.code != 0 {
		t.Fatalf("update HEAD exit = %d, want 0 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stdout, "nothing to update") {
		t.Fatalf("stdout = %q, want the no-op report", result.stdout)
	}
	if strings.Contains(result.stdout, "pnpm install") {
		t.Fatalf("stdout = %q, want no install for a no-op", result.stdout)
	}
	if got := strings.TrimSpace(cliGit(t, repoDir, "rev-parse", "HEAD")); got != head {
		t.Fatalf("head = %q, want it untouched at %q", got, head)
	}
	if diff := firstTreeDifference(before, worktreeSnapshot(t, repoDir)); diff != "" {
		t.Fatalf("a no-op update changed the working tree:\n%s", diff)
	}
}

// TestTimelineFailsPreflightOutsideACheckout pins the exit code a script
// branches on when the configured path is not a repository at all.
func TestTimelineFailsPreflightOutsideACheckout(t *testing.T) {
	root := t.TempDir()
	result := runBinaryIn(t, root, nil, "timeline")
	if result.code != 4 {
		t.Fatalf("timeline exit = %d, want 4 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "the checkout does not exist") {
		t.Fatalf("stderr = %q, want the missing-checkout message", result.stderr)
	}
}

// TestTimelineExitsPreflightWhenFetchFails pins the honesty contract at the
// command line: the locally known report is printed, the remote is named as
// unknown, "up to date (" never appears, and the exit code says the preflight
// failed.
func TestTimelineExitsPreflightWhenFetchFails(t *testing.T) {
	root := t.TempDir()
	seedGitCheckout(t, root)
	repoDir := filepath.Join(root, "repo")
	cliGit(t, repoDir, "remote", "set-url", "origin", filepath.Join(root, "missing.git"))

	result := runBinaryIn(t, root, nil, "timeline")
	if result.code != 4 {
		t.Fatalf("timeline exit = %d, want 4 (stderr = %s)", result.code, result.stderr)
	}
	if !strings.Contains(result.stdout, "remote: unavailable (") {
		t.Fatalf("stdout = %q, want the unknown-remote header", result.stdout)
	}
	if strings.Contains(result.stdout, "up to date (") {
		t.Fatalf("stdout = %q, must not claim to be up to date", result.stdout)
	}
	if !strings.Contains(result.stderr, "the remote could not be fetched") {
		t.Fatalf("stderr = %q, want the fetch warning", result.stderr)
	}
}
