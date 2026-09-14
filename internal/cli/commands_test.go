package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/logfile"
)

// These tests drive every subcommand through the real command line, in process,
// against a throwaway environment. The binary-level tests in binary_test.go
// prove the wiring end to end but cannot say much about a command's own flags,
// output or exit code, because only a few commands are ever run there.
//
// Nothing here starts a server: every fixture either answers a read-only
// question or refuses at a precondition. The one process this file owns is a
// listening socket it opened itself, which is what makes "the port was left
// alone" an assertion rather than a hope.

// holdLoopbackPort opens a listening socket and returns its port.
//
// The socket is held for the whole test, so a command that sees an occupied port
// sees a process the test controls and can verify afterwards.
func holdLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

// portAcceptsConnections reports whether something still answers on the port.
func portAcceptsConnections(t *testing.T, port int) bool {
	t.Helper()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// runtimeRecords lists the runtime records a state directory holds.
func runtimeRecords(t *testing.T, stateDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(stateDir, "dsh-web-*.state.json"))
	if err != nil {
		t.Fatalf("glob records: %v", err)
	}
	return matches
}

// seedLogAt writes a log file where the prepared environment says the log lives,
// and returns its path.
func seedLogAt(t *testing.T, environment map[string]string, lines ...string) string {
	t.Helper()
	path := environment["DSH_LOG_FILE"]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir for the log: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	return path
}

// TestEveryCommandAnswersItsOwnHelp pins that help is available for every
// registered command, prints the command's own name and touches nothing.
func TestEveryCommandAnswersItsOwnHelp(t *testing.T) {
	for _, command := range Commands() {
		for _, flag := range []string{"-h", "--help"} {
			t.Run(command.Name+flag, func(t *testing.T) {
				code, stdout, stderr, stateDir := execute(t, command.Name, flag)
				if code != exitcode.OK {
					t.Fatalf("%s %s exit = %d, want 0 (stderr = %s)", command.Name, flag, code, stderr)
				}
				if !strings.Contains(stdout, "dshctl "+command.Name+" —") {
					t.Fatalf("%s %s stdout = %q, want the command's own help", command.Name, flag, stdout)
				}
				if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
					t.Fatalf("%s %s created the state directory", command.Name, flag)
				}
			})
		}
	}
}

// TestEveryCommandRejectsUnexpectedArguments pins that no command silently
// ignores a stray word. The message names the command, so a copy-paste mistake
// in the shared helper would be visible.
func TestEveryCommandRejectsUnexpectedArguments(t *testing.T) {
	for _, command := range Commands() {
		t.Run(command.Name, func(t *testing.T) {
			code, stdout, stderr, _ := execute(t, command.Name, "stray-argument")
			if code != exitcode.Usage {
				t.Fatalf("%s exit = %d, want %d (stderr = %s)", command.Name, code, exitcode.Usage, stderr)
			}
			if !strings.Contains(stderr, "命令 "+command.Name+" 不接受位置参数") {
				t.Fatalf("%s stderr = %q, want it to name the command", command.Name, stderr)
			}
			if stdout != "" {
				t.Fatalf("%s wrote to stdout on a usage error: %q", command.Name, stdout)
			}
		})
	}
}

// TestEveryCommandRejectsAnUndefinedFlag pins that a global-scope flag written
// after the command name is refused where it stands, with the hint that says
// where it belongs.
func TestEveryCommandRejectsAnUndefinedFlag(t *testing.T) {
	for _, command := range Commands() {
		t.Run(command.Name, func(t *testing.T) {
			code, stdout, stderr, _ := execute(t, command.Name, "--definitely-not-a-flag")
			if code != exitcode.Usage {
				t.Fatalf("%s exit = %d, want %d (stderr = %s)", command.Name, code, exitcode.Usage, stderr)
			}
			if !strings.Contains(stderr, "definitely-not-a-flag") {
				t.Fatalf("%s stderr = %q, want it to name the flag", command.Name, stderr)
			}
			if !strings.Contains(stderr, "须写在命令名之前") {
				t.Fatalf("%s stderr = %q, want the placement hint", command.Name, stderr)
			}
			if stdout != "" {
				t.Fatalf("%s wrote to stdout on a usage error: %q", command.Name, stdout)
			}
		})
	}
}

