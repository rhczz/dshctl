package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/config"
)

// buildOnce compiles the real command once per test run.
//
// The wiring from a command name to its implementation has no branches worth
// unit-testing — what can go wrong there is a command that is registered but
// unusable. Only running the actual binary answers that.
var (
	buildOnce   sync.Once
	builtBinary string
	builtDir    string
	buildErr    error
)

// binary builds cmd/dshctl and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "dshctl-binary-")
		if err != nil {
			buildErr = err
			return
		}
		builtDir = dir
		name := "dshctl"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", path, "github.com/rhczz/dshctl/cmd/dshctl")
		// The build runs with the module proxy switched off and the local
		// toolchain pinned: the module has no dependencies, so a build that
		// needs the network is a build that is doing something the test did not
		// ask for, and a test must not reach outside the machine it runs on.
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-trimpath", "GOTOOLCHAIN=local")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildFailure{output: string(out), err: err}
			return
		}
		builtBinary = path
	})
	if buildErr != nil {
		// A broken binary is a broken tool, not an absent nicety: every test
		// above this one exists to run the real binary, and a skip here would
		// turn a compile failure into a green suite.
		t.Fatalf("cannot build the binary here: %v", buildErr)
	}
	return builtBinary
}

// cleanupBuiltBinary removes the directory the built binary lives in.
//
// It is called from TestMain after the run: without it every `go test` of this
// package would leave a full binary behind in the system temporary directory,
// which is exactly the kind of residue this suite refuses to produce.
func cleanupBuiltBinary() {
	if builtDir != "" {
		_ = os.RemoveAll(builtDir)
	}
}

// buildFailure carries the compiler output for the skip message.
type buildFailure struct {
	output string
	err    error
}

// Error implements error.
func (b *buildFailure) Error() string { return b.err.Error() + ": " + b.output }

// invocation is one run of the real binary.
type invocation struct {
	code   int
	stdout string
	stderr string
}

// runBinary executes the binary with a throwaway home and state directory, and
// with every stream redirected so a detached child cannot hold a pipe open.
func runBinary(t *testing.T, args ...string) (invocation, string) {
	t.Helper()
	return runBinaryWith(t, nil, args...)
}

// runBinaryWith runs the built binary with the hermetic environment plus
// per-test overrides, replacing a variable rather than adding a second copy of
// it (the comparison is case-insensitive because Windows spells PATH as "Path").
func runBinaryWith(t *testing.T, overrides map[string]string, args ...string) (invocation, string) {
	t.Helper()
	path := binary(t)
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	environment := binaryEnvironment(t, root, home, stateDir)
	for key, value := range overrides {
		environment = setEnvironment(environment, key, value)
	}

	cmd := exec.Command(path, args...)
	cmd.Dir = root
	cmd.Env = environment
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if ok := asExitError(err, &exit); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("running %v: %v", args, err)
		}
	}
	return invocation{code: code, stdout: stdout.String(), stderr: stderr.String()}, stateDir
}

// setEnvironment replaces one variable in a KEY=VALUE list, or appends it when
// it is not there yet.
func setEnvironment(environment []string, key, value string) []string {
	kept := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, key) {
			continue
		}
		kept = append(kept, entry)
	}
	return append(kept, key+"="+value)
}

// stubToolPath writes do-nothing executables for the programs a command's
// preflight resolves, and returns a PATH that finds them ahead of the operating
// system's own directories.
//
// A test that asserts what happens *after* a preflight has to make the
// preflight pass on every machine, or it measures the runner instead of the
// code: with pnpm installed the run reached the step under test, while on a
// clean CI runner the very same run stopped earlier with "pnpm not found" — a
// green test on one machine and a red one on another, for reasons that have
// nothing to do with the assertion. Nothing here executes the stubs (the run
// stops at the step being pinned); they only have to be resolvable.
func stubToolPath(t *testing.T, programs ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, program := range programs {
		name := program
		body := "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" {
			// Windows resolves through PATHEXT, so a bare name is not a program.
			name += ".cmd"
			body = "@exit /b 0\r\n"
		}
		if program == "node" {
			// node is the one stub a command may actually run: resolving a
			// runtime on PATH is followed by `node -v` to learn its version, and
			// a stub that answers with noise would make the command under test
			// decide it is running an ancient Node.
			name, body = nodeStubBody(config.TestedNodeVersion)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write the %s stub: %v", program, err)
		}
	}
	return dir + string(os.PathListSeparator) + minimalToolPath()
}

