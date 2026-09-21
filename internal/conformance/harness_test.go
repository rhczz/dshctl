package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// update rewrites the golden files from the candidate binary. The conformance
// job runs it against a build of the reference tag and fails on any diff, which
// is what keeps a golden from being edited into agreement with a behavior
// change.
var update = flag.Bool("update", false, "rewrite the golden files from this run")

var (
	candidateBinary string
	stubDir         string
	// toolchainDir keeps the nested builds' bookkeeping inside the harness's
	// own temporary directory. The toolchain creates $HOME/go (its default
	// GOPATH) for a build, and a test that leaves that in somebody's home
	// directory is exactly what the hermetic check exists to catch.
	toolchainDir string
)

// buildEnv is the environment every nested build runs with: no proxy, the local
// toolchain, and caches pointed at the harness's own directory.
func buildEnv() []string {
	return append(os.Environ(),
		"GOPROXY=off",
		"GOFLAGS=-trimpath",
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
		"GOPATH="+filepath.Join(toolchainDir, "gopath"),
		"GOCACHE="+filepath.Join(toolchainDir, "gocache"),
		"GOMODCACHE="+filepath.Join(toolchainDir, "gomodcache"),
	)
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conformance: %v\n", err)
		return 1
	}
	dir, err := os.MkdirTemp("", "dshctl-conformance-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "conformance: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)
	toolchainDir = filepath.Join(dir, "toolchain")
	if err := os.MkdirAll(toolchainDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "conformance: %v\n", err)
		return 1
	}
	if stubDir, err = buildStubDir(dir); err != nil {
		fmt.Fprintf(os.Stderr, "conformance: %v\n", err)
		return 1
	}
	candidateBinary = filepath.Join(dir, "dshctl"+exeSuffix())
	build := exec.Command("go", "build", "-o", candidateBinary, "./cmd/dshctl")
	build.Dir = root
	build.Env = buildEnv()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "conformance: building the candidate failed: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

func moduleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(wd, "..", ".."))
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// fixture is one scenario's world: a temporary home, a state directory under it,
// an empty bin directory on PATH, and optionally a git checkout.
type fixture struct {
	t        *testing.T
	root     string
	stateDir string
	home     string
	repoDir  string
	bin      string
	extra    []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:        t,
		root:     root,
		stateDir: filepath.Join(root, "state"),
		home:     filepath.Join(root, "home"),
		repoDir:  filepath.Join(root, "repo"),
		bin:      filepath.Join(root, "bin"),
	}
	for _, dir := range []string{f.home, f.bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	return f
}

func (f *fixture) setenv(key, value string) { f.extra = append(f.extra, key+"="+value) }

func (f *fixture) mkdirState() {
	f.t.Helper()
	if err := os.MkdirAll(f.stateDir, 0o700); err != nil {
		f.t.Fatalf("creating the state directory: %v", err)
	}
}

func (f *fixture) write(relative, body string) {
	f.t.Helper()
	path := filepath.Join(f.stateDir, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		f.t.Fatalf("writing %s: %v", path, err)
	}
}

// checkout creates a server checkout with one commit and no origin, which is
// the state timeline and update report on.
func (f *fixture) checkout() {
	f.t.Helper()
	if err := os.MkdirAll(f.repoDir, 0o755); err != nil {
		f.t.Fatalf("creating the checkout: %v", err)
	}
	f.writeRepoFile("package.json", "{\"name\":\"deepseek-harness\"}\n")
	f.writeRepoFile("pnpm-workspace.yaml", "packages: []\n")
	f.git("init", "-q")
	f.git("symbolic-ref", "HEAD", "refs/heads/master")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "the first commit")
}

