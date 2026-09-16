//go:build unix

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
)

// TestBinaryManagesServersOnSeveralPorts is the end-to-end regression test for
// the reported bug, and it runs the sequence exactly as it was reported:
//
//	dshctl --port <B> start      # one server, named on the command line
//	dshctl start                 # a second one, on the port the file names
//	dshctl stop                  # both must be ended, both records retired
//
// Before the fix the second start overwrote nothing but the *view*: `status`
// and `stop` looked only at the configured port, so the server started with
// `--port` kept serving while dshctl reported that nothing was running and
// refused to end it. The failure was permanent — nothing in dshctl could name
// that process again.
func TestBinaryManagesServersOnSeveralPorts(t *testing.T) {
	def := newMultiportFixture(t)
	configured, named, run, stateDir := def.configured, def.named, def.run, def.stateDir

	namedStart := run("--port", strconv.Itoa(named), "start")
	if namedStart.code != 0 {
		t.Fatalf("start on %d exit = %d\nstdout=%s\nstderr=%s",
			named, namedStart.code, namedStart.stdout, namedStart.stderr)
	}
	configuredStart := run("start")
	if configuredStart.code != 0 {
		t.Fatalf("start on %d exit = %d\nstdout=%s\nstderr=%s",
			configured, configuredStart.code, configuredStart.stdout, configuredStart.stderr)
	}

	// status must report the instance it was asked about on standard output and
	// name every other one beside it, so a server started with a port of its own
	// cannot be silently absent from the report.
	status := run("status")
	if status.code != 0 {
		t.Fatalf("status exit = %d, stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}
	if !strings.Contains(status.stdout, "运行中") {
		t.Fatalf("status does not report the configured port as running:\n%s", status.stdout)
	}
	for _, port := range []int{configured, named} {
		if !strings.Contains(status.stdout+status.stderr, strconv.Itoa(port)) {
			t.Fatalf("status never names port %d:\nstdout=%s\nstderr=%s", port, status.stdout, status.stderr)
		}
	}
	// Where a token appears is the contract pinned above; that it appears at all
	// is this assertion. The report is one document split over two streams, so
	// the address of the named instance is looked for in both.
	if !strings.Contains(status.stdout+status.stderr, "token=PORT-"+strconv.Itoa(named)) {
		t.Fatalf("status never hands out the address of port %d:\nstdout=%s\nstderr=%s\n%s",
			named, status.stdout, status.stderr, def.describe(t))
	}

	// The token of each port must be reachable: naming the port selects that
	// instance, and a bare `url` reports every running one.
	for _, port := range []int{configured, named} {
		address := run("--port", strconv.Itoa(port), "url")
		if address.code != 0 {
			t.Fatalf("url on %d exit = %d, stderr=%s", port, address.code, address.stderr)
		}
		if want := "token=PORT-" + strconv.Itoa(port); !strings.Contains(address.stdout, want) {
			t.Fatalf("url on %d = %q, want it to carry %q", port, address.stdout, want)
		}
	}
	everyAddress := run("url")
	if everyAddress.code != 0 {
		t.Fatalf("url exit = %d, stderr=%s", everyAddress.code, everyAddress.stderr)
	}
	for _, port := range []int{configured, named} {
		if want := "token=PORT-" + strconv.Itoa(port); !strings.Contains(everyAddress.stdout, want) {
			t.Fatalf("url never reports port %d:\n%s", port, everyAddress.stdout)
		}
	}

	// Naming one port stops exactly that instance: the other keeps serving.
	namedStop := run("--port", strconv.Itoa(named), "stop")
	if namedStop.code != 0 {
		t.Fatalf("stop on %d exit = %d, stderr=%s", named, namedStop.code, namedStop.stderr)
	}
	if listenerAlive(named) {
		t.Fatalf("port %d still has a listener after its stop", named)
	}
	if !listenerAlive(configured) {
		t.Fatalf("stopping port %d also took down port %d", named, configured)
	}

	// A bare stop ends what is left of this state directory, whatever port it
	// was started on. This is the reported failure: it used to report "not
	// running" and leave the server alone.
	bareStop := run("stop")
	if bareStop.code != 0 {
		t.Fatalf("stop exit = %d, stderr=%s", bareStop.code, bareStop.stderr)
	}
	if listenerAlive(configured) {
		t.Fatalf("port %d still has a listener after a bare stop", configured)
	}
	if after := run("status"); after.code != 3 {
		t.Fatalf("status after a bare stop exit = %d, stdout=%s stderr=%s",
			after.code, after.stdout, after.stderr)
	}

	// No record may survive a clean stop: a leftover record is how a machine
	// keeps reporting a server that is not there.
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	var records []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".state.json") {
			records = append(records, entry.Name())
		}
	}
	if len(records) != 0 {
		t.Fatalf("records were left behind after a clean stop: %v", records)
	}
}