// nodeStubBody renders a fake node that reports version, in the shape the
// platform resolves and executes.
func nodeStubBody(version string) (string, string) {
	if runtime.GOOS == "windows" {
		return "node.cmd", "@echo v" + version + "\r\n"
	}
	return "node", "#!/bin/sh\necho v" + version + "\n"
}

// stubNodePath writes a node stub that reports version, together with a pnpm
// stub, and returns a PATH that finds them ahead of the operating system's own
// directories.
//
// It is how a test says which release the machine serves, which is the input the
// whole resolution is about. pnpm comes along because every mutating command
// resolves it before the runtime, so a machine without it never reaches the
// decision under test.
func stubNodePath(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	name, body := nodeStubBody(version)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write the node stub: %v", err)
	}
	pnpmName, pnpmBody := "pnpm", "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		pnpmName, pnpmBody = "pnpm.cmd", "@exit /b 0\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, pnpmName), []byte(pnpmBody), 0o755); err != nil {
		t.Fatalf("write the pnpm stub: %v", err)
	}
	return dir + string(os.PathListSeparator) + minimalToolPath()
}

// readSettingsDocument decodes the settings document of a run.
func readSettingsDocument(t *testing.T, stateDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "config.json"))
	if err != nil {
		t.Fatalf("read the settings document: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode the settings document: %v\n%s", err, data)
	}
	return document
}