func (f *fixture) writeRepoFile(name, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.repoDir, name), []byte(body), 0o644); err != nil {
		f.t.Fatalf("writing %s: %v", name, err)
	}
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.repoDir
	cmd.Env = append(gitEnv(f.root), "GIT_CONFIG_NOSYSTEM=1", "HOME="+f.home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitEnv fixes the identity and the clocks so a fixture repository has the same
// object ids on every machine and every run.
func gitEnv(root string) []string {
	return []string{
		"GIT_AUTHOR_NAME=conformance",
		"GIT_AUTHOR_EMAIL=conformance@example.invalid",
		"GIT_COMMITTER_NAME=conformance",
		"GIT_COMMITTER_EMAIL=conformance@example.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"TMPDIR=" + root,
	}
}

type result struct {
	Exit   int
	Stdout string
	Stderr string
}

// run executes the candidate binary in the fixture's environment.
func (f *fixture) run(args []string, probes, realPATH bool) result {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, candidateBinary, args...)
	cmd.Env = f.environment(probes, realPATH)
	cmd.Dir = f.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			f.t.Fatalf("running %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return result{Exit: code, Stdout: normalize(f.root, stdout.String()), Stderr: normalize(f.root, stderr.String())}
}

// environment builds the whole environment from scratch: every dshctl variable
// the surrounding shell may carry is dropped, so a scenario sees its fixture and
// nothing else.
func (f *fixture) environment(probes, realPATH bool) []string {
	entries := []string{}
	if probes {
		entries = append(entries, stubDir)
	}
	entries = append(entries, f.bin)
	if realPATH {
		entries = append(entries, os.Getenv("PATH"))
	}
	path := strings.Join(entries, string(os.PathListSeparator))
	env := make([]string, 0, 24)
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		switch {
		case strings.HasPrefix(key, "DSH"), strings.HasPrefix(key, "DSHCTL"):
			continue
		case key == "PATH", key == "HOME", key == "USERPROFILE", key == "LC_ALL", key == "LANG":
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+f.home,
		"USERPROFILE="+f.home,
		"PATH="+path,
		"DSHCTL_STATE_DIR="+f.stateDir,
		// The goldens were recorded from v0.2.5, whose text is Chinese. Pinning
		// the language keeps the oracle about behavior rather than about which
		// language the machine happens to prefer.
		"DSHCTL_LANG=zh",
		"LC_ALL=C",
		"LANG=C",
	)
	return append(env, f.extra...)
}

// stubTool installs a stub of its own into this fixture's bin directory, which
// is first on PATH. A scenario that needs node or pnpm resolves must not depend
// on what the machine running the suite happens to have installed.
func (f *fixture) stubTool(name string) {
	f.t.Helper()
	source := filepath.Join(stubDir, stubTools[0]+exeSuffix())
	if err := copyFile(source, filepath.Join(f.bin, name+exeSuffix())); err != nil {
		f.t.Fatalf("installing a %s stub: %v", name, err)
	}
}

// snapshot records everything the run left under the fixture root: directories
// (with their permissions), files (with permissions and a hash of their
// normalized contents). A read-only command that writes anything shows up here.
func (f *fixture) snapshot() []string {
	f.t.Helper()
	var entries []string
	err := filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if hasGitComponent(relative) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		slashed := filepath.ToSlash(relative)
		if strings.HasPrefix(slashed, "bin/") && !entry.IsDir() {
			// The stub tools are the harness's own furniture. Their bytes are a
			// property of the machine that compiled them, so the snapshot records
			// that the tool exists and its mode instead of its hash.
			entries = append(entries, fmt.Sprintf("%s|%04o|stub", slashed, info.Mode().Perm()))
			return nil
		}
		if entry.IsDir() {
			entries = append(entries, fmt.Sprintf("%s/|%04o|dir", slashed, info.Mode().Perm()))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(normalizeFile(f.root, slashed, data)))
		entries = append(entries, fmt.Sprintf("%s|%04o|%x", slashed, info.Mode().Perm(), sum[:12]))
		return nil
	})
	if err != nil {
		f.t.Fatalf("snapshotting %s: %v", f.root, err)
	}
	sort.Strings(entries)
	return entries
}

// normalizeFile applies the output normalizers to a file's contents. The lock
// file gets one extra rule: its contents are a bare pid, a documented diagnostic
// record, and the pid changes on every run by construction.
func normalizeFile(root, relative string, data []byte) string {
	text := normalize(root, string(data))
	if strings.HasSuffix(relative, ".lock") {
		text = barePID.ReplaceAllString(text, "<PID>")
	}
	return text
}

var barePID = regexp.MustCompile(`[0-9]+`)

