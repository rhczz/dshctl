package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
)

// execute runs the command line against a temporary state directory that must
// not exist unless the command creates it.
//
// The port is one the operating system just handed back, so a test never depends
// on what happens to be listening on the developer's machine.
func execute(t *testing.T, args ...string) (code int, stdout, stderr, stateDir string) {
	t.Helper()
	return executeWith(t, nil, args...)
}

// executeWith runs the command line with the hermetic environment plus per-test
// overrides.
func executeWith(t *testing.T, overrides map[string]string, args ...string) (code int, stdout, stderr, stateDir string) {
	t.Helper()
	_, stateDir = freshEnvironment(t, overrides)
	var out, errOut bytes.Buffer
	code = Main(context.Background(), args, &out, &errOut, os.Getenv)
	return code, out.String(), errOut.String(), stateDir
}

// freshEnvironment installs the environment the command line under test runs
// with, and returns it together with the state directory it names.
//
// Every dshctl variable is set explicitly rather than inherited. An inherited
// one — DSHCTL_CONFIG from a test harness, DSH_LOG_FILE from the operator's
// shell — silently redirects the command under test, which is how a test ends up
// measuring the environment instead of the code. The ports, the state directory
// and the home directory all live under a temporary directory the test owns.
//
// The returned map is the source of truth for the paths involved: a test that
// needs to place a file where the command will look reads it from here instead
// of guessing the layout.
func freshEnvironment(t *testing.T, overrides map[string]string) (map[string]string, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	environment := map[string]string{
		"DSHCTL_STATE_DIR": stateDir,
		"DSH_HOME":         filepath.Join(root, "harness"),
		"DSH_REPO_DIR":     filepath.Join(root, "repo"),
		"DSHCTL_CONFIG":    filepath.Join(stateDir, "config.json"),
		"DSH_LOG_FILE":     filepath.Join(stateDir, "dsh-web.log"),
		"DSH_NODE_VERSION": config.TestedNodeVersion,
		"DSH_PORT":         strconv.Itoa(freePort(t)),
		// The home directory is redirected too: `doctor` resolves Node against
		// paths.Home(), and reading the operator's ~/.nvm or ~/.local/share/fnm
		// would make the answer a function of the machine the test runs on.
		// Both names are set because os.UserHomeDir reads $HOME on Unix and
		// %USERPROFILE% on Windows.
		"HOME":        filepath.Join(root, "home"),
		"USERPROFILE": filepath.Join(root, "home"),
		// The PATH is narrowed to the operating system's own directories: the
		// port probes need lsof/ss/netstat, and nothing else may be reachable
		// from the tests — in particular not the operator's pnpm. A pnpm
		// installed through corepack would otherwise be executed here, and a
		// corepack shim fetches the real package on first use. The shim is also
		// told to stay local, for the runners that ship one despite the narrow
		// PATH.
		"PATH":                            minimalToolPath(),
		"COREPACK_ENABLE_NETWORK":         "0",
		"COREPACK_ENABLE_DOWNLOAD_PROMPT": "0",
		"COREPACK_ENABLE_AUTO_PIN":        "0",
	}
	for key, value := range overrides {
		environment[key] = value
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Setenv(key, environment[key])
	}
	return environment, stateDir
}

// minimalToolPath is a PATH containing only the operating system's own tool
// directories. On Windows none of them exist, which is fine: the Windows probe
// reads the TCP table through the API and needs no external tool.
func minimalToolPath() string {
	var dirs []string
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// freePort returns a TCP port that was free a moment ago and that no earlier
// call in this process has handed out.
//
// The tests that start several servers ask for several ports and give them
// roles that must not collide. A fixture whose "configured" port is also a port
// the test starts a server on makes every assertion about that port vacuous, and
// the failure it produces on a machine where the kernel reuses the port reads
// like a dshctl defect. Remembering the answers costs nothing and removes the
// possibility.
func freePort(t *testing.T) int {
	t.Helper()
	freePorts.Lock()
	defer freePorts.Unlock()
	for attempt := 0; attempt < 64; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve a port: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatalf("release the port: %v", err)
		}
		if freePorts.handed[port] {
			continue
		}
		if freePorts.handed == nil {
			freePorts.handed = map[int]bool{}
		}
		freePorts.handed[port] = true
		return port
	}
	t.Fatal("no unused port could be reserved")
	return 0
}

// freePorts holds every port freePort has already returned. See freePort.
var freePorts = struct {
	sync.Mutex
	handed map[int]bool
}{}

// TestVersionCommand pins the plain version output and that it touches nothing.
func TestVersionCommand(t *testing.T) {
	code, stdout, stderr, stateDir := execute(t, "version")
	if code != exitcode.OK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "dshctl") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("version must not create the state directory")
	}
}

// TestVersionJSON pins the machine-readable form.
func TestVersionJSON(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "version", "--json")
	if code != exitcode.OK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	for _, field := range []string{"version", "commit", "buildDate", "platform"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("version JSON is missing %q: %s", field, stdout)
		}
	}
}