// TestBinaryReportsAndStopsEveryInstanceWithoutAPort is the multi-instance
// contract at the level an operator types it: with no port named, `status`
// describes every server this state directory manages, `url` hands out every
// address, and `stop` ends every one of them.
//
// The fixture is the same installation as the regression test above, driven from
// the other end. There, ports are named and the assertions check that naming
// narrows; here nothing is named except at the start, and the assertions check
// that a server dshctl started is never invisible to it.
func TestBinaryReportsAndStopsEveryInstanceWithoutAPort(t *testing.T) {
	def := newMultiportFixture(t)
	configured, named, other, run := def.configured, def.named, def.other, def.run

	// The configured port is never started. The servers live on ports that were
	// named on the command line, which is exactly the machine where a view built
	// on the configured port sees neither of them.
	for _, port := range []int{named, other} {
		started := run("--port", strconv.Itoa(port), "start")
		if started.code != 0 {
			t.Fatalf("start on %d exit = %d\nstdout=%s\nstderr=%s",
				port, started.code, started.stdout, started.stderr)
		}
	}

	// status answers for the configured port on standard output — nothing is
	// running there — and names both servers that are running beside it, with
	// their addresses, so "nothing is running" is never the whole answer.
	status := run("status")
	if status.code != 3 {
		t.Fatalf("status exit = %d, want 3 for a stopped configured port\nstdout=%s\nstderr=%s\n%s",
			status.code, status.stdout, status.stderr, def.describe(t))
	}
	for _, port := range []int{named, other} {
		if !strings.Contains(status.stderr, "端口 "+strconv.Itoa(port)) {
			t.Fatalf("status never names port %d:\n%s\n%s", port, status.stderr, def.describe(t))
		}
		if want := "token=PORT-" + strconv.Itoa(port); !strings.Contains(status.stderr, want) {
			t.Fatalf("the status note for port %d does not carry %q:\n%s", port, want, status.stderr)
		}
	}
	if strings.Contains(status.stdout, strconv.Itoa(named)) || strings.Contains(status.stdout, strconv.Itoa(other)) {
		t.Fatalf("the configured port's own report claims another instance:\n%s", status.stdout)
	}

	// url is a pipeline command: one address per running server, and success
	// because it handed out addresses even though the configured port is down.
	addresses := run("url")
	if addresses.code != 0 {
		t.Fatalf("url exit = %d, want 0 once an address was printed\nstderr=%s",
			addresses.code, addresses.stderr)
	}
	for _, port := range []int{named, other} {
		if want := "token=PORT-" + strconv.Itoa(port); !strings.Contains(addresses.stdout, want) {
			t.Fatalf("url never reports port %d:\n%s", port, addresses.stdout)
		}
	}

	// A bare stop ends every one of them, including the port this command was
	// never told about, and leaves no record claiming otherwise.
	stopped := run("stop")
	if stopped.code != 0 {
		t.Fatalf("stop exit = %d\nstdout=%s\nstderr=%s", stopped.code, stopped.stdout, stopped.stderr)
	}
	for _, port := range []int{configured, named, other} {
		if listenerAlive(port) {
			t.Fatalf("port %d still has a listener after a bare stop", port)
		}
	}
	if after := run("status"); after.code != 3 {
		t.Fatalf("status after a bare stop exit = %d, stdout=%s stderr=%s",
			after.code, after.stdout, after.stderr)
	}
	entries, err := os.ReadDir(def.stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".state.json") {
			t.Fatalf("a clean stop left the record %s behind", entry.Name())
		}
	}
}

// multiportFixture is one installation with a settings document naming one port,
// and a way to run the real binary against it.
//
// It is one fixture rather than a copy per test because both tests are about the
// same machine: several servers, one state directory, and a command line that
// may or may not name a port. `configured` is the port `config.json` names;
// `named` and `other` are free ports a test starts servers on.
type multiportFixture struct {
	configured  int
	named       int
	other       int
	stateDir    string
	environment []string
	run         func(args ...string) invocation
}

