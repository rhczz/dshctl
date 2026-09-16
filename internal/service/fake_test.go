package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/logfile"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/repo"
	"github.com/rhczz/dshctl/internal/run"
	"github.com/rhczz/dshctl/internal/state"
)

// fixtureStartTime is the start time every fictional process shares, so that a
// record built by a test and a process spawned by the service agree on the
// fingerprint unless a test deliberately makes them differ.
const fixtureStartTime int64 = 1_700_000_000

// fakeProcess is one entry of the fictional process table.
type fakeProcess struct {
	command   string
	startedAt int64
	alive     bool
	// group is the process group the process belongs to, or 0 for "its own".
	group int
	// survivesGraceful makes the process ignore a graceful request, which is how
	// a test reaches the force step of a stop.
	survivesGraceful bool
	// survivesForce makes the process ignore the force request too, which is
	// how a test reaches the "still alive after force" report. Unix cannot
	// ignore SIGKILL, so this models the Windows path: TerminateProcess can
	// fail, and the report must exist for it.
	survivesForce bool
	// onGraceful runs instead of killing the process when the first graceful
	// request arrives. It models a pid that is recycled during the grace period:
	// the hook swaps in a different process under the same pid.
	onGraceful func()
}

// fakeSignal is one delivered signal.
type fakeSignal struct {
	pid     int
	request host.Request
}

// fakeHost is a fictional machine: a process table, a listener, and the
// commands the service runs.
type fakeHost struct {
	mu sync.Mutex
	// processes is the process table, keyed by pid.
	processes map[int]*fakeProcess
	// listener is the pid holding the port, or 0 when nothing does.
	listener int
	// listenersByPort models other ports: when a port is present here, that pid
	// owns it, which is what the cross-port guards are about. The fixture's own
	// port keeps using listener.
	listenersByPort map[int]int
	// strangerAfterSignal makes an unrelated process take the port the moment a
	// signal is delivered to the server holding it — the race a stop has to
	// tell apart from a server that refused to let go.
	strangerAfterSignal int
	// listenErr makes every port probe fail, which models a host whose tools
	// cannot look at the port at all.
	listenErr error
	// ready decides what the readiness dial answers.
	ready bool
	// commands records every command the service ran, in order.
	commands []string
	// signals records every delivered signal, in order.
	signals []fakeSignal
	// spawns records every spawn call with the exact arguments it was handed.
	// Without it a wrong --no-open, a wrong --port or a wrong working directory
	// would pass every test, because the fictional launcher does not read them.
	spawns []spawnCall
	// signalErr fails selected signals, which models a host whose termination
	// request could not be delivered.
	signalErr func(request host.Request) error
	// gitStatus is the answer to `git status --porcelain`: empty means a clean
	// worktree, anything else means the checkout has uncommitted changes.
	gitStatus string
	// fail lets one test fail selected commands.
	fail func(cmd run.Command) error
	// nextPID is handed to the next spawn.
	nextPID int
	// handedOut is every pid this fictional machine has ever given a process.
	// Process ids are identities: a port still mapped to a pid whose entry came
	// back to life as an unrelated process makes a stopped server look like an
	// intruder on its own port, which is a defect in the double rather than in
	// the code under test. The map is what makes that impossible instead of
	// unlikely.
	handedOut map[int]bool
	// spawnErr makes the next spawn fail.
	spawnErr error
	// spontaneouslyServed makes a spawned child take the port on its own.
	spontaneouslyServed bool
	// opaqueServed makes the spawned server bind the port in a way the
	// platform cannot attribute: Listening reports "something listens" with
	// no pid, which is what netstat answers on a host without lsof or ss.
	opaqueServed bool
	// unnameablePolls makes the next Listening calls report a listener whose
	// owner the platform will not name, and then answer normally again. It is
	// the transient shape a probe chain produces while a socket is being
	// created: the table probe sees it before the per-process probe does.
	unnameablePolls int
	// listenerIgnoresGrace makes the spontaneously served listener survive a
	// graceful request while the wrapper answers it, the shape of a cleanup
	// whose leader dies first.
	listenerIgnoresGrace bool
	// servedByListener and servedByWrapper record the two pids of the last
	// spontaneous spawn, so a test can assert on both.
	servedByListener int
	servedByWrapper  int
	// diesImmediately makes a spawned child exit right away.
	diesImmediately bool
	// trackedFiles is the NUL-separated answer to git ls-files.
	trackedFiles string
	// nodeVersion is the release the fictional node binary reports, and
	// nodeExecPath where it says it lives. They are the two answers a probe
	// gets, and they decide what the resolver concludes.
	nodeVersion  string
	nodeExecPath string
	// probedNode records the questions put to a node binary, in order, so a test
	// can pin how many processes a resolution costs.
	probedNode []string
}

func newFakeHost() *fakeHost {
	// The release a machine's PATH node reports by default: the one dshctl is
	// verified against. Tests that need another one set it.
	const defaultNodeVersion = config.TestedNodeVersion
	return &fakeHost{
		processes:   map[int]*fakeProcess{},
		nextPID:     9000,
		ready:       true,
		nodeVersion: defaultNodeVersion,
	}
}

// spawnCall is one call the service made to the launcher: everything it decided
// the child should be.
type spawnCall struct {
	path string
	args []string
	dir  string
	env  []string
}

// spawnAttempt describes one request to start a process, recorded before the
// request is answered so that wantNoSpawn can see an attempt that failed.
type spawnAttempt struct {
	// pid is the process the attempt produced, or 0 when it failed.
	pid int
	// call is what the launcher was asked for.
	call spawnCall
}

// add puts a live process into the table.
func (h *fakeHost) add(pid int, command string, startedAt int64) *fakeProcess {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := &fakeProcess{command: command, startedAt: startedAt, alive: true}
	h.processes[pid] = entry
	return entry
}