// TestBareInvocationMeansStart pins the documented default: a bare `dshctl` runs
// start rather than printing usage, so an operator who types the tool's name gets
// the server.
func TestBareInvocationMeansStart(t *testing.T) {
	code, _, stderr, _ := execute(t)
	if code != exitcode.Preflight {
		t.Fatalf("exit = %d, want %d for a missing checkout (stderr = %s)", code, exitcode.Preflight, stderr)
	}
	if !strings.Contains(stderr, "仓库目录") {
		t.Fatalf("stderr = %q, want the checkout problem start refuses on", stderr)
	}
}

// TestVersionFlagShortCircuits pins the precedence: the version flag answers
// before the command name and before help are even looked at.
func TestVersionFlagShortCircuits(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-V"},
		{"-V", "status"},
		{"--version", "bogus-command"},
		{"--help", "--version"},
	} {
		code, stdout, stderr, _ := execute(t, args...)
		if code != exitcode.OK {
			t.Fatalf("%v: exit = %d, want 0 (stderr = %s)", args, code, stderr)
		}
		if !strings.Contains(stdout, "dshctl") {
			t.Fatalf("%v: stdout = %q, want the version report", args, stdout)
		}
		if strings.Contains(stdout, "命令:") {
			t.Fatalf("%v: version must not print the command list: %q", args, stdout)
		}
	}
}

// TestUnknownCommandWinsOverTheHelpFlag pins that help does not invent commands:
// asking for the help of something that does not exist is still a usage error.
func TestUnknownCommandWinsOverTheHelpFlag(t *testing.T) {
	for _, args := range [][]string{{"--help", "bogus-command"}, {"bogus-command", "--help"}} {
		code, _, stderr, _ := execute(t, args...)
		if code != exitcode.Usage {
			t.Fatalf("%v: exit = %d, want %d", args, code, exitcode.Usage)
		}
		if !strings.Contains(stderr, "未知命令") {
			t.Fatalf("%v: stderr = %q", args, stderr)
		}
	}
}

// TestHelpSubcommand pins how the help command reads its argument: one extra word
// is ignored, and a flag is a command name it does not know.
func TestHelpSubcommand(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "help", "build", "extra")
	if code != exitcode.OK {
		t.Fatalf("help build extra: exit = %d, want 0 (stderr = %s)", code, stderr)
	}
	if !strings.Contains(stdout, "dshctl build —") {
		t.Fatalf("help build extra: stdout = %q", stdout)
	}

	code, _, stderr, _ = execute(t, "help", "--json")
	if code != exitcode.Usage {
		t.Fatalf("help --json: exit = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(stderr, "未知命令") {
		t.Fatalf("help --json: stderr = %q", stderr)
	}
}

// TestGlobalFlagErrorsKeepStdoutClean pins that every global-flag mistake writes
// the diagnosis and the usage text to stderr. A script that captures stdout must
// never receive half a help page because a flag was misspelled.
func TestGlobalFlagErrorsKeepStdoutClean(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"},
		{"--port"},
		{"--port", "abc"},
		{"--port="},
		{"--repo="},
		{"--node="},
		{"--config="},
	} {
		code, stdout, stderr, _ := execute(t, args...)
		if code != exitcode.Usage {
			t.Fatalf("%v: exit = %d, want %d", args, code, exitcode.Usage)
		}
		if stdout != "" {
			t.Fatalf("%v: stdout = %q, want nothing on a usage error", args, stdout)
		}
		if !strings.HasPrefix(stderr, "错误: ") {
			t.Fatalf("%v: stderr = %q, want it to open with the error prefix", args, stderr)
		}
		for _, want := range []string{"命令:", "全局参数:", "退出码:"} {
			if !strings.Contains(stderr, want) {
				t.Fatalf("%v: stderr is missing %q:\n%s", args, want, stderr)
			}
		}
	}
}