// binaryEnvironment builds the environment the binary runs with.
//
// Every dshctl variable is set explicitly rather than inherited. An inherited
// one — DSHCTL_CONFIG from a test harness, DSH_REPO_DIR from the operator's
// shell — silently redirects the command under test, which is how a test ends up
// passing or failing for reasons unrelated to the code. Only the operating
// system's own variables are kept, because the binary legitimately needs PATH.
func binaryEnvironment(t *testing.T, root, home, stateDir string) []string {
	t.Helper()
	environment := []string{
		"HOME=" + home,
		"DSH_HOME=" + filepath.Join(home, ".dsh"),
		"DSHCTL_STATE_DIR=" + stateDir,
		"DSHCTL_CONFIG=" + filepath.Join(stateDir, "config.json"),
		"DSH_LOG_FILE=" + filepath.Join(stateDir, "dsh-web.log"),
		"DSH_REPO_DIR=" + filepath.Join(root, "repo"),
		"DSH_PORT=" + freePortString(t),
		// A pnpm installed through corepack is a shim that downloads the real
		// package on first use, which would make `doctor` depend on the network
		// and could even block on an interactive prompt. The test binary is
		// never allowed to do either: whatever the shim can answer from its
		// local cache is what the command under test sees.
		"COREPACK_ENABLE_NETWORK=0",
		"COREPACK_ENABLE_DOWNLOAD_PROMPT=0",
		"COREPACK_ENABLE_AUTO_PIN=0",
	}
	for _, name := range []string{"PATH", "TMPDIR", "TEMP", "TMP", "SystemRoot", "USERPROFILE", "COMSPEC"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

// asExitError reports whether err is a non-zero exit status.
func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// freePortString reserves a port and returns it as the environment value.
func freePortString(t *testing.T) string {
	t.Helper()
	port := freePort(t)
	return strconv.Itoa(port)
}

// TestBinaryVersionAndHelp pins that the real binary answers the two commands
// that must never touch the filesystem.
func TestBinaryVersionAndHelp(t *testing.T) {
	version, stateDir := runBinary(t, "version")
	if version.code != 0 {
		t.Fatalf("version exit = %d, stderr = %s", version.code, version.stderr)
	}
	if !strings.Contains(version.stdout, "dshctl") {
		t.Fatalf("version stdout = %q", version.stdout)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("version created the state directory")
	}

	help, _ := runBinary(t, "--help")
	if help.code != 0 || !strings.Contains(help.stdout, "命令:") {
		t.Fatalf("help exit = %d, stdout = %q", help.code, help.stdout)
	}
}

// TestBinaryVersionJSON pins the script-facing output of the real binary.
func TestBinaryVersionJSON(t *testing.T) {
	result, _ := runBinary(t, "version", "--json")
	if result.code != 0 {
		t.Fatalf("exit = %d, stderr = %s", result.code, result.stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &decoded); err != nil {
		t.Fatalf("version --json is not JSON: %v\n%s", err, result.stdout)
	}
	if decoded["platform"] == "" {
		t.Fatalf("platform is missing: %s", result.stdout)
	}
}

// TestBinaryStatusDoctorAndLogsAreUsableWithoutConfiguration pins requirement 5
// at the level that matters: a freshly installed binary, with no config file and
// no repository, still produces an answer for every reporting command instead of
// crashing or hanging.
func TestBinaryStatusDoctorAndLogsAreUsableWithoutConfiguration(t *testing.T) {
	status, stateDir := runBinary(t, "status")
	if status.code != 3 {
		t.Fatalf("status exit = %d, want 3 (stderr = %s)", status.code, status.stderr)
	}
	if !strings.Contains(status.stdout, "未运行") && !strings.Contains(status.stdout, "端口") {
		t.Fatalf("status stdout = %q", status.stdout)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("status created the state directory")
	}

	doctor, _ := runBinary(t, "doctor", "--json")
	var checks []map[string]any
	if err := json.Unmarshal([]byte(doctor.stdout), &checks); err != nil {
		t.Fatalf("doctor --json is not JSON: %v\n%s", err, doctor.stdout)
	}
	if len(checks) == 0 {
		t.Fatal("doctor reported no checks")
	}
	if doctor.code != 1 {
		t.Fatalf("doctor exit = %d, want 1 for an unusable environment", doctor.code)
	}

	logs, _ := runBinary(t, "logs")
	if logs.code != 1 {
		t.Fatalf("logs exit = %d, want 1 for a missing log", logs.code)
	}
	if !strings.Contains(logs.stderr, "日志文件不存在") {
		t.Fatalf("logs stderr = %q", logs.stderr)
	}

	url, _ := runBinary(t, "url")
	if url.code != 3 {
		// The same "not running" code status uses: one vocabulary for one fact.
		t.Fatalf("url exit = %d, want 3 without a running server", url.code)
	}
}

// TestBinaryRefusesDestructiveCommandsWithoutARealCheckout pins that start,
// build and update all fail before touching anything when the directory is not
// the managed checkout. This is the real binary, so it also proves the wiring
// for those three commands.
func TestBinaryRefusesDestructiveCommandsWithoutARealCheckout(t *testing.T) {
	root := t.TempDir()
	notACheckout := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(notACheckout, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, command := range []string{"start", "build", "update"} {
		result, _ := runBinary(t, "--repo", notACheckout, command)
		if result.code != 4 {
			t.Fatalf("%s exit = %d, want 4 (stderr = %s)", command, result.code, result.stderr)
		}
		if !strings.Contains(result.stderr, "DeepSeek Harness") && !strings.Contains(result.stderr, "git") {
			t.Fatalf("%s stderr = %q, want it to name the problem", command, result.stderr)
		}
	}
}

// TestBinaryStopIsSafeWithoutAServer pins that stop is a no-op rather than an
// error when nothing runs, and that restart surfaces the missing checkout.
func TestBinaryStopIsSafeWithoutAServer(t *testing.T) {
	stop, _ := runBinary(t, "stop")
	if stop.code != 0 {
		t.Fatalf("stop exit = %d, want 0 (stderr = %s)", stop.code, stop.stderr)
	}
	if !strings.Contains(stop.stdout, "未在运行") {
		t.Fatalf("stop stdout = %q", stop.stdout)
	}

	root := t.TempDir()
	restart, _ := runBinary(t, "--repo", root, "restart")
	if restart.code != 4 {
		t.Fatalf("restart exit = %d, want 4 (stderr = %s)", restart.code, restart.stderr)
	}
}

// TestBinaryInterruptExitsOneThirty pins that Ctrl-C on a real process produces
// the conventional code, which scripts rely on.
func TestBinaryInterruptExitsOneThirty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to a console process is not portable")
	}
	path := binary(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// logs --follow blocks until it is interrupted; there must be a log to read.
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "dsh-web.log"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	cmd := exec.Command(path, "logs", "--follow")
	cmd.Env = binaryEnvironment(t, root, home, stateDir)
	cmd.Stdin = nil
	// Read the output rather than discarding it: the first line proves the
	// process is running its follow loop, which means its signal handler is
	// installed. Interrupting before that kills it by default disposition, and
	// the test would be measuring the wrong thing.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	first := make(chan string, 1)
	go func() {
		buffer := make([]byte, 64)
		read, _ := stdout.Read(buffer)
		first <- string(buffer[:read])
	}()
	select {
	case line := <-first:
		if !strings.Contains(line, "hello") {
			t.Fatalf("first output = %q, want the log's first line", line)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the follow produced no output")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !asExitError(err, &exit) {
			t.Fatalf("expected an exit status, got %v", err)
		}
		if exit.ExitCode() != 130 {
			t.Fatalf("exit = %d, want 130", exit.ExitCode())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the binary did not exit after an interrupt")
	}
}