// serving makes pid own the port.
func (h *fakeHost) serving(pid int, command string) {
	h.add(pid, command, fixtureStartTime)
	h.mu.Lock()
	h.listener = pid
	h.mu.Unlock()
}

// servingOnPort makes pid own a port other than the fixture's own. Two servers
// of one state directory is the shape every multi-instance rule is about, and
// the fixture has to be able to describe both at once.
func (h *fakeHost) servingOnPort(port, pid int, command string) {
	h.add(pid, command, fixtureStartTime)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listenersByPort == nil {
		h.listenersByPort = map[int]int{}
	}
	h.listenersByPort[port] = pid
}

// listen makes pid own the port without touching the process table.
func (h *fakeHost) listen(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listener = pid
}

// remove deletes a pid from the process table and releases the port it held,
// which is the state a process that exited leaves behind.
func (h *fakeHost) remove(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.processes, pid)
	if h.listener == pid {
		h.listener = 0
	}
}

// alive reports whether the table holds a live pid.
func (h *fakeHost) isAlive(pid int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.processes[pid]
	return ok && entry.alive
}

// Listening implements OsHost.
func (h *fakeHost) Listening(_ context.Context, port int) (host.PortResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listenErr != nil {
		return host.PortResult{}, h.listenErr
	}
	if h.unnameablePolls > 0 {
		h.unnameablePolls--
		return host.PortResult{Listening: true}, nil
	}
	if pid, ok := h.listenersByPort[port]; ok {
		if entry, alive := h.processes[pid]; !alive || !entry.alive {
			return host.PortResult{}, nil
		}
		return host.PortResult{Listening: true, PID: pid}, nil
	}
	// Ports the fixture was not told about fall back to its own listener, which
	// is how every single-instance test describes its port without naming it.

	switch {
	case h.listener == 0:
		return host.PortResult{}, nil
	case h.listener == -1:
		// Something listens, and the platform will not say what it is.
		return host.PortResult{Listening: true}, nil
	}
	if entry, ok := h.processes[h.listener]; !ok || !entry.alive {
		return host.PortResult{}, nil
	}
	return host.PortResult{Listening: true, PID: h.listener}, nil
}

// Inspect implements OsHost.
func (h *fakeHost) Inspect(_ context.Context, pid int) host.Facts {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.processes[pid]
	if !ok || !entry.alive {
		return host.Facts{PID: pid}
	}
	return host.Facts{PID: pid, Alive: true, StartedAt: entry.startedAt, Command: entry.command, Source: "fake"}
}

// Alive implements OsHost.
func (h *fakeHost) Alive(ctx context.Context, pid int) bool {
	return h.Inspect(ctx, pid).Alive
}

// Signal implements OsHost.
func (h *fakeHost) Signal(pid int, request host.Request) error {
	h.mu.Lock()
	h.signals = append(h.signals, fakeSignal{pid: pid, request: request})
	hook := h.signalErr
	h.mu.Unlock()
	if hook != nil {
		if err := hook(request); err != nil {
			return err
		}
	}
	h.mu.Lock()
	entry, ok := h.processes[pid]
	if !ok || !entry.alive {
		h.mu.Unlock()
		return nil
	}
	if request == host.Graceful && entry.survivesGraceful {
		hook := entry.onGraceful
		h.mu.Unlock()
		if hook != nil {
			hook()
		}
		return nil
	}
	if request == host.Force && entry.survivesForce {
		h.mu.Unlock()
		return nil
	}
	entry.alive = false
	if h.listener == pid {
		h.listener = 0
	}
	// An opaque listener is one the platform will not attribute, so it is
	// recorded as "something listens, owner unknown" rather than against a pid.
	// Ending the process that bound it frees the port, and the port probe has to
	// say so: leaving the port looking occupied made every cleanup after an
	// opaque listener wait out the whole stop timeout, which is a property of the
	// fixture rather than of the code under test.
	if h.listener == -1 && pid == h.servedByWrapper {
		h.listener = 0
	}
	if h.strangerAfterSignal > 0 {
		stranger := h.strangerAfterSignal
		h.processes[stranger] = &fakeProcess{
			command:   "/usr/sbin/nginx -g daemon off;",
			startedAt: fixtureStartTime,
			alive:     true,
		}
		h.listener = stranger
		h.strangerAfterSignal = 0
	}
	h.mu.Unlock()
	return nil
}

// DescendsFrom implements OsHost: the fictional machine puts every process in
// the group of the pid that spawned it.
//
// The real server is started through a package script, so the listener is a
// child of the wrapper rather than the wrapper itself, and descent is how the
// two are recognized as one unit.
func (h *fakeHost) DescendsFrom(ancestor, pid int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ancestor <= 0 || pid <= 0 {
		return false
	}
	entry, ok := h.processes[pid]
	if !ok || !entry.alive {
		return false
	}
	group := entry.group
	if group == 0 {
		group = pid
	}
	return pid == ancestor || group == ancestor
}

// GroupExists implements OsHost: the group exists while any live member is in
// it. Unlike DescendsFrom, a reaped leader does not make the group disappear —
// which is exactly the distinction waitForGroupExit depends on.
func (h *fakeHost) GroupExists(pid int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pid <= 0 {
		return false
	}
	for member, entry := range h.processes {
		if !entry.alive {
			continue
		}
		if member == pid || entry.group == pid {
			return true
		}
	}
	return false
}

// SignalGroup implements OsHost.
func (h *fakeHost) SignalGroup(pid int, request host.Request) error {
	h.mu.Lock()
	members := make([]int, 0, 2)
	for member, entry := range h.processes {
		if !entry.alive {
			continue
		}
		group := entry.group
		if group == 0 {
			group = member
		}
		if group == pid || member == pid {
			members = append(members, member)
		}
	}
	h.mu.Unlock()
	for _, member := range members {
		if err := h.Signal(member, request); err != nil {
			return err
		}
	}
	return nil
}

// KillGroup implements OsHost.
func (h *fakeHost) KillGroup(pid int) error {
	return h.SignalGroup(pid, host.Force)
}