// TestSingleDashHelpIsAFlagHelpRequest pins the behaviour of `-help`, which Go's
// flag package accepts as a help request even though the command line documents
// only `-h`/`--help`.
//
// It is pinned rather than endorsed: the flag package answers it by printing the
// flag usage to stderr while the command exits successfully with an empty stdout,
// which is a different shape from `--help`. Recording the difference is what
// makes a later decision to unify the two visible instead of silent.
func TestSingleDashHelpIsAFlagHelpRequest(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "status", "-help")
	if code != exitcode.OK {
		t.Fatalf("status -help exit = %d, want 0 (stderr = %s)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("status -help stdout = %q, want the flag usage on stderr", stdout)
	}
	if !strings.Contains(stderr, "用法: dshctl [全局参数] status") {
		t.Fatalf("status -help stderr = %q, want the flag usage", stderr)
	}

	// The global parser does not accept it at all, which is the asymmetry: the
	// same spelling means "help" after a command name and "unknown flag" before
	// one.
	code, _, stderr, _ = execute(t, "-help")
	if code != exitcode.Usage {
		t.Fatalf("-help exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
	}
}

// TestPortFlagBoundaries pins which ports the command line accepts and which it
// refuses as a usage error, at the two ends of the range and just outside it.
func TestPortFlagBoundaries(t *testing.T) {
	for _, port := range []string{"1", "65535"} {
		code, _, stderr, _ := execute(t, "--port", port, "status")
		if code != exitcode.NotRunning {
			t.Fatalf("--port %s: exit = %d, want %d (stderr = %s)", port, code, exitcode.NotRunning, stderr)
		}
	}
	for _, port := range []string{"0", "-1", "65536", "70000"} {
		code, stdout, stderr, _ := execute(t, "--port", port, "status")
		if code != exitcode.Usage {
			t.Fatalf("--port %s: exit = %d, want %d (stderr = %s)", port, code, exitcode.Usage, stderr)
		}
		if stdout != "" {
			t.Fatalf("--port %s: stdout = %q, want nothing", port, stdout)
		}
	}

	// A flag is not a value: the mistake is reported as a bad port rather than
	// swallowed as the token that follows.
	code, _, stderr, _ := execute(t, "--port", "--json", "status")
	if code != exitcode.Usage || !strings.Contains(stderr, "不是数字") {
		t.Fatalf("--port --json: exit = %d, stderr = %q", code, stderr)
	}

	// The last occurrence wins, which is what every other command line does.
	held := holdLoopbackPort(t)
	code, stdout, stderr, _ := execute(t, "--port", "1", "--port", strconv.Itoa(held), "status")
	if code != exitcode.NotRunning {
		t.Fatalf("repeated --port: exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stdout, strconv.Itoa(held)) {
		t.Fatalf("repeated --port: stdout = %q, want it to report port %d", stdout, held)
	}
}

// TestSettingsFlagsBeatTheEnvironment pins the documented precedence at the
// command line rather than at the parser: a flag is echoed with its own source.
func TestSettingsFlagsBeatTheEnvironment(t *testing.T) {
	repo := t.TempDir()
	code, _, stderr, _ := executeWith(t, map[string]string{
		"DSH_REPO_DIR":     filepath.Join(t.TempDir(), "from-environment"),
		"DSH_NODE_VERSION": "24.20.0",
	}, "--repo", repo, "--node", "26.1.0", "-v", "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stderr, "仓库目录: "+repo+" (flag)") {
		t.Fatalf("stderr = %q, want the flag's repository and its source", stderr)
	}
	if !strings.Contains(stderr, "Node 版本: 26.1.0 (flag)") {
		t.Fatalf("stderr = %q, want the flag's node version and its source", stderr)
	}
}

// TestVerboseDescribesEverySetting pins the full verbose report. It is the answer
// to "which value is in effect, and where did it come from", so a line that
// disappears takes away the only way to tell a flag from a default.
func TestVerboseDescribesEverySetting(t *testing.T) {
	code, _, stderr, _ := execute(t, "-v", "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	for _, want := range []string{
		"配置文件: ", "状态目录: ", "仓库目录: ", "监听端口: ", "Node 版本: ",
		"日志文件: ", "启动超时: ", "停止超时: ", "锁超时:   ", "日志轮转: ",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("verbose output is missing %q:\n%s", want, stderr)
		}
	}
	if lines := strings.Count(stderr, "\n"); lines != 10 {
		t.Fatalf("verbose output has %d lines, want 10:\n%s", lines, stderr)
	}
}

// TestMutatingCommandsRefuseWithoutACheckout pins that every command that would
// touch the checkout fails at its precondition instead of doing half the work.
func TestMutatingCommandsRefuseWithoutACheckout(t *testing.T) {
	for _, command := range []string{"start", "restart", "build", "update"} {
		t.Run(command, func(t *testing.T) {
			empty := t.TempDir()
			code, _, stderr, stateDir := execute(t, "--repo", empty, command)
			if code != exitcode.Preflight {
				t.Fatalf("%s exit = %d, want %d (stderr = %s)", command, code, exitcode.Preflight, stderr)
			}
			if !strings.Contains(stderr, empty) {
				t.Fatalf("%s stderr = %q, want it to name the checkout", command, stderr)
			}
			if records := runtimeRecords(t, stateDir); len(records) != 0 {
				t.Fatalf("%s left a runtime record behind: %v", command, records)
			}
		})
	}
}

// TestStopLeavesAForeignPortAlone is the safety property the whole tool exists
// for, at the level an operator types it: a port held by something dshctl did not
// start is reported and never signalled.
func TestStopLeavesAForeignPortAlone(t *testing.T) {
	port := holdLoopbackPort(t)
	code, stdout, stderr, _ := executeWith(t, map[string]string{"DSH_PORT": strconv.Itoa(port)}, "stop")
	if code != exitcode.OK {
		t.Fatalf("stop exit = %d, want 0 (stderr = %s)", code, stderr)
	}
	if !portAcceptsConnections(t, port) {
		t.Fatal("stop ended a process it did not start")
	}
	// The occupant is reported, and the report must name the port.
	if !strings.Contains(stderr, strconv.Itoa(port)) {
		t.Fatalf("stderr = %q, want the occupant reported", stderr)
	}
	if strings.Contains(stdout, "已停止") {
		t.Fatalf("stop claimed a success it did not have: %q", stdout)
	}
}

// TestStartAndRestartRefuseAHeldPort pins that neither command takes a port from
// another program, and that refusing is all they do: the socket still answers and
// no record was written that would claim the server.
func TestStartAndRestartRefuseAHeldPort(t *testing.T) {
	for _, command := range []string{"start", "restart"} {
		t.Run(command, func(t *testing.T) {
			port := holdLoopbackPort(t)
			code, _, stderr, stateDir := executeWith(t,
				map[string]string{"DSH_PORT": strconv.Itoa(port)}, command)
			if code != exitcode.Preflight {
				t.Fatalf("%s exit = %d, want %d (stderr = %s)", command, code, exitcode.Preflight, stderr)
			}
			if !strings.Contains(stderr, strconv.Itoa(port)) {
				t.Fatalf("%s stderr = %q, want it to name the port", command, stderr)
			}
			if !portAcceptsConnections(t, port) {
				t.Fatalf("%s ended the process holding the port", command)
			}
			if records := runtimeRecords(t, stateDir); len(records) != 0 {
				t.Fatalf("%s claimed the port by writing a record: %v", command, records)
			}
		})
	}
}

// TestStatusAndDoctorAgreeAboutAHeldPort pins that the two reporting commands
// answer from one observation: the same port must be described as occupied by
// both, and neither may call it free or ours.
func TestStatusAndDoctorAgreeAboutAHeldPort(t *testing.T) {
	port := holdLoopbackPort(t)
	environment := map[string]string{"DSH_PORT": strconv.Itoa(port)}

	code, stdout, stderr, _ := executeWith(t, environment, "status")
	if code != exitcode.NotRunning {
		t.Fatalf("status exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stdout, "端口") || !strings.Contains(stdout, strconv.Itoa(port)) {
		t.Fatalf("status stdout = %q, want the port reported as occupied", stdout)
	}
	if strings.Contains(stdout, "状态: 运行中") {
		t.Fatalf("status claimed a server of ours on a foreign port: %q", stdout)
	}

	code, stdout, _, _ = executeWith(t, environment, "doctor", "--json")
	if code != exitcode.Failure {
		t.Fatalf("doctor exit = %d, want %d for a missing checkout", code, exitcode.Failure)
	}
	var checks []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(stdout), &checks); err != nil {
		t.Fatalf("doctor --json is not JSON: %v\n%s", err, stdout)
	}
	var portDetail, recordDetail string
	for index, check := range checks {
		if check.Name == "" || check.Status == "" || check.Detail == "" {
			t.Fatalf("check %d is incomplete: %+v", index, check)
		}
		switch check.Name {
		case "端口":
			portDetail = check.Detail
			if check.Status == "ok" {
				t.Fatalf("doctor called a foreign port healthy: %+v", check)
			}
		case "运行记录":
			recordDetail = check.Detail
		}
	}
	if !strings.Contains(portDetail, strconv.Itoa(port)) {
		t.Fatalf("doctor's port row = %q, want it to name %d", portDetail, port)
	}
	if !strings.Contains(recordDetail, "不存在") {
		t.Fatalf("doctor's record row = %q, want a fresh state directory to have no record", recordDetail)
	}
}

// TestURLWithoutAServerIsANotRunningError pins that the address is only handed
// out while a server of ours is running, and that the refusal uses the same "not
// running" vocabulary `status` uses rather than a generic failure.
func TestURLWithoutAServerIsANotRunningError(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "url")
	if code != exitcode.NotRunning {
		t.Fatalf("url exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if stdout != "" {
		t.Fatalf("url printed an address with no server running: %q", stdout)
	}
	if !strings.Contains(stderr, "未在运行") {
		t.Fatalf("url stderr = %q, want the not-running explanation", stderr)
	}
}

// TestLogsLineCount pins the -n flag at its boundaries, including the documented
// "values below 1 use the default" rule.
func TestLogsLineCount(t *testing.T) {
	lines := make([]string, 0, 250)
	for index := 0; index < 250; index++ {
		lines = append(lines, fmt.Sprintf("line-%03d", index))
	}
	const defaultLines = 200
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"short", []string{"logs", "-n", "3"}, lines[247:]},
		{"double dash", []string{"logs", "--n", "3"}, lines[247:]},
		{"equals form", []string{"logs", "-n=3"}, lines[247:]},
		{"exactly the file", []string{"logs", "-n", "250"}, lines},
		{"zero means the default", []string{"logs", "-n", "0"}, lines[len(lines)-defaultLines:]},
		{"negative means the default", []string{"logs", "-n", "-5"}, lines[len(lines)-defaultLines:]},
		{"the default itself", []string{"logs"}, lines[len(lines)-defaultLines:]},
		{"more than the file holds", []string{"logs", "-n", "1000"}, lines},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			environment, _ := freshEnvironment(t, nil)
			seedLogAt(t, environment, lines...)

			var stdout, stderr strings.Builder
			code := Main(context.Background(), testCase.args, &stdout, &stderr, os.Getenv)
			if code != exitcode.OK {
				t.Fatalf("exit = %d, want 0 (stderr = %s)", code, stderr.String())
			}
			want := strings.Join(testCase.want, "\n") + "\n"
			if stdout.String() != want {
				t.Fatalf("stdout = %s, want %s", summarise(stdout.String()), summarise(want))
			}
		})
	}
}

