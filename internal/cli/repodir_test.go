//go:build unix

package cli

// These are the end-to-end rows of the checkout decision table: the real binary,
// a real detached server, a real state directory and a real settings document,
// driven the way an operator drives them — one command per process.
//
// They live in a Unix-only file for the same reason the two-port test does: the
// server they start is a shell script that binds a port. Everything they pin is
// about the wiring from the command line to the settings document, which is the
// layer no in-process test can reach: the flag, the environment and the file are
// resolved by loadSettings, and only the real binary proves which one reached the
// checkout that actually ran.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
)

// cliWorld is one installation: a checkout, a home, a state directory, a
// PATH that serves a node and a pnpm the checkout can run with, and a port.
type cliWorld struct {
	t        *testing.T
	root     string
	home     string
	stateDir string
	checkout string
	port     int
	// variables are the extra environment variables every invocation carries.
	variables map[string]string
}

// newCLIWorld builds the installation, including a second checkout a test can
// point the configuration at.
func newCLIWorld(t *testing.T) *cliWorld {
	t.Helper()
	realNode, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	root := t.TempDir()
	port := freePort(t)
	world := &cliWorld{
		t:        t,
		root:     root,
		home:     filepath.Join(root, "home"),
		stateDir: filepath.Join(root, "state"),
		port:     port,
		// DSH_REPO_DIR is deliberately absent: which layer names the checkout is
		// what these rows are about, and a variable set for every invocation —
		// as the shared binary harness does — would decide every case. The port
		// is the opposite: it belongs to this world, so the value the shared
		// harness invents for one call cannot point the next call at an empty
		// port.
		variables: map[string]string{
			"DSH_REPO_DIR": "",
			"DSH_PORT":     strconv.Itoa(port),
		},
	}
	if err := os.MkdirAll(world.home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	world.checkout = world.makeCheckout("checkout")

	// A server the pnpm stub starts: it binds the port and announces the address
	// the way `dsh web` does, which is what a start waits for.
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	server := filepath.Join(root, "server.js")
	writeFile(t, server, `
const net = require('net');
const port = Number(process.argv[process.argv.indexOf('--port') + 1]);
console.log('dsh web: http://127.0.0.1:' + port + '/?token=PORT-' + port);
net.createServer(() => {}).listen(port, '127.0.0.1', () => console.log('listening'));
setInterval(() => {}, 1000);
`)
	node := filepath.Join(binDir, "node")
	writeFile(t, node, "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"  -v) echo v"+config.TestedNodeVersion+"; exit 0;;\n"+
		"  -p) echo \""+node+"\"; exit 0;;\n"+
		"esac\n"+
		"exec \""+realNode+"\" \"$@\"\n")
	if err := os.Chmod(node, 0o755); err != nil {
		t.Fatalf("chmod the node stub: %v", err)
	}
	pnpm := filepath.Join(binDir, "pnpm")
	writeFile(t, pnpm, "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"  --version) echo 11.0.0; exit 0;;\n"+
		"esac\n"+
		"exec "+node+" "+server+" \"$@\"\n")
	if err := os.Chmod(pnpm, 0o755); err != nil {
		t.Fatalf("chmod the pnpm stub: %v", err)
	}
	world.variables["PATH"] = binDir + string(os.PathListSeparator) + systemToolDirs

	// The server is detached: whatever the test asserts, the machine has to be
	// left as it was found.
	t.Cleanup(func() { world.run("stop") })
	return world
}