func hasGitComponent(relative string) bool {
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

// refs records what the checkout points at after the run.
func (f *fixture) refs() []string {
	f.t.Helper()
	env := append(os.Environ(), gitEnv(f.root)...)
	env = append(env, "HOME="+f.home, "GIT_CONFIG_NOSYSTEM=1")
	out := f.gitWith(env, "for-each-ref", "--format=%(refname) %(objectname)")
	lines := []string{}
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if head := f.gitWith(env, "rev-parse", "HEAD"); head != "" {
		lines = append(lines, "HEAD "+head)
	}
	sort.Strings(lines)
	return lines
}

func (f *fixture) gitWith(env []string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.repoDir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

var normalizers = []struct {
	pattern *regexp.Regexp
	replace string
}{
	// Microseconds and timezone offsets both appear in the state records.
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`), "<TS>"},
	{regexp.MustCompile(`"spawnedPid":(\s*)\d+`), `"spawnedPid":${1}<PID>`},
	{regexp.MustCompile(`"pid":(\s*)\d+`), `"pid":${1}<PID>`},
	{regexp.MustCompile(`(?i)\bpid \d+`), "pid <PID>"},
	{regexp.MustCompile(`耗时 \S+`), "耗时 <DUR>"},
	{regexp.MustCompile(`dshctl \S+ \(`), "dshctl <V> ("},
	{regexp.MustCompile(`commit [^,)]+, built [^)]*`), "commit <C>, built <B>"},
	{regexp.MustCompile(`"version":(\s*)"[^"]*"`), `"version":${1}"<V>"`},
	{regexp.MustCompile(`"commit":(\s*)"[^"]*"`), `"commit":${1}"<C>"`},
	{regexp.MustCompile(`"buildDate":(\s*)"[^"]*"`), `"buildDate":${1}"<B>"`},
}

// normalize replaces the paths that differ between runs and the fields that
// cannot be recorded — timestamps, pids, the build stamp — with placeholders.
// Everything else survives, so a changed message or a changed file is a diff.
func normalize(root, text string) string {
	for _, prefix := range []string{root, filepath.Dir(candidateBinary), os.TempDir()} {
		if prefix == "" {
			continue
		}
		text = strings.ReplaceAll(text, prefix, "<PATH>")
		text = strings.ReplaceAll(text, filepath.ToSlash(prefix), "<PATH>")
	}
	for _, rule := range normalizers {
		text = rule.pattern.ReplaceAllString(text, rule.replace)
	}
	return text
}

type expectation struct {
	Exit   int      `json:"exit"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Files  []string `json:"files"`
	Refs   []string `json:"refs,omitempty"`
}

func goldenPath() string {
	return filepath.Join("testdata", "expected-"+runtime.GOOS+".json")
}

func loadGoldens(t *testing.T) map[string]expectation {
	t.Helper()
	data, err := os.ReadFile(goldenPath())
	if err != nil {
		t.Fatalf("reading the goldens: %v (run with -update to record them)", err)
	}
	goldens := map[string]expectation{}
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatalf("parsing the goldens: %v", err)
	}
	return goldens
}

func saveGoldens(t *testing.T, goldens map[string]expectation) {
	t.Helper()
	data, err := json.MarshalIndent(goldens, "", "  ")
	if err != nil {
		t.Fatalf("rendering the goldens: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(goldenPath()), 0o755); err != nil {
		t.Fatalf("creating testdata: %v", err)
	}
	if err := os.WriteFile(goldenPath(), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writing the goldens: %v", err)
	}
}

func compare(t *testing.T, want, got expectation) {
	t.Helper()
	if want.Exit != got.Exit {
		t.Errorf("exit code: want %d, got %d\nstdout:\n%s\nstderr:\n%s", want.Exit, got.Exit, got.Stdout, got.Stderr)
	}
	if want.Stdout != got.Stdout {
		t.Errorf("stdout changed:\n--- want ---\n%s\n--- got ---\n%s", want.Stdout, got.Stdout)
	}
	if want.Stderr != got.Stderr {
		t.Errorf("stderr changed:\n--- want ---\n%s\n--- got ---\n%s", want.Stderr, got.Stderr)
	}
	if diff := linesDiff(want.Files, got.Files); diff != "" {
		t.Errorf("files changed:\n%s", diff)
	}
	if diff := linesDiff(want.Refs, got.Refs); diff != "" {
		t.Errorf("git refs changed:\n%s", diff)
	}
}

// linesDiff renders the set difference of two sorted lists, so a failure names
// the file that appeared or disappeared instead of dumping both lists.
func linesDiff(want, got []string) string {
	inWant := map[string]bool{}
	for _, line := range want {
		inWant[line] = true
	}
	inGot := map[string]bool{}
	for _, line := range got {
		inGot[line] = true
	}
	var b strings.Builder
	for _, line := range want {
		if !inGot[line] {
			fmt.Fprintf(&b, "  - %s\n", line)
		}
	}
	for _, line := range got {
		if !inWant[line] {
			fmt.Fprintf(&b, "  + %s\n", line)
		}
	}
	return b.String()
}