// summarise renders a block of lines compactly for a failure message.
func summarise(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) <= 4 {
		return strconv.Quote(text)
	}
	return fmt.Sprintf("%d lines, %q .. %q", len(lines), lines[0], lines[len(lines)-1])
}

// TestLogsRejectsAnUnparsableCount pins that a non-numeric count is a usage error
// rather than a silently ignored flag.
func TestLogsRejectsAnUnparsableCount(t *testing.T) {
	code, _, stderr, _ := execute(t, "logs", "-n", "abc")
	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Usage, stderr)
	}
}

// TestLogsBuildSection pins `logs --build`: the last build record is printed
// without its marker, and a log that holds no record is a note rather than an
// error.
func TestLogsBuildSection(t *testing.T) {
	environment, _ := freshEnvironment(t, nil)
	logPath := seedLogAt(t, environment, "服务输出一行")
	logger := logfile.New(logPath, 0)
	for _, step := range []struct {
		title string
		lines []string
	}{
		{title: "start", lines: []string{"启动输出"}},
		{title: "build", lines: []string{"构建第一行", "构建第二行"}},
		{title: "start", lines: []string{"再次启动"}},
	} {
		if err := logger.Section(step.title); err != nil {
			t.Fatalf("section %s: %v", step.title, err)
		}
		for _, line := range step.lines {
			if err := logger.Line(line); err != nil {
				t.Fatalf("line: %v", err)
			}
		}
	}

	var stdout, stderr strings.Builder
	code := Main(context.Background(), []string{"logs", "--build"}, &stdout, &stderr, os.Getenv)
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want 0 (stderr = %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "构建第一行") || !strings.Contains(stdout.String(), "构建第二行") {
		t.Fatalf("stdout = %q, want the build record's body", stdout.String())
	}
	if strings.Contains(stdout.String(), "再次启动") || strings.Contains(stdout.String(), "dshctl build") {
		t.Fatalf("stdout = %q, want only the build record", stdout.String())
	}

	// A log with no build record answers with a note on stderr, not a failure.
	plain, _ := freshEnvironment(t, nil)
	seedLogAt(t, plain, "只有服务输出")
	stdout.Reset()
	stderr.Reset()
	code = Main(context.Background(), []string{"logs", "--build"}, &stdout, &stderr, os.Getenv)
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want 0 when there is no build record (stderr = %s)", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want nothing when there is no build record", stdout.String())
	}
	if !strings.Contains(stderr.String(), "没有 build/update 记录") {
		t.Fatalf("stderr = %q, want the explanation", stderr.String())
	}
}