// TestVersionFlag pins the short-circuit form.
func TestVersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		code, stdout, _, _ := execute(t, flag)
		if code != exitcode.OK || !strings.Contains(stdout, "dshctl") {
			t.Fatalf("%s: exit = %d, stdout = %q", flag, code, stdout)
		}
	}
}

// TestHelpSelection pins the help routing and that help never touches the
// filesystem.
func TestHelpSelection(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "命令:"},
		{[]string{"-h"}, "命令:"},
		{[]string{"help"}, "命令:"},
		{[]string{"help", "build"}, "dshctl build"},
		{[]string{"build", "-h"}, "dshctl build"},
		{[]string{"--help", "status"}, "dshctl status"},
		{[]string{"logs", "--help"}, "dshctl logs"},
	}
	for _, testCase := range cases {
		code, stdout, stderr, stateDir := execute(t, testCase.args...)
		if code != exitcode.OK {
			t.Fatalf("%v: exit = %d, stderr = %s", testCase.args, code, stderr)
		}
		if !strings.Contains(stdout, testCase.want) {
			t.Fatalf("%v: output does not contain %q:\n%s", testCase.args, testCase.want, stdout)
		}
		if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
			t.Fatalf("%v: help must not create the state directory", testCase.args)
		}
	}
}

// TestHelpForAnUnknownCommand pins the usage error.
func TestHelpForAnUnknownCommand(t *testing.T) {
	code, _, stderr, _ := execute(t, "help", "totally-unknown")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(stderr, "未知命令") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// TestUnknownCommand pins the usage error for a stray command.
func TestUnknownCommand(t *testing.T) {
	code, _, stderr, stateDir := execute(t, "totally-unknown")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(stderr, "未知命令") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("a usage error must not touch the state directory")
	}
}

// TestUnexpectedArgumentIsAUsageError pins that a stray word is reported.
func TestUnexpectedArgumentIsAUsageError(t *testing.T) {
	code, _, stderr, stateDir := execute(t, "logs", "extra")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(stderr, "不接受位置参数") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("a usage error must not touch the state directory")
	}
}

// TestGlobalFlagErrors pins the malformed global flags.
func TestGlobalFlagErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--bogus"}, "未知的全局参数"},
		{"missing value", []string{"--port"}, "需要一个值"},
		{"empty value", []string{"--repo="}, "不能为空"},
		{"empty config", []string{"--config="}, "不能为空"},
		{"non-numeric port", []string{"--port", "abc"}, "不是数字"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, _, stderr, _ := execute(t, testCase.args...)
			if code != exitcode.Usage {
				t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
			}
			if !strings.Contains(stderr, testCase.want) {
				t.Fatalf("stderr = %q, want it to mention %q", stderr, testCase.want)
			}
		})
	}
}

// TestOutOfRangePortIsAUsageError pins that a syntactically valid but impossible
// port is rejected with the same code as a malformed one.
func TestOutOfRangePortIsAUsageError(t *testing.T) {
	for _, port := range []string{"0", "65536", "70000", "-1"} {
		code, _, stderr, _ := execute(t, "--port", port, "status")
		if code != exitcode.Usage {
			t.Fatalf("--port %s: exit = %d, want %d (stderr = %s)", port, code, exitcode.Usage, stderr)
		}
	}
}

// TestMisplacedGlobalFlagPrintsTheHint pins the guidance that turns the bare
// "flag provided but not defined" into an actionable message: global flags must
// be written before the command name.
func TestMisplacedGlobalFlagPrintsTheHint(t *testing.T) {
	code, _, stderr, stateDir := execute(t, "status", "--port", "4000")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
	}
	if !strings.Contains(stderr, "须写在命令名之前") {
		t.Fatalf("stderr = %q, want the placement hint", stderr)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("a usage error must not touch the state directory")
	}
}

// TestRelativeRepoIsAUsageError pins that a relative repository path is refused
// rather than resolved against the current directory.
func TestRelativeRepoIsAUsageError(t *testing.T) {
	code, _, stderr, _ := execute(t, "--repo", "rel/path", "status")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
	}
	if !strings.Contains(stderr, "绝对路径") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// TestReportingCommandsDoNotProvision pins the side-effect boundary.
func TestReportingCommandsDoNotProvision(t *testing.T) {
	code, _, stderr, stateDir := execute(t, "logs")
	if code != exitcode.Failure {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Failure, stderr)
	}
	if !strings.Contains(stderr, "日志文件不存在") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("logs must not create the state directory")
	}
}

// TestStatusReportsAFreePortAsNotRunning pins the exit code scripts branch on.
func TestStatusReportsAFreePortAsNotRunning(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stdout, "未运行") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stderr, "错误") {
		t.Fatalf("a not-running status must not print an error: %q", stderr)
	}
}

// TestStatusJSONIsParseable pins that scripts get clean JSON.
func TestStatusJSONIsParseable(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "status", "--json")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	// The document is the report of the instance the command was about; `ports`
	// carries every instance beside it.
	status, ok := decoded["status"].(map[string]any)
	if !ok {
		t.Fatalf("status JSON has no status object: %s", stdout)
	}
	if status["state"] == nil {
		t.Fatalf("status JSON has no state: %s", stdout)
	}
	if _, ok := decoded["ports"].([]any); !ok {
		t.Fatalf("status JSON has no ports list: %s", stdout)
	}
}