// newMultiportFixture builds the installation, the node and pnpm stubs the child
// runs, and the environment every invocation is given.
//
// Every server a test starts is ended by the cleanup, so a failure in the middle
// of a test cannot leave a detached process serving on the machine that ran it.
func newMultiportFixture(t *testing.T) multiportFixture {
	t.Helper()
	realNode, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		// The assertions read each port back through lsof. Without it the probe
		// would answer "no listener" for every port and the assertions would
		// pass vacuously, which is worse than not running them.
		t.Skip("lsof is unavailable")
	}

	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	stateDir := filepath.Join(root, "state")
	home := filepath.Join(root, "home")
	for _, dir := range []string{
		filepath.Join(repoDir, "node_modules", ".bin"),
		filepath.Join(repoDir, ".dsh-build"),
		home,
		stateDir,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeFile := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeFile(filepath.Join(repoDir, "package.json"), `{"name":"deepseek-harness"}`)
	writeFile(filepath.Join(repoDir, "pnpm-workspace.yaml"), "packages:\n  - packages/*/*\n")
	writeFile(filepath.Join(repoDir, ".dsh-build", "client-build-environment.json"), "{}")

	// The server announces its own address the way the real one does, so the
	// `url` assertions read a token that belongs to the port that printed it.
	//
	// The writes are synchronous on purpose. A pipe leaves stdout buffered, so
	// console.log would still be sitting in the process's buffer at the moment
	// dshctl reads the log: on a fast machine the start finishes before node
	// flushes, the record is written without an address, and the test fails for a
	// reason that has nothing to do with several instances. The real server
	// flushes what it announces; the stub has to as well.
	server := filepath.Join(root, "server.js")
	writeFile(server, `
const fs = require('fs');
const net = require('net');
const port = Number(process.argv[process.argv.indexOf('--port') + 1]);
const announce = (line) => fs.writeSync(1, line + '\n');
announce('dsh web: http://127.0.0.1:' + port + '/?token=PORT-' + port);
net.createServer(() => {}).listen(port, '127.0.0.1', () => announce('listening'));
setInterval(() => {}, 1000);
`)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	// The node the child runs reports the release dshctl is verified against and
	// forwards everything else to the real interpreter, so this test does not
	// depend on the machine's own node clearing the version floor.
	node := filepath.Join(binDir, "node")
	writeFile(node, "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"  -v) echo v"+config.TestedNodeVersion+"; exit 0;;\n"+
		"  -p) echo \""+node+"\"; exit 0;;\n"+
		"esac\n"+
		"exec \""+realNode+"\" \"$@\"\n")
	if err := os.Chmod(node, 0o755); err != nil {
		t.Fatalf("chmod the node stub: %v", err)
	}
	// pnpm resolves through PATH for the child; the stub above is what runs.
	writeFile(filepath.Join(binDir, "pnpm"), "#!/bin/sh\nexec "+node+" "+server+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(binDir, "pnpm"), 0o755); err != nil {
		t.Fatalf("chmod the pnpm stub: %v", err)
	}

	// The port the settings file names, and two to start on: the reported shape,
	// where the file decides what a bare command means and the command line
	// decides which instance it is about.
	configured := freePort(t)
	named := freePort(t)
	other := freePort(t)
	writeFile(filepath.Join(stateDir, "config.json"), `{"port": `+strconv.Itoa(configured)+`}`+"\n")

	// The port comes from the settings document rather than the environment:
	// DSH_PORT is a decision about one instance, and this test is about the
	// machine where nobody named one.
	environment := setEnvironment(binaryEnvironment(t, root, home, stateDir), "DSH_PORT", "")
	environment = append(environment,
		"DSH_REPO_DIR="+repoDir,
		"PATH="+binDir+string(os.PathListSeparator)+filepath.Dir(node)+string(os.PathListSeparator)+systemToolDirs,
	)
	run := func(args ...string) invocation {
		t.Helper()
		cmd := exec.Command(binary(t), args...)
		cmd.Dir = root
		// The environment is rebuilt for every call, so a variable exported by
		// an earlier invocation cannot leak into the next one.
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
				t.Fatalf("running %v: %v", args, err)
			}
			code = exit.ExitCode()
		}
		return invocation{code: code, stdout: stdout.String(), stderr: stderr.String()}
	}

	t.Cleanup(func() {
		for _, port := range []int{configured, named, other} {
			_ = run("--port", strconv.Itoa(port), "stop")
		}
	})
	return multiportFixture{
		configured: configured, named: named, other: other,
		stateDir: stateDir, environment: environment, run: run,
	}
}

// describe is the evidence a failure needs: what each port looks like from
// outside (a listener or not), what this state directory holds for it, what the
// binary reports, and what the servers announced.
//
// The properties under test are about things outside dshctl — a port that
// answers, a process that is gone — and "the assertion failed" is not enough to
// tell a defect from an environment in which the port was taken by somebody
// else between two commands. One snapshot turns the next failure into an answer
// instead of another run.
func (f multiportFixture) describe(t *testing.T) string {
	t.Helper()
	var report strings.Builder
	for _, port := range []int{f.configured, f.named, f.other} {
		fmt.Fprintf(&report, "port %d: listening=%v record=%v\n",
			port, listenerAlive(port), recordNames(t, f.stateDir, port))
	}
	status := f.run("status", "--json")
	fmt.Fprintf(&report, "status --json: exit=%d\n%s\n%s\n", status.code, status.stdout, status.stderr)
	if entries, err := os.ReadDir(f.stateDir); err == nil {
		for _, entry := range entries {
			if !strings.Contains(entry.Name(), ".state.json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(f.stateDir, entry.Name()))
			if err != nil {
				continue
			}
			fmt.Fprintf(&report, "%s: %s\n", entry.Name(), data)
		}
	}
	data, err := os.ReadFile(filepath.Join(f.stateDir, "dsh-web.log"))
	if err == nil {
		fmt.Fprintf(&report, "log:\n%s\n", data)
	}
	for _, port := range []int{f.configured, f.named, f.other} {
		record, err := os.ReadFile(filepath.Join(f.stateDir, fmt.Sprintf("dsh-web-%d.state.json", port)))
		if err == nil {
			fmt.Fprintf(&report, "record %d: %s\n", port, record)
		}
	}
	return report.String()
}

// recordNames lists the runtime records this state directory holds for one port.
func recordNames(t *testing.T, stateDir string, port int) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(stateDir, fmt.Sprintf("dsh-web-%d.state.json", port)))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, filepath.Base(match))
	}
	return names
}