// Run implements run.Executor for every external command.
func (h *fakeHost) Run(_ context.Context, cmd run.Command) error {
	h.mu.Lock()
	h.commands = append(h.commands, cmd.String())
	fail := h.fail
	h.mu.Unlock()
	if fail != nil {
		if err := fail(cmd); err != nil {
			return err
		}
	}
	switch programName(cmd.Name) {
	case "git":
		return h.answerGit(cmd)
	case "pnpm":
		if hasArgument(cmd, "--version") {
			return emit(cmd.Stdout, "11.0.0\n")
		}
		return nil
	}
	if strings.HasSuffix(cmd.Name, "pnpm") && hasArgument(cmd, "--version") {
		return emit(cmd.Stdout, "11.0.0\n")
	}
	return nil
}

// answerGit replies the way a healthy, clean checkout does.
func (h *fakeHost) answerGit(cmd run.Command) error {
	switch {
	case hasArgument(cmd, "rev-parse", "--short"):
		return emit(cmd.Stdout, "abc1234\n")
	case hasArgument(cmd, "rev-parse", "--abbrev-ref"):
		return emit(cmd.Stdout, "main\n")
	case hasArgument(cmd, "status", "--porcelain"):
		return emit(cmd.Stdout, h.status())
	}
	return nil
}

// status answers `git status --porcelain` from the fixture's script.
func (h *fakeHost) status() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gitStatus
}

// Capture answers a command's collected output.
//
// Implementing run.Capturer is what lets the fake host answer "what does git
// say" without a real git process, and it is why no test in this package can
// silently fall through to the operator's machine.
func (h *fakeHost) Capture(_ context.Context, cmd run.Command) run.Result {
	h.mu.Lock()
	h.commands = append(h.commands, "capture "+cmd.String())
	fail := h.fail
	h.mu.Unlock()
	if fail != nil {
		if err := fail(cmd); err != nil {
			return run.Result{Err: err}
		}
	}
	switch programName(cmd.Name) {
	case "git":
		switch {
		case hasArgument(cmd, "rev-parse", "--short"):
			return run.Result{Stdout: "abc1234"}
		case hasArgument(cmd, "rev-parse", "--abbrev-ref"):
			return run.Result{Stdout: "main"}
		case hasArgument(cmd, "ls-files"):
			return run.Result{Stdout: h.lsFiles()}
		case hasArgument(cmd, "status", "--porcelain"):
			return run.Result{Stdout: h.status()}
		default:
			return run.Result{}
		}
	case "node":
		return run.Result{Stdout: h.answerNode(cmd)}
	case "pnpm":
		return run.Result{Stdout: "11.0.0"}
	}
	return run.Result{}
}

// programName reports the program a command names, in the spelling the
// switches below match on.
//
// The fixture's node stub is spelled node.exe on Windows, and matching the base
// name against "node" therefore missed it: every resolution on that platform
// reported "no usable node", which is a fact about the fixture rather than about
// the code.
func programName(path string) string {
	name := filepath.Base(path)
	if name == fixtureNodeName() {
		return "node"
	}
	return name
}

// answerNode plays the two questions the resolver puts to a node binary.
//
// A real probe runs a real process; here the answers are the fixture's, which is
// what keeps every resolution test off the operator's machine. The questions are
// recorded so a test can pin what a resolution cost.
func (h *fakeHost) answerNode(cmd run.Command) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	probe := strings.Join(cmd.Args, " ")
	h.probedNode = append(h.probedNode, probe)
	if hasArgument(cmd, "-p") {
		if h.nodeExecPath == "" {
			return ""
		}
		return h.nodeExecPath + "\n"
	}
	return "v" + h.nodeVersion + "\n"
}

// nodeProbes returns the questions put to a node binary so far.
func (h *fakeHost) nodeProbes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.probedNode...)
}

// lsFiles answers the prune's tracking query from the fixture's script.
func (h *fakeHost) lsFiles() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.trackedFiles
}

// commandsRun returns a copy of the command log.
func (h *fakeHost) commandsRun() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.commands...)
}

// signalsSent returns a copy of the signal log.
func (h *fakeHost) signalsSent() []fakeSignal {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeSignal(nil), h.signals...)
}

// syncBuffer is a writer a background follower and the test can both use.
//
// A plain strings.Builder would be a data race the moment a command writes from
// its own goroutine, which is exactly what the follow path does.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write implements io.Writer.
func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

// String returns what has been written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// unknownFingerprint wraps a host and hides process start times, which is what a
// host looks like when a security policy denies reading process information.
type unknownFingerprint struct{ inner OsHost }

// Listening implements OsHost.
func (u unknownFingerprint) Listening(ctx context.Context, port int) (host.PortResult, error) {
	return u.inner.Listening(ctx, port)
}

// Inspect implements OsHost, reporting a live process with no start time.
func (u unknownFingerprint) Inspect(ctx context.Context, pid int) host.Facts {
	facts := u.inner.Inspect(ctx, pid)
	facts.StartedAt = 0
	return facts
}

// Alive implements OsHost.
func (u unknownFingerprint) Alive(ctx context.Context, pid int) bool { return u.inner.Alive(ctx, pid) }

// Signal implements OsHost.
func (u unknownFingerprint) Signal(pid int, request host.Request) error {
	return u.inner.Signal(pid, request)
}

// DescendsFrom implements OsHost.
func (u unknownFingerprint) DescendsFrom(ancestor, pid int) bool {
	return u.inner.DescendsFrom(ancestor, pid)
}

// GroupExists implements OsHost.
func (u unknownFingerprint) GroupExists(pid int) bool { return u.inner.GroupExists(pid) }

// SignalGroup implements OsHost.
func (u unknownFingerprint) SignalGroup(pid int, request host.Request) error {
	return u.inner.SignalGroup(pid, request)
}

// KillGroup implements OsHost.
func (u unknownFingerprint) KillGroup(pid int) error { return u.inner.KillGroup(pid) }