// TestDoctorJSONIsParseable pins that warnings go to the checks, not to stderr,
// so a script can parse the document.
func TestDoctorJSONIsParseable(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "doctor", "--json")
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s\nstderr=%s", err, stdout, stderr)
	}
	if len(decoded) == 0 {
		t.Fatal("doctor produced no checks")
	}
	// The repository does not exist in this fixture, so the diagnosis must fail
	// and report it as a check rather than as a stream of prose on stderr.
	if code != exitcode.Failure {
		t.Fatalf("exit = %d, want %d", code, exitcode.Failure)
	}
	failed := false
	for _, check := range decoded {
		if check["status"] == "fail" {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("no failing check reported: %s", stdout)
	}
}

// TestLogsRejectsBuildWithFollow pins that a contradictory flag combination is a
// usage error instead of a silently ignored flag.
func TestLogsRejectsBuildWithFollow(t *testing.T) {
	code, _, stderr, _ := execute(t, "logs", "--build", "--follow")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
	}
}

// TestAllCommandsAreComplete pins the registry's integrity.
func TestAllCommandsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, command := range Commands() {
		if command.Summary == "" || command.Run == nil || command.Name == "" {
			t.Fatalf("incomplete command registration: %+v", command)
		}
		if seen[command.Name] {
			t.Fatalf("duplicate command %q", command.Name)
		}
		seen[command.Name] = true
	}
	for _, expected := range []string{"start", "stop", "restart", "status", "url", "logs", "build", "update", "doctor", "version"} {
		if !seen[expected] {
			t.Fatalf("command %q is missing from the registry", expected)
		}
	}
}

// TestParseGlobals pins the global flag parser directly.
func TestParseGlobals(t *testing.T) {
	parsed, rest, err := parseGlobals([]string{"--port", "4000", "--repo=/tmp/repo", "--node", "24.20.0", "-v", "status", "--json"})
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if parsed.port == nil || *parsed.port != 4000 {
		t.Fatalf("port = %v", parsed.port)
	}
	if parsed.repoDir != "/tmp/repo" || !parsed.repoSet {
		t.Fatalf("repo = %q set=%v", parsed.repoDir, parsed.repoSet)
	}
	if parsed.nodeVersion != "24.20.0" || !parsed.nodeSet {
		t.Fatalf("node = %q set=%v", parsed.nodeVersion, parsed.nodeSet)
	}
	if !parsed.verbose {
		t.Fatal("verbose was not parsed")
	}
	if strings.Join(rest, " ") != "status --json" {
		t.Fatalf("rest = %v", rest)
	}
}

// TestParseGlobalsStopsAtTheCommand pins that command arguments are not consumed
// by the global parser.
func TestParseGlobalsStopsAtTheCommand(t *testing.T) {
	_, rest, err := parseGlobals([]string{"status", "--port", "4000"})
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if strings.Join(rest, " ") != "status --port 4000" {
		t.Fatalf("rest = %v, want the command and its own arguments", rest)
	}
}

// TestParseGlobalsHonoursTheTerminator pins "--".
func TestParseGlobalsHonoursTheTerminator(t *testing.T) {
	_, rest, err := parseGlobals([]string{"--", "status"})
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	if strings.Join(rest, " ") != "status" {
		t.Fatalf("rest = %v", rest)
	}
}

// TestConfigFlagIsLayeredOverTheEnvironment pins that --config only replaces the
// config file path and leaves the other environment variables in effect.
func TestConfigFlagIsLayeredOverTheEnvironment(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "custom.json")
	if err := os.WriteFile(configPath, []byte(`{"port": 4321}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DSHCTL_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("DSH_HOME", filepath.Join(root, "harness"))
	t.Setenv("DSH_REPO_DIR", filepath.Join(root, "repo"))
	t.Setenv("DSH_NODE_VERSION", "24.20.0")

	parsed, _, err := parseGlobals([]string{"--config", configPath, "status"})
	if err != nil {
		t.Fatalf("parseGlobals: %v", err)
	}
	settings, err := loadSettings(parsed, os.Getenv)
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if settings.Port != 4321 {
		t.Fatalf("port = %d, want the value from the custom config", settings.Port)
	}
	if settings.RepoDir != filepath.Join(root, "repo") {
		t.Fatalf("repoDir = %q, want the environment value", settings.RepoDir)
	}
	if settings.NodeVersion != "24.20.0" {
		t.Fatalf("nodeVersion = %q, want the environment value", settings.NodeVersion)
	}
}

// TestVerboseNamesTheSources pins the -v output.
func TestVerboseNamesTheSources(t *testing.T) {
	code, _, stderr, _ := execute(t, "-v", "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d", code, exitcode.NotRunning)
	}
	for _, want := range []string{"配置文件", "状态目录", "仓库目录", "监听端口"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("verbose output is missing %q:\n%s", want, stderr)
		}
	}
}
