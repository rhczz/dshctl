//go:build unix

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTwoPortsShareOneStateDirectoryWithoutOrphaningEachOther is the regression
// test for a single runtime record serving several ports.
//
// The failure it guards against was silent: the second start overwrote the
// first server's record, so the first server kept serving while dshctl could no
// longer recognize it — `status` called it unmanaged and `stop` refused to touch
// it, leaving a server nobody could stop through the tool.
func TestTwoPortsShareOneStateDirectoryWithoutOrphaningEachOther(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		// The stop assertions below verify the port through lsof. Without it
		// the probe would answer "no listener" for every port and the
		// assertions would pass vacuously.
		t.Skip("lsof is unavailable")
	}
	// A state directory, one checkout, two ports.
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	stateDir := filepath.Join(root, "state")
	home := filepath.Join(root, "home")
	for _, dir := range []string{filepath.Join(repoDir, "node_modules", ".bin"), filepath.Join(repoDir, ".dsh-build"), home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(filepath.Join(repoDir, "package.json"), `{"name":"deepseek-harness"}`)
	write(filepath.Join(repoDir, "pnpm-workspace.yaml"), "packages:\n  - packages/*/*\n")
	write(filepath.Join(repoDir, ".dsh-build", "client-build-environment.json"), "{}")

	server := filepath.Join(root, "server.js")
	write(server, `
const net = require('net');
const port = Number(process.argv[process.argv.indexOf('--port') + 1]);
console.log('dsh web: http://127.0.0.1:' + port + '/?token=PORT-' + port);
net.createServer(() => {}).listen(port, '127.0.0.1', () => console.log('listening'));
setInterval(() => {}, 1000);
`)
	// pnpm resolves through PATH for the child; the real binary is used.
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	write(filepath.Join(binDir, "pnpm"), "#!/bin/sh\nexec "+node+" "+server+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(binDir, "pnpm"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	portA := freePort(t)
	portB := freePort(t)
	environment := func(port int) []string {
		return append(binaryEnvironment(t, root, home, stateDir),
			"DSH_REPO_DIR="+repoDir,
			"DSH_PORT="+strconv.Itoa(port),
			"PATH="+binDir+string(os.PathListSeparator)+filepath.Dir(node)+string(os.PathListSeparator)+systemToolDirs,
		)
	}
	runPort := func(port int, args ...string) invocation {
		t.Helper()
		cmd := exec.Command(binary(t), args...)
		cmd.Dir = root
		// The environment is rebuilt for each call, so a stale DSH_PORT from a
		// previous invocation cannot leak into the next one.
		cmd.Env = environment(port)
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

	startA := runPort(portA, "start")
	if startA.code != 0 {
		t.Fatalf("start A exit = %d\nstdout=%s\nstderr=%s", startA.code, startA.stdout, startA.stderr)
	}
	// The cleanup is registered the moment the first server exists, so even a
	// failure in the second start cannot leave a detached, forever-serving
	// process on the machine. Stopping a port that never started is a harmless
	// "not running" no-op.
	t.Cleanup(func() {
		for _, port := range []int{portA, portB} {
			_ = runPort(port, "stop")
		}
	})

	startB := runPort(portB, "start")
	if startB.code != 0 {
		t.Fatalf("start B exit = %d\nstdout=%s\nstderr=%s", startB.code, startB.stdout, startB.stderr)
	}

	// Both ports must still answer after the second start.
	for _, port := range []int{portA, portB} {
		status := runPort(port, "status")
		if status.code != 0 {
			t.Fatalf("status on %d exit = %d, stdout=%s stderr=%s", port, status.code, status.stdout, status.stderr)
		}
		if !strings.Contains(status.stdout, "运行中") {
			t.Fatalf("port %d is no longer recognized as running:\n%s", port, status.stdout)
		}
		if !strings.Contains(status.stdout, strconv.Itoa(port)) {
			t.Fatalf("port %d status does not name its own address:\n%s", port, status.stdout)
		}
	}

	// And each port must be able to stop its own server without touching the other.
	stopA := runPort(portA, "stop")
	if stopA.code != 0 {
		t.Fatalf("stop A exit = %d, stderr=%s", stopA.code, stopA.stderr)
	}
	if listenerAlive(portA) {
		t.Fatalf("port %d still has a listener after stop", portA)
	}
	if !listenerAlive(portB) {
		t.Fatalf("stopping port %d also took down port %d", portA, portB)
	}
	if final := runPort(portB, "status"); !strings.Contains(final.stdout, "运行中") {
		t.Fatalf("port %d lost its server:\n%s", portB, final.stdout)
	}
	if stop := runPort(portB, "stop"); stop.code != 0 {
		t.Fatalf("stop B exit = %d, stderr=%s", stop.code, stop.stderr)
	}
	if listenerAlive(portB) {
		t.Fatalf("port %d still has a listener after stop", portB)
	}

	// One record per port, and no leftovers.
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

// systemToolDirs names where the operating system's own tools live. The child
// process needs them; nothing else from the surrounding PATH is inherited.
const systemToolDirs = "/usr/bin:/bin:/usr/sbin:/sbin"

// listenerAlive reports whether something listens on a loopback port.
func listenerAlive(port int) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("lsof", "-nP", "-ti", "tcp:"+strconv.Itoa(port), "-sTCP:LISTEN")
		if out, err := cmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