// fixture is one service under test plus the machine it runs against.
type fixture struct {
	*Service
	host   *fakeHost
	root   string
	repo   string
	state  string
	out    *syncBuffer
	errOut *syncBuffer
}

// newFixture builds a service against a fictional machine and a real temporary
// directory tree. It never touches the network, the process table, or the
// operator's own state directory.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	// Even a mistake cannot reach the operator's home from here: os.UserHomeDir
	// answers with a throwaway directory for the duration of the test. It reads
	// $HOME on Unix and %USERPROFILE% on Windows, so both are redirected — with
	// only HOME set, the resolver searched the runner's real profile on Windows
	// and every start, build and update test failed with "Node not found".
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	repoDir := filepath.Join(root, "repo")
	stateDir := filepath.Join(root, "state")
	logPath := filepath.Join(stateDir, "dsh-web.log")

	makeCheckout(t, repoDir)

	signature := nodeSignature(t, root)

	settings := config.Default(root)
	settings.RepoDir = repoDir
	settings.StateDir = stateDir
	settings.ConfigPath = filepath.Join(stateDir, "config.json")
	settings.LogPath = logPath
	// A port the operating system just handed back, so the fixture never
	// collides with whatever the developer has running.
	settings.Port = reserveFreePort(t)
	settings.StartTimeout = 2 * time.Second
	settings.StopTimeout = 2 * time.Second

	// The fixture owns the environment completely: no node manager of the
	// operator's and an empty PATH. An empty PATH is deliberate — it makes any
	// accidental reliance on a tool installed on the machine fail loudly here
	// instead of passing on a developer's laptop and failing in CI.
	//
	// The state directory and the port are answered rather than left out so that
	// a test can resolve settings the way a command line does (see fixture.run):
	// resolving them from the environment is what keeps every later invocation of
	// a sequence on the same state directory and port as the first one. HOME is
	// redirected above, so the built-in checkout guess is <root>/deepseek-harness.
	envLookup := func(key string) string {
		switch key {
		case paths.EnvHarnessHome:
			return root
		case paths.EnvStateDir:
			return stateDir
		case paths.EnvPort:
			return strconv.Itoa(settings.Port)
		default:
			return ""
		}
	}

	h := newFakeHost()
	h.nodeExecPath = signature
	out := &syncBuffer{}
	errOut := &syncBuffer{}
	svc := &Service{
		Settings: settings,
		Exec:     h,
		Host:     h,
		Repo: repo.Repo{
			Dir:                  repoDir,
			Ex:                   h,
			ManifestRel:          config.ServerManifestRel,
			WorkspaceManifestRel: config.WorkspaceManifestRel,
			BuildRecordRel:       buildRecordRel,
		},
		Node: &nodejs.Resolver{
			Output: run.NewCollector(h),
			LookPath: func(name string) (string, error) {
				if name == "node" {
					return signature, nil
				}
				return "/fake/bin/" + name, nil
			},
			Glob: filepath.Glob,
			Stat: os.Stat,
		},
		Log:    logfile.New(logPath, settings.LogRotateBytes),
		Record: state.Store{Path: settings.StateFile()},
		Out:    out,
		Err:    errOut,
		Getenv: envLookup,
		LookPath: func(name string) (string, error) {
			return "/fake/bin/" + name, nil
		},
		Dial: func(context.Context, int) bool { return h.ready },
		Spawn: func(path string, args []string, dir string, env []string, _ *os.File) (int, func(context.Context) bool, error) {
			pid, err := h.attemptSpawn(spawnCall{path: path, args: args, dir: dir, env: env})
			if err != nil {
				return 0, nil, err
			}
			return pid, func(context.Context) bool { return !h.isAlive(pid) }, nil
		},
		sleep: func(ctx context.Context, d time.Duration) error {
			// Real waiting is pointless against a fictional machine.
			timer := time.NewTimer(minDuration(d, 2*time.Millisecond))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		poll:  time.Millisecond,
		grace: 20 * time.Millisecond,
	}
	return &fixture{Service: svc, host: h, root: root, repo: repoDir, state: stateDir, out: out, errOut: errOut}
}

// attemptSpawn records a launch request in the command log and then answers it.
//
// The log entry is written before the attempt is answered on purpose: a spawn
// that failed must still be visible to wantNoSpawn, or the assertion would be
// blind to exactly the case it exists for (a start that tried to launch a second
// server and was refused by the operating system).
func (h *fakeHost) attemptSpawn(call spawnCall) (int, error) {
	h.mu.Lock()
	h.spawns = append(h.spawns, call)
	index := len(h.commands)
	h.commands = append(h.commands, spawnLogLine(call, 0))
	h.mu.Unlock()

	pid, err := h.spawn(portFromArgs(call.args))
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	h.commands[index] = spawnLogLine(call, pid)
	h.mu.Unlock()
	return pid, nil
}

// portFromArgs reads the port a launch was asked to serve.
//
// The fixture models the port argument rather than ignoring it because a
// multi-instance fixture has to know which server a child became: a restart of one
// instance observes its own port, and a child recorded against the wrong one would
// make that observation describe a server that does not exist.
func portFromArgs(args []string) int {
	for index, arg := range args {
		if arg == "--port" && index+1 < len(args) {
			if port, err := strconv.Atoi(args[index+1]); err == nil {
				return port
			}
		}
	}
	return 0
}

// spawnLogLine renders one spawn attempt for the command log.
func spawnLogLine(call spawnCall, pid int) string {
	return fmt.Sprintf("spawn pid=%d args=%s", pid, strings.Join(call.args, " "))
}

