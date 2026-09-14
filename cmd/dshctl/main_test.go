package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRunReturnsTheCommandStatus pins the process boundary: the status the
// command line produced is the status the process exits with.
func TestRunReturnsTheCommandStatus(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"version succeeds", []string{"version"}, 0},
		{"unknown command is a usage error", []string{"totally-unknown"}, 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			env := map[string]string{
				"HOME":             filepath.Join(root, "home"),
				"DSHCTL_STATE_DIR": filepath.Join(root, "state"),
				"DSH_HOME":         filepath.Join(root, "harness"),
			}
			var stdout, stderr bytes.Buffer
			got := run(context.Background(), testCase.args, &stdout, &stderr, func(key string) string {
				return env[key]
			})
			if got != testCase.want {
				t.Fatalf("run(%v) = %d, want %d (stderr = %s)", testCase.args, got, testCase.want, stderr.String())
			}
		})
	}
}

// TestRunReportsCancellation pins that a cancelled context becomes the
// conventional interrupt status rather than a generic failure.
func TestRunReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "dsh-web.log"), []byte("line\n"), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	env := map[string]string{
		"HOME":             filepath.Join(root, "home"),
		"DSHCTL_STATE_DIR": stateDir,
		"DSH_HOME":         filepath.Join(root, "harness"),
	}
	var stdout, stderr bytes.Buffer
	got := run(ctx, []string{"logs", "--follow"}, &stdout, &stderr, func(key string) string { return env[key] })
	if got != 130 {
		t.Fatalf("run with a cancelled context = %d, want 130 (stderr = %s)", got, stderr.String())
	}
}

// TestRunIsUsableWithoutAnyConfiguration pins that the process boundary adds no
// requirement of its own: with no config file, no repository and only a home
// directory, the command still answers.
func TestRunIsUsableWithoutAnyConfiguration(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{
		"HOME":             filepath.Join(root, "home"),
		"DSHCTL_STATE_DIR": filepath.Join(root, "state"),
		"DSH_HOME":         filepath.Join(root, "harness"),
	}
	var stdout, stderr bytes.Buffer
	got := run(context.Background(), []string{"doctor", "--json"}, &stdout, &stderr, func(key string) string {
		return env[key]
	})
	if got != 1 {
		t.Fatalf("doctor exit = %d, want 1 for an unusable environment (stderr = %s)", got, stderr.String())
	}
	var checks []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &checks); err != nil {
		t.Fatalf("doctor --json is not JSON: %v\n%s", err, stdout.String())
	}
	if len(checks) == 0 {
		t.Fatal("doctor produced no checks")
	}
	if _, err := os.Lstat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("a reporting command created state")
	}
}

// signalHelperEnv marks the child process of the signal test.
//
// The value is not a constant: it names the parent that set it, and the child
// only takes the helper branch when the marker names its own parent. A bare
// "1" in the ambient environment would make the parent test process itself run
// the helper — and the helper calls os.Exit, so an exported variable would end
// the whole test run with a mysterious status instead of running the test.
const signalHelperEnv = "DSHCTL_SIGNAL_HELPER"

// helperMarker renders the value the parent writes and the child verifies.
func helperMarker(parentPID int) string { return "child-of-" + strconv.Itoa(parentPID) }

// signalHelperChildEnv returns the prepared environment with any ambient
// helper-marker variable removed before the real one is appended. Unix resolves
// a duplicated environment key to its first occurrence, so an exported
// DSHCTL_SIGNAL_HELPER would otherwise shadow the appended value and the child
// would re-run the test instead of taking the helper branch — a fork chain.
func signalHelperChildEnv(environment []string, extra ...string) []string {
	filtered := make([]string, 0, len(environment)+len(extra))
	for _, entry := range environment {
		if strings.HasPrefix(entry, signalHelperEnv+"=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, extra...)
}

// TestSignalHandlingExitsCleanly re-executes this test binary as a helper so the
// real signal handler runs, because a signal handler cannot be installed and
// delivered faithfully in-process.
func TestSignalHandlingExitsCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fixture needs POSIX signals")
	}
	if os.Getenv(signalHelperEnv) == helperMarker(os.Getppid()) {
		runSignalHelper()
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	root := t.TempDir()
	cmd := exec.Command(executable, "-test.run=TestSignalHandlingExitsCleanly")
	stateDir := filepath.Join(root, "state")
	cmd.Env = signalHelperChildEnv(helperEnvironment(root, stateDir), signalHelperEnv+"="+helperMarker(os.Getpid()))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// The helper's environment is spelled out rather than inherited: an exported
	// DSH_LOG_FILE from the surrounding shell would send it looking somewhere
	// else, and the test would then be measuring the environment, not the code.
	// The helper prints a line once its handler is installed and it is blocked.
	ready := make(chan string, 1)
	go func() {
		buffer := make([]byte, 32)
		read, _ := stdout.Read(buffer)
		ready <- string(buffer[:read])
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, "ready") {
			_ = cmd.Process.Kill()
			t.Fatalf("helper output = %q, want it to report readiness", line)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the helper never became ready")
	}
	signals := terminationSignals()
	if len(signals) == 0 {
		t.Fatal("no termination signal is configured for this platform")
	}
	if err := cmd.Process.Signal(signals[0]); err != nil {
		t.Fatalf("signal: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errorsAs(err, &exit) {
			t.Fatalf("expected an exit status, got %v", err)
		}
		// os.Exit(130) is observable as 130; a process killed by the signal
		// itself would report -1, which is what this distinguishes.
		if exit.ExitCode() != 130 {
			t.Fatalf("exit = %d, want 130", exit.ExitCode())
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the helper did not exit after the signal")
	}
}

// runSignalHelper is the child process in TestSignalHandlingExitsCleanly.
//
// It mirrors main: install the handler, then run a command that blocks until the
// context is cancelled. The command must be one that actually waits, otherwise
// the helper would finish before the signal arrived and the test would prove
// nothing. `logs --follow` blocks on its follow loop, and its cancellation
// status is 130.
func runSignalHelper() {
	ctx, stop := signalContext()
	defer stop()

	if err := seedHelperState(); err != nil {
		os.Stdout.WriteString("setup failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("ready\n")
	os.Stdout.Sync()
	os.Exit(run(ctx, []string{"logs", "--follow"}, os.Stdout, os.Stderr, os.Getenv))
}

// helperEnvironment builds the environment the signal helper runs with.
//
// Every dshctl variable is set explicitly, and only the operating system's own
// variables are inherited, because the binary needs PATH and a temporary
// directory but nothing else about the surrounding shell is its business.
func helperEnvironment(root, stateDir string) []string {
	environment := []string{
		"HOME=" + filepath.Join(root, "home"),
		"DSH_HOME=" + filepath.Join(root, "harness"),
		"DSHCTL_STATE_DIR=" + stateDir,
		"DSHCTL_CONFIG=" + filepath.Join(stateDir, "config.json"),
		"DSH_LOG_FILE=" + filepath.Join(stateDir, "dsh-web.log"),
		"DSH_REPO_DIR=" + filepath.Join(root, "repo"),
		"DSH_NODE_VERSION=latest",
	}
	for _, name := range []string{"PATH", "TMPDIR", "TEMP", "TMP", "SystemRoot", "USERPROFILE", "COMSPEC"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

// seedHelperState gives the helper a log to follow.
func seedHelperState() error {
	logPath := os.Getenv("DSH_LOG_FILE")
	if logPath == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(logPath, []byte("first line\n"), 0o600)
}

// errorsAs reports whether err is an *exec.ExitError.
func errorsAs(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}