// makeCheckout builds a second checkout inside the world and returns its path.
func (w *cliWorld) makeCheckout(name string) string {
	w.t.Helper()
	dir := filepath.Join(w.root, name)
	writeFile(w.t, filepath.Join(dir, "package.json"), `{"name":"deepseek-harness"}`)
	writeFile(w.t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - packages/*/*\n")
	writeFile(w.t, filepath.Join(dir, "node_modules", ".keep"), "")
	writeFile(w.t, filepath.Join(dir, ".dsh-build", "client-build-environment.json"), "{}")
	initCheckoutGit(w.t, dir)
	return dir
}

// guess is the checkout the built-in default assumes on this machine.
func (w *cliWorld) guess() string { return config.DefaultRepoDir(w.home) }

// run executes the real binary with this world's environment.
func (w *cliWorld) run(args ...string) invocation {
	w.t.Helper()
	environment := binaryEnvironment(w.t, w.root, w.home, w.stateDir)
	for key, value := range w.variables {
		environment = setEnvironment(environment, key, value)
	}
	cmd := exec.Command(binary(w.t), args...)
	cmd.Dir = w.root
	cmd.Env = environment
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !asExitError(err, &exit) {
			w.t.Fatalf("running %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return invocation{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// document writes the settings document this world starts from.
func (w *cliWorld) document(fields map[string]any) {
	w.t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		w.t.Fatalf("encode the document: %v", err)
	}
	if err := os.MkdirAll(w.stateDir, 0o700); err != nil {
		w.t.Fatalf("mkdir the state directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.stateDir, config.ConfigFileName), append(data, '\n'), 0o600); err != nil {
		w.t.Fatalf("write the settings document: %v", err)
	}
}

// settingsDocument reads the settings document back.
func (w *cliWorld) settingsDocument() map[string]any {
	w.t.Helper()
	return readSettingsDocument(w.t, w.stateDir)
}

// TestACheckoutNamedForOneRunIsTheCheckoutEveryLaterCommandUses is the
// end-to-end regression test for the reported sequence: a settings document left
// behind by an older build names the built-in guess, the operator starts the
// service with --repo, and every later command — a separate process, with no
// flags at all — must operate on the checkout that is actually serving.
//
// Before the fix the document kept the guess, so `doctor` reported three
// failures about a directory nobody had chosen and `stop` was the only command
// that still described reality.
func TestACheckoutNamedForOneRunIsTheCheckoutEveryLaterCommandUses(t *testing.T) {
	w := newCLIWorld(t)
	w.document(map[string]any{"repoDir": w.guess()})

	start := w.run("--repo", w.checkout, "start")
	if start.code != 0 {
		t.Fatalf("start exit = %d\nstdout=%s\nstderr=%s", start.code, start.stdout, start.stderr)
	}
	if !strings.Contains(start.stdout, "已将仓库目录 "+w.checkout+" 写入配置") {
		t.Fatalf("start did not report the recorded checkout:\n%s", start.stdout)
	}
	if got := w.settingsDocument()["repoDir"]; got != w.checkout {
		t.Fatalf("settings document = %v, want repoDir %q", w.settingsDocument(), w.checkout)
	}

	// A second process with no overrides at all: this is where the reported bug
	// showed up.
	status := w.run("status")
	if status.code != 0 {
		t.Fatalf("status exit = %d\nstdout=%s\nstderr=%s", status.code, status.stdout, status.stderr)
	}
	if !strings.Contains(status.stdout, "仓库: "+w.checkout) {
		t.Fatalf("status does not name the checkout that serves:\n%s", status.stdout)
	}
	if strings.Contains(status.stdout, w.guess()) {
		t.Fatalf("status still names the stale guess:\n%s", status.stdout)
	}

	doctor := w.run("doctor")
	if doctor.code != 0 {
		t.Fatalf("doctor exit = %d, want a healthy installation\n%s", doctor.code, doctor.stdout)
	}
	// A healthy checkout is reported by its revision, and the rows that name a
	// path always name the checkout this run resolved.
	for _, want := range []string{
		filepath.Join(w.checkout, "node_modules") + " 已安装",
		filepath.Join(w.checkout, ".dsh-build", "client-build-environment.json"),
	} {
		if !strings.Contains(doctor.stdout, want) {
			t.Fatalf("doctor does not report %q:\n%s", want, doctor.stdout)
		}
	}
	if strings.Contains(doctor.stdout, w.guess()) {
		t.Fatalf("doctor still reports the stale guess:\n%s", doctor.stdout)
	}

	// And the verbose echo names the layer the value came from.
	verbose := w.run("-v", "doctor")
	if !strings.Contains(verbose.stderr, "仓库目录: "+w.checkout+" (file)") {
		t.Fatalf("verbose output does not name the file as the source:\n%s", verbose.stderr)
	}
}

// TestADecidedCheckoutSurvivesAOneOffOverride pins the operator's side of the
// rule end to end: a document that names its own checkout is never rewritten by
// a run that used another one, and the run says which checkout it used.
func TestADecidedCheckoutSurvivesAOneOffOverride(t *testing.T) {
	w := newCLIWorld(t)
	decided := w.makeCheckout("decided-checkout")
	w.document(map[string]any{"repoDir": decided})

	start := w.run("--repo", w.checkout, "start")
	if start.code != 0 {
		t.Fatalf("start exit = %d\nstdout=%s\nstderr=%s", start.code, start.stdout, start.stderr)
	}
	if !strings.Contains(start.stdout, "本次使用仓库 "+w.checkout) ||
		!strings.Contains(start.stdout, "配置中为 "+decided) {
		t.Fatalf("start did not report which checkout it used:\n%s", start.stdout)
	}
	if got := w.settingsDocument()["repoDir"]; got != decided {
		t.Fatalf("settings document = %v, want the operator's %q untouched", w.settingsDocument(), decided)
	}

	// The next command, with no overrides, follows the operator's document.
	verbose := w.run("-v", "doctor")
	if !strings.Contains(verbose.stderr, "仓库目录: "+decided+" (file)") {
		t.Fatalf("verbose output does not follow the document:\n%s", verbose.stderr)
	}
}

// TestTheRepeatedGuessIsNotADecision pins the repair rule from outside: a
// document that spells out the built-in default is read as the default, which is
// what the verbose output says and what leaves the override layers in charge.
func TestTheRepeatedGuessIsNotADecision(t *testing.T) {
	w := newCLIWorld(t)
	w.document(map[string]any{"repoDir": w.guess()})

	plain := w.run("-v", "doctor")
	if !strings.Contains(plain.stderr, "仓库目录: "+w.guess()+" ("+config.SourceRepoDirRepeatsDefault+")") {
		t.Fatalf("verbose output does not explain the repeated guess:\n%s", plain.stderr)
	}

	w.variables["DSH_REPO_DIR"] = w.checkout
	fromEnv := w.run("-v", "doctor")
	if !strings.Contains(fromEnv.stderr, "仓库目录: "+w.checkout+" (env)") {
		t.Fatalf("the environment did not win over the repeated guess:\n%s", fromEnv.stderr)
	}

	flag := w.run("-v", "--repo", w.checkout, "doctor")
	if !strings.Contains(flag.stderr, "仓库目录: "+w.checkout+" (flag)") {
		t.Fatalf("the flag did not win over the repeated guess:\n%s", flag.stderr)
	}
}

// TestReportingCommandsLeaveAnExistingDocumentAlone pins the side-effect
// boundary against a document that already exists: a reporting command reads, it
// does not write. The service-level rows compare the bytes around the calls; this
// one does it for the real binary, which is where a write-back wired into the
// command line itself would show up.
func TestReportingCommandsLeaveAnExistingDocumentAlone(t *testing.T) {
	w := newCLIWorld(t)
	document := filepath.Join(w.stateDir, config.ConfigFileName)
	w.document(map[string]any{"repoDir": w.checkout, "port": w.port})
	before, err := os.ReadFile(document)
	if err != nil {
		t.Fatalf("read the settings document: %v", err)
	}

	for _, args := range [][]string{
		{"status"}, {"url"}, {"logs"}, {"doctor"}, {"version"}, {"--help"},
	} {
		w.run(args...)
		after, err := os.ReadFile(document)
		if err != nil {
			t.Fatalf("read the settings document after %v: %v", args, err)
		}
		if string(after) != string(before) {
			t.Fatalf("%v rewrote the settings document:\nbefore: %s\nafter:  %s", args, before, after)
		}
	}
	if _, err := os.Lstat(filepath.Join(w.stateDir, config.LockFileName)); !os.IsNotExist(err) {
		t.Fatalf("a reporting command created the operation lock: %v", err)
	}
}

// TestARefusedStartRecordsNothing pins that the write-back cannot outrun the
// work: a start that never left the preflight must leave the document deciding
// nothing, or the next command would follow a checkout that demonstrably does not
// work.
func TestARefusedStartRecordsNothing(t *testing.T) {
	w := newCLIWorld(t)
	gone := filepath.Join(w.root, "gone-checkout")

	start := w.run("--repo", gone, "start")
	if start.code != 4 {
		t.Fatalf("start exit = %d, want 4\nstdout=%s\nstderr=%s", start.code, start.stdout, start.stderr)
	}
	for _, want := range []string{gone, "DSH_REPO_DIR", "--repo"} {
		if !strings.Contains(start.stderr, want) {
			t.Fatalf("the failure does not name %q:\n%s", want, start.stderr)
		}
	}
	if _, present := w.settingsDocument()["repoDir"]; present {
		t.Fatalf("settings document = %v, want no checkout: nothing ran", w.settingsDocument())
	}
}

// TestAPortIsNotFrozenIntoTheDocumentByAFlag pins the other half of the
// provisioning promise: an override applies to one run and is never written down,
// so a machine is not left talking to the port of a one-off experiment.
func TestAPortIsNotFrozenIntoTheDocumentByAFlag(t *testing.T) {
	w := newCLIWorld(t)
	other := freePort(t)

	// A refused start still provisions the document; the port it was given must
	// not be in it.
	w.variables["DSH_PORT"] = strconv.Itoa(other)
	w.run("--port", strconv.Itoa(other), "--repo", filepath.Join(w.root, "gone"), "start")
	if got := w.settingsDocument()["port"]; got != float64(config.DefaultPort) {
		t.Fatalf("settings document port = %v, want the default %d", got, config.DefaultPort)
	}
}

// initCheckoutGit turns a directory into a git worktree, which is what the
// checkout checks and `doctor` require. The environment is built from an
// allow-list so that nothing here can reach a repository on the machine running
// the test.
func initCheckoutGit(t *testing.T, dir string) {
	t.Helper()
	commands := [][]string{
		{"init", "-q", "."},
		{"add", "-A"},
		{"-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-qm", "init"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = []string{
			"HOME=" + dir,
			"PATH=" + os.Getenv("PATH"),
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable for the checkout fixture (%v): %s", err, out)
		}
	}
}

// writeFile creates a file together with its parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