// TestFakeHostNeverReusesAPid pins the double's own identity rule: two processes
// on this fictional machine never share a pid.
//
// The listener used to be "wrapper plus one", a pid the counter later handed to
// somebody else. A port still mapped to that pid then reported a live process
// that had nothing to do with it, and a restart that started the configured
// instance first failed against a stopped port that looked occupied — a defect
// in the double, reported by a test that is about the product.
func TestFakeHostNeverReusesAPid(t *testing.T) {
	h := newFakeHost()
	h.spontaneouslyServed = true
	seen := map[int]string{}
	for round := 0; round < 3; round++ {
		wrapper, err := h.spawn(4000 + round)
		if err != nil {
			t.Fatalf("spawn %d: %v", round, err)
		}
		for _, process := range []struct {
			role string
			pid  int
		}{{"wrapper", wrapper}, {"listener", h.servedByListener}} {
			if where, ok := seen[process.pid]; ok {
				t.Fatalf("pid %d was handed out twice: %s and %s", process.pid, where, process.role)
			}
			seen[process.pid] = process.role
		}
	}
}

// spawnCalls returns a copy of the spawn log.
func (h *fakeHost) spawnCalls() []spawnCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]spawnCall(nil), h.spawns...)
}

// nextUnusedPID hands out one pid and remembers it, so no two processes on this
// fictional machine share an identity. It is called with the mutex held.
func (h *fakeHost) nextUnusedPID() (int, error) {
	if h.handedOut == nil {
		h.handedOut = map[int]bool{}
	}
	pid := h.nextPID
	h.nextPID++
	if h.handedOut[pid] {
		return 0, fmt.Errorf("fixture 把 pid %d 分配给了两个进程", pid)
	}
	h.handedOut[pid] = true
	return pid, nil
}

// spawn hands out the next pid and applies the child's scripted fate.
//
// Every pid comes from the counter, including the listener's: the listener used
// to be "wrapper plus one", which is a pid the counter later handed to somebody
// else. A port whose entry in listenersByPort still named that recycled pid then
// reported a live listener that had nothing to do with it, and a restart of the
// *other* instance first put a stopped port back in that state — Start refused,
// correctly, to take a port it could not account for.
func (h *fakeHost) spawn(port int) (int, error) {
	h.mu.Lock()
	if h.spawnErr != nil {
		err := h.spawnErr
		h.mu.Unlock()
		return 0, err
	}
	pid, err := h.nextUnusedPID()
	if err != nil {
		h.mu.Unlock()
		return 0, err
	}
	entry := &fakeProcess{command: "pnpm --dir repo dsh web", startedAt: fixtureStartTime, alive: true}
	h.processes[pid] = entry
	serve := h.spontaneouslyServed
	die := h.diesImmediately
	if die {
		entry.alive = false
	}
	if serve && !die {
		if h.opaqueServed {
			// The server binds the port but the platform cannot attribute it:
			// Listening reports a listener with no pid.
			h.listener = -1
			h.servedByWrapper = pid
			h.mu.Unlock()
			return pid, nil
		}
		// The listener is a child of the wrapper, in the wrapper's group: this is
		// the shape a package script produces.
		listener, err := h.nextUnusedPID()
		if err != nil {
			h.mu.Unlock()
			return 0, err
		}
		h.processes[listener] = &fakeProcess{
			command:   "node apps/cli/src/bin.ts web",
			startedAt: fixtureStartTime,
			alive:     true,
			group:     pid,
		}
		if h.listenerIgnoresGrace {
			h.processes[listener].survivesGraceful = true
		}
		h.listener = listener
		// A spawned child takes whatever port the launch asked for, which on a
		// multi-instance fixture is not the fixture's own. Recording it per port
		// is what lets a restart of another instance observe its own listener
		// instead of the fixture's.
		if h.listenersByPort == nil {
			h.listenersByPort = map[int]int{}
		}
		h.listenersByPort[port] = listener
		h.servedByListener = listener
		h.servedByWrapper = pid
	}
	h.mu.Unlock()
	return pid, nil
}

// makeCheckout turns a directory into a checkout every check accepts: the
// markers, the installed dependencies, the build record, and a real git
// repository. It is how a fixture describes a second checkout — the shape a
// machine has when the configured checkout and the running one differ.
func makeCheckout(t *testing.T, dir string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, "package.json"), "{}")
	writeFile(t, filepath.Join(dir, "pnpm-workspace.yaml"), "packages:\n  - packages/*\n")
	writeFile(t, filepath.Join(dir, "node_modules", ".keep"), "")
	writeFile(t, filepath.Join(dir, filepath.FromSlash(buildRecordRel)), "{}")
	initGitRepo(t, dir)
	return dir
}

// initGitRepo creates a real, minimal git repository.
//
// Some code paths (git ls-files during a prune, git pull during an update) are
// only meaningful against a real checkout, so the fixture builds one instead of
// pretending.
//
// The subprocess environment is built explicitly rather than inherited. git
// reads GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE and GIT_COMMON_DIR from the
// environment, and an exported one would redirect these commands — and every
// `git add -A` in them — into a real repository on the operator's machine. The
// global and system configuration files are disabled for the same reason: a
// user-level `core.hooksPath`, `commit.gpgsign` or `init.templateDir` would run
// the operator's own hooks and templates inside the fixture. Nothing here may
// reach outside the temporary directory the test owns.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"config", "user.email", "fixture@example.com"},
		{"config", "user.name", "fixture"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = hermeticGitEnv(dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable for the fixture (%v): %s", err, out)
		}
	}
}

// hermeticGitEnv is the environment a fixture git runs with: the fixture's own
// directory as the only repository, no global or system configuration, and no
// inherited variable that could point git somewhere else.
//
// The environment is built from an allow-list rather than by unsetting the
// dangerous names. Some of them (GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR) treat
// an *empty* value as a malformed path and fail the command, so "set it to
// nothing" is not the same as "it is not set"; omitting the name entirely is the
// only form that means "look in the working directory".
func hermeticGitEnv(dir string) []string {
	var env []string
	for _, name := range []string{"PATH", "SYSTEMROOT", "TEMP", "TMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return append(env,
		// HOME is the checkout itself, so git cannot read a configuration
		// file the operator keeps in their home directory.
		"HOME="+dir,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
	)
}