// TestLogsFollowEndsOnCancellation pins that a cancelled context ends the follow
// with the conventional interrupt status instead of an error report, for both
// ways a context is cancelled.
func TestLogsFollowEndsOnCancellation(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"cancel", func() (context.Context, context.CancelFunc) {
			// Cancelled before the command runs: the follow must notice at
			// once rather than block until the log grows.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}},
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 0)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			environment, _ := freshEnvironment(t, nil)
			seedLogAt(t, environment, "第一行")
			ctx, cancel := testCase.ctx()
			defer cancel()

			var stdout, stderr strings.Builder
			code := Main(ctx, []string{"logs", "--follow"}, &stdout, &stderr, os.Getenv)
			if code != exitcode.Interrupted {
				t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.Interrupted, stderr.String())
			}
			if !strings.Contains(stdout.String(), "第一行") {
				t.Fatalf("stdout = %q, want the tail before the cancellation", stdout.String())
			}
		})
	}
}

// TestVersionSurvivesABrokenConfiguration pins that the one command that must
// always answer does not depend on the configuration being readable.
func TestVersionSurvivesABrokenConfiguration(t *testing.T) {
	// A directory where the config file belongs: reading it fails on every
	// platform, and resolving it fails before any command that needs settings.
	broken := filepath.Join(t.TempDir(), "config.json")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	code, stdout, stderr, _ := executeWith(t, map[string]string{"DSHCTL_CONFIG": broken}, "version")
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want 0 (stderr = %s)", code, stderr)
	}
	if !strings.Contains(stdout, "dshctl") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// TestStatusJSONCarriesTheStateMachineFields pins the machine-readable contract