// reserveFreePort returns a TCP port that was free a moment ago.
func reserveFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return port
}

// nodeSignature creates a fake node installation and returns its binary path.
func nodeSignature(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, ".nvm", "versions", "node", "v"+config.TestedNodeVersion, "bin", fixtureNodeName())
	writeFile(t, path, "#!/bin/sh\necho v"+config.TestedNodeVersion+"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake node: %v", err)
	}
	return path
}

// lockHolderThroughLock is the pid a status report can name for a lock this
// process holds.
//
// Unix locks are advisory, so the record inside the file stays readable and
// names the holder. Windows byte-range locks are mandatory: the locked region
// cannot be read through another handle, so the honest answer is "unknown",
// which is what zero means in the status field.
func lockHolderThroughLock(pid int) int {
	if runtime.GOOS == "windows" {
		return 0
	}
	return pid
}

// fixtureNodeName is the file name the platform resolves a Node runtime by.
//
// Windows looks for node.exe, so a fixture that writes "node" leaves every
// start, build and update test failing there with "Node not found": the same
// code passes on Unix and fails on Windows for a reason that has nothing to do
// with what the test measures.
func fixtureNodeName() string {
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
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

// emit writes canned output to a command's standard output.
func emit(w interface{ Write([]byte) (int, error) }, text string) error {
	if w == nil {
		return nil
	}
	_, err := w.Write([]byte(text))
	return err
}

// hasArgument reports whether cmd carries every given argument.
func hasArgument(cmd run.Command, args ...string) bool {
	for _, want := range args {
		found := false
		for _, arg := range cmd.Args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// wantNoSpawn fails the test when the service started a process.
func (f *fixture) wantNoSpawn(t *testing.T) {
	t.Helper()
	for _, command := range f.host.commandsRun() {
		if strings.HasPrefix(command, "spawn ") {
			t.Fatalf("a process was started: %v", f.host.commandsRun())
		}
	}
}

// wantNoSignals fails the test when the service delivered a signal.
func (f *fixture) wantNoSignals(t *testing.T) {
	t.Helper()
	if signals := f.host.signalsSent(); len(signals) != 0 {
		t.Fatalf("a signal was delivered: %v", signals)
	}
}

// wantSpawn asserts that exactly one process was spawned and returns what the
// launcher was asked for. The arguments are asserted separately because a wrong
// --no-open, --port or working directory would otherwise pass every test: the
// fictional launcher never reads them.
func (f *fixture) wantSpawn(t *testing.T) spawnCall {
	t.Helper()
	calls := f.host.spawnCalls()
	if len(calls) != 1 {
		t.Fatalf("spawn calls = %v, want exactly one", calls)
	}
	return calls[0]
}

// wantEnvPathPrefix asserts that an environment carries dir at the front of
// PATH, which is how the spawned server finds the resolved Node runtime.
func wantEnvPathPrefix(t *testing.T, env []string, dir string) {
	t.Helper()
	want := "PATH=" + dir + string(os.PathListSeparator)
	for _, entry := range env {
		if strings.HasPrefix(entry, want) {
			return
		}
	}
	t.Fatalf("environment PATH does not start with %q: %v", want, env)
}

// wantSignals asserts the exact signal sequence that was delivered.
func (f *fixture) wantSignals(t *testing.T, want []fakeSignal) {
	t.Helper()
	got := f.host.signalsSent()
	if len(got) != len(want) {
		t.Fatalf("signals = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("signals = %v, want %v", got, want)
		}
	}
}

// stateRecord builds a record for the fixture's port with a matching
// fingerprint, which is the shape a successful start writes.
func stateRecord(f *fixture, pid int) state.Record {
	return state.Record{PID: pid, StartedAt: fixtureStartTime, Port: f.Settings.Port, Phase: state.PhaseRunning}
}

// startServer makes the fixture look like a server this service started: a
// matching record plus a listener.
func (f *fixture) startServer(t *testing.T, pid int, url string) state.Record {
	t.Helper()
	f.host.serving(pid, "pnpm --dir repo dsh web")
	record := state.Record{PID: pid, StartedAt: 1_700_000_000, Port: f.Settings.Port, Phase: state.PhaseRunning, URL: url}
	if err := f.Record.Save(record); err != nil {
		t.Fatalf("save record: %v", err)
	}
	return record
}

// sectionTitles lists the section markers found in a log file.
func sectionTitles(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	var titles []string
	for _, line := range strings.Split(string(data), "\n") {
		if title, ok := logfile.ParseSection(line); ok {
			titles = append(titles, title)
		}
	}
	sort.Strings(titles)
	return titles
}

// stateRecord reads the runtime record, or reports that there is none.
func (f *fixture) stateRecord(t *testing.T) (state.Record, bool) {
	t.Helper()
	record, ok, err := f.Record.Load()
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	return record, ok
}

// describeCommands renders the command log for failure messages.
func (f *fixture) describeCommands() string {
	return fmt.Sprint(f.host.commandsRun())
}

// checkedRecordExists fails the test when the runtime record is not on disk.
func (f *fixture) wantRecordOnDisk(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(f.Settings.StateFile()); err != nil {
		t.Fatalf("the runtime record must still exist after the call: %v", err)
	}
}

// wantNoRecordOnDisk fails the test when a runtime record survives.
func (f *fixture) wantNoRecordOnDisk(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(f.Settings.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("the runtime record must be gone, got err=%v", err)
	}
}

// wantNoPull fails the test when the checkout was updated.
func (f *fixture) wantNoPull(t *testing.T) {
	t.Helper()
	for _, command := range f.host.commandsRun() {
		if strings.Contains(command, "pull") {
			t.Fatalf("the checkout was updated: %v", f.host.commandsRun())
		}
	}
}

// seedNodeInstallation replaces the fixture's Node runtime with an installation
// of the given version, so a test can drive the resolver's pinned-release and
// minimum-version branches without touching the operator's machine. The settings
// pin the same version, which is what makes the branch a test is after the one
// that runs: leaving the default pinned would exercise "the requested release is
// not installed" instead.
func (f *fixture) seedNodeInstallation(t *testing.T, version string) {
	t.Helper()
	f.Settings.NodeVersion = version
	f.Settings.ConfiguredNodeVersion = version
	f.Settings.Sources.NodeVersion = "file"
	f.installNodeTree(t, version)
}

// installNodeTree puts a release in the fixture's version-manager tree and makes
// PATH serve it, without touching the settings: it is the on-disk half of a
// runtime, which a test combines with whichever settings state it is about.
func (f *fixture) installNodeTree(t *testing.T, version string) string {
	t.Helper()
	f.host.nodeVersion = version
	if err := os.RemoveAll(filepath.Join(f.root, ".nvm")); err != nil {
		t.Fatalf("remove the previous node installation: %v", err)
	}
	path := filepath.Join(f.root, ".nvm", "versions", "node", "v"+version, "bin", fixtureNodeName())
	writeFile(t, path, "#!/bin/sh\necho v"+version+"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake node: %v", err)
	}
	f.host.nodeExecPath = path
	f.Node.LookPath = func(name string) (string, error) {
		if name == "node" {
			return path, nil
		}
		return "/fake/bin/" + name, nil
	}
	return path
}

// servePATHNode makes the machine's PATH node report version. The settings are
// left alone, which is the shape of an installation that has not determined a
// release yet — and of one whose document names a release that PATH happens to
// serve.
func (f *fixture) servePATHNode(t *testing.T, version string) {
	t.Helper()
	f.host.nodeVersion = version
}

// pinNode models an installation whose settings document already names a
// release: the file holds it, and the settings the service runs with carry it as
// both the effective value and the configured one.
func (f *fixture) pinNode(t *testing.T, version string) {
	t.Helper()
	f.Settings.NodeVersion = version
	f.Settings.ConfiguredNodeVersion = version
	f.Settings.Sources.NodeVersion = "file"
	writeFile(t, f.Settings.ConfigPath, `{"nodeVersion": "`+version+`"}`+"\n")
}

// serveNodeThroughShim makes PATH serve a forwarding entry — the shape asdf,
// mise and volta install — whose real interpreter lives elsewhere. It returns
// both paths so a test can assert which one is used.
func (f *fixture) serveNodeThroughShim(t *testing.T, version string) (string, string) {
	t.Helper()
	real := filepath.Join(f.root, "installs", "node", version, "bin", fixtureNodeName())
	writeFile(t, real, "#!/bin/sh\necho v"+version+"\n")
	shim := filepath.Join(f.root, "shims", fixtureNodeName())
	writeFile(t, shim, "#!/bin/sh\nexec "+real+" \"$@\"\n")
	f.host.nodeVersion = version
	f.host.nodeExecPath = real
	f.Node.LookPath = func(name string) (string, error) {
		if name == "node" {
			return shim, nil
		}
		return "/fake/bin/" + name, nil
	}
	return shim, real
}

// realNodeShim writes a node that reports the release dshctl is verified against
// and forwards everything else to the real interpreter, and returns its path.
//
// Tests that run real processes need a real node, but they must not depend on
// the machine's own node being new enough: an ubuntu runner that ships Node 22
// would otherwise fail a test about process handling because of the version
// floor, which is a fact about the runner rather than about the code. The
// reported release is the fixture's, exactly as it is everywhere else in this
// package; the interpreter that runs the server is the real one.
func realNodeShim(t *testing.T, dir string) string {
	t.Helper()
	real, err := run.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	shim := filepath.Join(dir, fixtureNodeName())
	writeFile(t, shim, "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"  -v) echo v"+config.TestedNodeVersion+"; exit 0;;\n"+
		"  -p) echo \""+shim+"\"; exit 0;;\n"+
		"esac\n"+
		"exec \""+real+"\" \"$@\"\n")
	if err := os.Chmod(shim, 0o755); err != nil {
		t.Fatalf("chmod the node shim: %v", err)
	}
	return shim
}

// servedNodePath reports the binary the fixture's PATH serves, whatever the
// settings say.
//
// The doctor baseline uses it rather than the resolver: the cases about a
// runtime that cannot be resolved replace the Node row, and a baseline that
// insisted on resolving would make exactly those cases untestable.
func (f *fixture) servedNodePath(t *testing.T) string {
	t.Helper()
	if f.host.nodeExecPath == "" {
		t.Fatal("the fixture serves no node binary")
	}
	return f.host.nodeExecPath
}

// run models one command of a sequence.
//
// Every dshctl invocation resolves its settings from scratch — flags over the
// environment over the settings document over the built-in defaults — and then
// builds a service from them. A test that keeps the settings it constructed by
// hand can therefore never express what the *next* command sees, which is
// exactly where a configuration that contradicts reality hides: the checkout the
// document names is what a second invocation uses, whether or not it is the
// checkout the first one ran from.
//
// Overrides are layered the way the command line layers them, and the fixture's
// own state directory and port come from the environment (see newFixture), so a
// sequence of calls shares one document, one record and one port.
func (f *fixture) run(t *testing.T, overrides config.Overrides) config.Settings {
	t.Helper()
	loaded, err := config.Load(f.Getenv, overrides)
	if err != nil {
		t.Fatalf("resolve the settings: %v", err)
	}
	// The wait budgets are pacing for the fictional machine rather than policy:
	// a failure path that waits out the built-in ninety seconds would turn one
	// row of a sequence into a minute of test time.
	loaded.StartTimeout = f.Settings.StartTimeout
	loaded.StopTimeout = f.Settings.StopTimeout
	f.Settings = loaded
	f.rebind()
	// Production wires the checkout from the settings into the repository
	// helper; a test that left the two apart would let a command act on one
	// directory while describing another.
	f.Repo.Dir = loaded.RepoDir
	return loaded
}

// rebind re-derives the stores that belong to the settings.
//
// A record is one file per port and a fixture's settings can move to another
// one, so a store pinned at construction would make the test read a file no
// command writes: the port-keyed name is the settings' business (see
// config.Settings.StateFile), and every caller that replaces the settings has to
// ask them again for it. `New` wires the same two values, which is what keeps a
// fixture and the service it stands for describing one machine.
func (f *fixture) rebind() {
	f.Record = state.Store{Path: f.Settings.StateFile()}
	f.Log = logfile.New(f.Settings.LogPath, f.Settings.LogRotateBytes)
}

// guess is the built-in checkout this machine's home implies.
func (f *fixture) guess() string { return config.DefaultRepoDir(f.root) }

// wantDocumentCheckout asserts the checkout the settings document names, and
// reports that it names none when want is empty.
func (f *fixture) wantDocumentCheckout(t *testing.T, want string) {
	t.Helper()
	got, present := f.configDocument(t)["repoDir"]
	if want == "" {
		if present {
			t.Fatalf("document repoDir = %v, want none", got)
		}
		return
	}
	if got != want {
		t.Fatalf("document repoDir = %v, want %q", got, want)
	}
}

// wantRecordedCheckout asserts the checkout the settings document names, and
// that it names none when want is empty.
func (f *fixture) wantRecordedCheckout(t *testing.T, want string) {
	t.Helper()
	got, present := f.configDocument(t)["repoDir"]
	if want == "" {
		if present {
			t.Fatalf("document repoDir = %v, want none", got)
		}
		return
	}
	if got != want {
		t.Fatalf("document repoDir = %v, want %q", got, want)
	}
}

// wantNoFailingCheckoutRow fails the test when a diagnosis reports a blocking row
// about the checkout: the shape a machine takes when the configuration points at
// a directory other than the one being served.
func wantNoFailingCheckoutRow(t *testing.T, checks []Check) {
	t.Helper()
	for _, check := range checks {
		switch check.Name {
		case "仓库目录", "依赖", "构建产物", "仓库版本", "服务仓库":
			if check.Status == CheckFail {
				t.Fatalf("doctor reported a blocking row about the checkout: %+v", check)
			}
		}
	}
}

// configDocument decodes the settings document the fixture wrote.
func (f *fixture) configDocument(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(f.Settings.ConfigPath)
	if err != nil {
		t.Fatalf("read the settings document: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode the settings document: %v\n%s", err, data)
	}
	return document
}

// wantRecordedNodeVersion asserts the release the settings document names.
func (f *fixture) wantRecordedNodeVersion(t *testing.T, want string) {
	t.Helper()
	if got := f.configDocument(t)["nodeVersion"]; got != want {
		t.Fatalf("settings document nodeVersion = %v, want %q", got, want)
	}
}

// wantNoRecordedNodeVersion asserts the settings document names no release.
func (f *fixture) wantNoRecordedNodeVersion(t *testing.T) {
	t.Helper()
	if got, present := f.configDocument(t)["nodeVersion"]; present {
		t.Fatalf("settings document nodeVersion = %v, want none", got)
	}
}

// wantNodeBinDir asserts the directory the child is handed in front of PATH,
// which is how the runtime dshctl resolved reaches the server.
func (f *fixture) wantNodeBinDir(t *testing.T, want string) {
	t.Helper()
	call := f.wantSpawn(t)
	wantEnvPathPrefix(t, call.env, want)
}

// resolvedNode resolves the runtime the way the service does, so a test can
// assert on the bin directory without repeating how the fixture lays the tree
// out.
func (f *fixture) resolvedNode(t *testing.T) nodejs.Installation {
	t.Helper()
	installation, err := f.Node.Resolve(context.Background(), nodejs.Preferences{
		Version: f.Settings.NodeVersion,
		Home:    f.root,
	})
	if err != nil {
		t.Fatalf("resolve the fixture's node: %v", err)
	}
	return installation
}

// seedCorruptRecord writes bytes that are not a runtime record to the record
// path, which is the shape a half-written or hand-edited file has.
func (f *fixture) seedCorruptRecord(t *testing.T, content string) {
	t.Helper()
	writeFile(t, f.Settings.StateFile(), content)
}

// serviceWithout is a host that hides one process from Inspect while every other
// question is answered by the fixture's own machine.
//
// It models the race a stop cannot close by checking: the process was alive when
// the command decided to signal it and is gone by the time the signal would be
// delivered.
type serviceWithout struct {
	inner OsHost
	gone  int
}

// Listening implements OsHost.
func (s serviceWithout) Listening(ctx context.Context, port int) (host.PortResult, error) {
	return s.inner.Listening(ctx, port)
}

// Inspect implements OsHost, reporting the hidden pid as already gone.
func (s serviceWithout) Inspect(ctx context.Context, pid int) host.Facts {
	if pid == s.gone {
		return host.Facts{PID: pid}
	}
	return s.inner.Inspect(ctx, pid)
}

// Alive implements OsHost.
func (s serviceWithout) Alive(ctx context.Context, pid int) bool {
	return s.Inspect(ctx, pid).Alive
}

// Signal implements OsHost.
func (s serviceWithout) Signal(pid int, request host.Request) error {
	return s.inner.Signal(pid, request)
}

// DescendsFrom implements OsHost.
func (s serviceWithout) DescendsFrom(ancestor, pid int) bool {
	return s.inner.DescendsFrom(ancestor, pid)
}

// GroupExists implements OsHost.
func (s serviceWithout) GroupExists(pid int) bool { return s.inner.GroupExists(pid) }

// SignalGroup implements OsHost.
func (s serviceWithout) SignalGroup(pid int, request host.Request) error {
	return s.inner.SignalGroup(pid, request)
}

// KillGroup implements OsHost.
func (s serviceWithout) KillGroup(pid int) error { return s.inner.KillGroup(pid) }