// scripts branch on: the keys are the API, so a rename has to fail here.
func TestStatusJSONCarriesTheStateMachineFields(t *testing.T) {
	code, stdout, stderr, _ := execute(t, "status", "--json")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	for _, key := range []string{
		"state", "url", "port", "ready", "recordLive", "recordStale",
		"repoDir", "repoReady", "buildReady", "logPath", "lockHeld",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("status JSON is missing %q: %s", key, stdout)
		}
	}
	if decoded["state"] != "stopped" {
		t.Fatalf("state = %v, want stopped for an empty state directory", decoded["state"])
	}
	// The token must not leak into a report about a server that is not running.
	if token, ok := decoded["urlWithToken"]; ok && token != "" {
		t.Fatalf("status handed out an address token with no server running: %s", stdout)
	}
}

// TestNodeFlagKeepsANumericVersion pins that a numeric node version is echoed as
// a version rather than mistaken for the port: the two settings are easy to
// transpose on a command line, and silently serving on the wrong port would be
// the worst way to find out.
func TestNodeFlagKeepsANumericVersion(t *testing.T) {
	code, _, stderr, _ := execute(t, "--node", "3080", "-v", "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d: a numeric node version is odd but not a usage error (stderr = %s)",
			code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stderr, "Node 版本: 3080") {
		t.Fatalf("stderr = %q, want the node version echoed", stderr)
	}
}

// TestConfigFlagPointsAtAnotherDocument pins that --config replaces only the
// document, while the rest of the environment stays in effect.
//
// The environment's port and node version are cleared for this test: they would
// otherwise win over the document, and the point here is that the document is
// the layer the flag selects.
func TestConfigFlagPointsAtAnotherDocument(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "custom.json")
	port := holdLoopbackPort(t)
	body := `{"port": ` + strconv.Itoa(port) + `, "nodeVersion": "26.2.0"}`
	if err := os.WriteFile(document, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code, stdout, stderr, _ := executeWith(t, map[string]string{
		"DSH_REPO_DIR":     filepath.Join(root, "repo"),
		"DSH_PORT":         "",
		"DSH_NODE_VERSION": "",
	}, "--config", document, "-v", "status")
	if code != exitcode.NotRunning {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, exitcode.NotRunning, stderr)
	}
	if !strings.Contains(stderr, "监听端口: "+strconv.Itoa(port)+" (file)") {
		t.Fatalf("stderr = %q, want the port from the document", stderr)
	}
	if !strings.Contains(stderr, "Node 版本: 26.2.0 (file)") {
		t.Fatalf("stderr = %q, want the node version from the document", stderr)
	}
	if !strings.Contains(stdout, strconv.Itoa(port)) {
		t.Fatalf("stdout = %q, want the document's port probed", stdout)
	}
}
