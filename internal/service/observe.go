package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/lock"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/state"
)

// Service states reported to callers.
const (
	// StateRunning means the server dshctl started owns the port.
	StateRunning = "running"
	// StateStarting means dshctl's process lives but the port is not answering.
	StateStarting = "starting"
	// StateStopped means nothing is listening and no record survives.
	StateStopped = "stopped"
	// StateForeign means the port is owned by a process that is positively not
	// the server this dshctl manages: another program, or a DeepSeek Harness
	// server of a different checkout.
	StateForeign = "port-foreign"
	// StateOrphan means something owns the port that dshctl cannot classify,
	// because the runtime record is missing or contradicts the live process.
	// An orphan is reported and never signalled directly; a mutating command
	// adopts it first when group evidence proves it is this dshctl's own
	// server left behind by an interrupted start.
	StateOrphan = "port-unmanaged"
)

// Timing constants of the observation loop. They are behaviour, not policy, so
// they stay in code rather than in the config file.
const (
	// dialTimeout bounds one readiness probe.
	dialTimeout = 300 * time.Millisecond
	// pollInterval is how often a wait re-checks the port.
	pollInterval = 400 * time.Millisecond
	// fingerprintTolerance is how far the recorded and observed start times may
	// differ. The elapsed time reported by ps has one-second granularity, and
	// the record is written a moment after the process starts.
	fingerprintTolerance = 5 * time.Second
	// terminateGrace is how long a stop waits after a graceful request before it
	// forces the process down.
	terminateGrace = 2 * time.Second
	// fingerprintTimeout bounds how long a start waits for the process start
	// time, which is the value every later ownership decision is checked against.
	fingerprintTimeout = 10 * time.Second
)

// Status is the observed state of the managed service.
type Status struct {
	// State is one of the State* constants.
	State string `json:"state"`
	// URL is the configured loopback address, without a token.
	URL string `json:"url"`
	// Port is the configured loopback port.
	Port int `json:"port"`
	// Ready reports whether the port accepted a connection.
	Ready bool `json:"ready"`
	// ListenerPID is the process holding the port, or 0.
	ListenerPID int `json:"listenerPid,omitempty"`
	// ListenerCommand identifies the listener when the platform can describe it.
	ListenerCommand string `json:"listenerCommand,omitempty"`
	// RecordedPID is the pid in the runtime record, or 0.
	RecordedPID int `json:"recordedPid,omitempty"`
	// RecordedPhase is the phase in the runtime record, or empty.
	RecordedPhase string `json:"recordedPhase,omitempty"`
	// RecordLive reports that the record names a process that is alive and
	// whose start time still matches: a server this dshctl started and can
	// still end, whether or not it holds the port.
	RecordLive bool `json:"recordLive"`
	// RecordStale reports that the record exists but names a pid that is gone or
	// has been recycled, so it describes nothing. It is never set for a record
	// whose process is still alive: that record is the only handle on a running
	// server and is kept until the server is ended.
	RecordStale bool `json:"recordStale"`
	// StaleRecord is the record itself when RecordStale is set, so a caller can
	// say which pid and phase the stale record named.
	StaleRecord *state.Record `json:"-"`
	// Survivor reports that a server of ours holds the port although the record
	// does not name it: the shape an interrupted start leaves behind, when
	// dshctl was killed between writing the wrapper record and the port
	// answering. A mutating command adopts it and manages it again.
	Survivor bool `json:"survivorService,omitempty"`
	// URLFromRecord is the token-carrying address the server announced.
	URLFromRecord string `json:"urlWithToken,omitempty"`
	// RepoDir is the managed checkout.
	RepoDir string `json:"repoDir"`
	// RepoReady reports whether the checkout is a DeepSeek Harness checkout.
	RepoReady bool `json:"repoReady"`
	// BuildReady reports whether the checkout is installed and built.
	BuildReady bool `json:"buildReady"`
	// LogPath is where server, build and update output accumulates.
	LogPath string `json:"logPath"`
	// LockHeld reports whether another dshctl operation holds the lock.
	LockHeld bool `json:"lockHeld"`
	// LockHolder is the pid holding the lock, or 0.
	LockHolder int `json:"lockHolder,omitempty"`
	// LockUnreadable reports that the lock exists but could not be inspected.
	LockUnreadable bool `json:"lockUnreadable,omitempty"`
}

// Owning reports whether the state describes a server this dshctl owns.
func (s Status) Owning() bool {
	return s.State == StateRunning || s.State == StateStarting
}

// ServeExitCode maps the state onto the process exit code, so `dshctl status`
// is usable in shell conditions.
//
// Running and starting both report success: the service exists and is being
// managed. Everything else reports "not running".
func (s Status) ServeExitCode() int {
	if s.Owning() {
		return exitcode.OK
	}
	return exitcode.NotRunning
}

// readiness is one observation of the port.
//
// "Something listens" and "here is whose it is" are separate facts, and they
// have to stay separate: a probe can report a listener without being able to
// name its owner, and calling that "nothing listens" would report a running
// service as stopped and delete the record that could have stopped it.
type readiness struct {
	// listening reports whether anything holds the port.
	listening bool
	// ready reports whether the port accepted a connection.
	ready bool
	// pid is the owner, or 0 when the platform could not name it.
	pid   int
	facts host.Facts
}

// observed is the full picture a lifecycle decision is made from.
type observed struct {
	status    Status
	record    state.Record
	hasRecord bool
	// corrupt reports that a record file exists but cannot be understood. It
	// describes nothing, yet the bytes are there, and a mutating command has to
	// retire them: doctor promises the operator that the next start or stop
	// rebuilds such a record.
	corrupt bool
}

// Status observes the service without changing anything at all.
//
// It takes no lock and creates no file. An operation lock would have to live in
// the state directory, and creating that directory is a write; a reporting
// command that quietly provisions state is not reporting, it is mutating. The
// record is read atomically, so a status running beside a start sees either the
// old record or the new one, never a partial document.
//
// The port is the authority for "is something listening"; the runtime record is
// the authority for "is it mine". When the two disagree the result is
// StateOrphan or StateForeign, never a silent "stopped", and a record that
// describes nothing is reported as stale for the operator to see. The next
// mutating command retires it.
func (s *Service) Status(ctx context.Context) (Status, error) {
	result, err := s.observe(ctx)
	if err != nil {
		return Status{}, err
	}
	return result.status, nil
}

// observe reads the current state. The caller holds the operation lock, which is
// what makes the check-then-act sequences in this package safe.
func (s *Service) observe(ctx context.Context) (observed, error) {
	status := s.baseStatus()
	record, hasRecord, err := s.Record.Load()
	corrupt := false
	if err != nil {
		if errors.Is(err, state.ErrCorrupt) {
			// A record that cannot be read describes nothing. Reporting commands
			// report that; the next mutating operation removes it.
			status.RecordStale = true
			corrupt = true
		} else {
			return observed{}, exitcode.Wrap(exitcode.Failure, err)
		}
	}
	if hasRecord {
		status.RecordedPID = record.PID
		status.RecordedPhase = string(record.Phase)
		status.URLFromRecord = record.URL
	}

	// The record's own liveness is one question, asked at most once: on some
	// platforms reading a start time runs a process listing, and several
	// branches below need the same answer.
	liveKnown := false
	isRecordLive := func() bool {
		if !liveKnown {
			liveKnown = true
			if hasRecord {
				status.RecordLive = s.RecordMatches(ctx, record, record.PID)
			}
		}
		return status.RecordLive
	}

	observation, err := s.probeRequired(ctx)
	if err != nil {
		return observed{}, err
	}
	status.Ready = observation.ready
	status.ListenerPID = observation.pid
	if observation.pid > 0 {
		status.ListenerCommand = describeFacts(observation.facts)
	}

	switch {
	case !observation.listening:
		// Nothing holds the port. The record keeps its meaning when its process
		// is alive: it is the handle on a server dshctl started, and `stop`
		// ends it rather than forgetting it.
		status.State = StateStopped
	case hasRecord && observation.pid == record.PID && isRecordLive():
		// The recorded process owns the port.
		status.State = StateRunning
		if !observation.ready {
			status.State = StateStarting
		}
	case hasRecord && observation.pid == record.PID:
		// The pid is the one we recorded but the start time does not match, so
		// the operating system recycled it: this is a stranger.
		status.State = StateForeign
	case hasRecord && observation.pid > 0 && record.SpawnedPID > 0 &&
		s.descendsFromSpawned(record.SpawnedPID, observation.pid):
		// The listener is not the recorded pid, but it descends from the
		// process the recorded start spawned: the shape left behind when
		// dshctl was killed between writing the wrapper record and the port
		// answering. It is this dshctl's own server, so it is never reported
		// as foreign, and the record is kept so a mutating command can adopt
		// the survivor and manage it again.
		status.State = StateOrphan
		status.Survivor = true
	case hasRecord && !s.Host.Alive(ctx, record.PID):
		// Our process is gone and something else took the port.
		status.State = StateForeign
	case isForeignServer(observation.facts, s.Settings.RepoDir):
		// Positively not the server this dshctl manages.
		status.State = StateForeign
	case observation.pid == 0:
		// Something holds the port and the platform will not say what. That is
		// as unmanageable as a stranger, and it is certainly not "stopped".
		status.State = StateOrphan
	case hasRecord:
		// Our process is alive but something else holds the port.
		status.State = StateOrphan
	default:
		// Something listens and nothing here says it is ours.
		status.State = StateOrphan
	}

	// What the record is worth is one fact, decided once and by the same
	// predicates every command uses: a record is stale when it describes neither
	// a live, fingerprint-matching process nor a tree that still serves its
	// port. Every shape above ends up with the right answer here — including the
	// one no branch names explicitly, a recorded pid recycled to a stranger while
	// yet another process holds the port.
	isRecordLive()
	if hasRecord && !status.RecordLive && !status.Survivor {
		status.RecordStale = true
		stale := record
		status.StaleRecord = &stale
	}
	return observed{status: status, record: record, hasRecord: hasRecord, corrupt: corrupt}, nil
}

// probeRequired observes the port, turning a failed probe into a Preflight error
// so that "I could not look" is never read as "nothing is listening".
func (s *Service) probeRequired(ctx context.Context) (readiness, error) {
	result, err := s.Host.Listening(ctx, s.Settings.Port)
	if err != nil {
		if errors.Is(err, host.ErrUnsupported) {
			return readiness{}, exitcode.New(exitcode.Preflight,
				"缺少可用的端口探测工具(lsof/ss/netstat)，无法判断端口 %d 的状态\n"+
					"提示: 安装其中任意一个(例如 iproute2 或 net-tools)后重试", s.Settings.Port)
		}
		return readiness{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	observation := readiness{listening: result.Listening, pid: result.PID}
	if !result.Listening {
		return observation, nil
	}
	observation.ready = s.dials(ctx)
	if result.PID > 0 {
		observation.facts = s.Host.Inspect(ctx, result.PID)
	}
	return observation, nil
}

// RecordMatches reports whether the record still describes the live pid.
func (s *Service) RecordMatches(ctx context.Context, record state.Record, pid int) bool {
	facts := s.Host.Inspect(ctx, pid)
	if !facts.Alive {
		return false
	}
	return state.Match(record, facts.StartedAt, fingerprintTolerance)
}

// dials reports whether the loopback port accepts a connection, which is the
// strongest readiness signal available without speaking the server's protocol.
func (s *Service) dials(ctx context.Context) bool {
	dial := s.Dial
	if dial == nil {
		dial = dialPort
	}
	return dial(ctx, s.Settings.Port)
}

// dialPort connects to the loopback port once.
func dialPort(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: dialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// baseStatus fills the fields that do not need a probe.
func (s *Service) baseStatus() Status {
	status := Status{
		URL:        s.Settings.URL(),
		Port:       s.Settings.Port,
		RepoDir:    s.Settings.RepoDir,
		RepoReady:  s.Repo.IsServerCheckout(),
		BuildReady: s.Repo.BuildReady(),
		LogPath:    s.Settings.LogPath,
	}
	holder, held, err := lock.Held(s.Settings.LockFile())
	switch {
	case err != nil:
		status.LockUnreadable = true
	case held:
		status.LockHeld = true
		status.LockHolder = holder
	}
	return status
}

// isForeignServer reports whether a listener is positively not the server this
// dshctl manages.
//
// The test is evidence-based: dshctl always starts the server with the checkout
// on its command line, so a listening process whose command line names a
// DeepSeek Harness entry point without naming this checkout belongs to somebody
// else. A process the platform could not describe yields no evidence at all and
// is therefore reported as unclassified rather than as foreign.
func isForeignServer(facts host.Facts, repoDir string) bool {
	if facts.Command == "" || !mentionsDeepSeekHarness(facts.Command) {
		return false
	}
	return !strings.Contains(facts.Command, repoDir)
}

// mentionsDeepSeekHarness reports whether a command line looks like the harness.
func mentionsDeepSeekHarness(command string) bool {
	for _, marker := range []string{"deepseek-harness", "deepseek-ai/dsh", "dsh web", "dsh/lib/bin.js", "apps/cli"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

// describeFacts renders what the platform could say about a process.
func describeFacts(facts host.Facts) string {
	if facts.Command != "" {
		return facts.Command
	}
	if facts.Source != "" {
		return "pid=" + strconv.Itoa(facts.PID) + " (来源: " + facts.Source + ")"
	}
	return "未知进程"
}

// waitForListening waits until the port is served by the process this start
// spawned, or until the deadline passes, or until ctx is cancelled.
//
// The port was verified free before anything was spawned and this start holds
// the operation lock, so a listener that appears now is the one this call
// started. The listener is not required to *be* the spawned pid: the harness is
// launched through a package script, and pnpm runs those in a child process, so
// the pid that binds the port is normally a descendant.
//
// Returns:
//   - nil when the port answers.
//   - a Preflight error when something else took the port.
//   - exitcode.Failure when the wait ran out.
//   - the context error when it was cancelled.
func (s *Service) waitForListening(ctx context.Context, expectedPID int, exited func(context.Context) bool, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	// ownerlessSince is when the port first answered without a nameable owner.
	// The probe chain is not atomic — it asks a per-process probe first and a
	// whole-table probe after it — so a socket created by the server this call
	// has just spawned can be in the table and not yet in the per-process scan.
	// Deciding on that one sample fails a start for a server that is plainly its
	// own, so the state is re-examined for a moment before it is reported as
	// unverifiable. Nothing is concluded during the grace period: the port is
	// simply looked at again, and a listener that stays unnameable is refused
	// exactly as before, with the message naming what could not be verified.
	//
	// The grace is a couple of probe cycles, not a timeout: it exists to let the
	// two probes agree, and a port that is genuinely held by a process the
	// platform cannot describe is still refused long before the start deadline.
	var ownerlessSince time.Time
	const ownerlessListenGrace = time.Second
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		observation, err := s.probeRequired(ctx)
		if err != nil {
			lastErr = err
		} else if observation.pid == 0 {
			if observation.listening {
				if ownerlessSince.IsZero() {
					ownerlessSince = time.Now()
				}
				if time.Since(ownerlessSince) >= ownerlessListenGrace {
					return 0, exitcode.New(exitcode.Preflight,
						"端口 %d 已有监听，但平台探测工具未报告其归属进程，无法确认它是本次启动的服务", s.Settings.Port)
				}
			} else {
				ownerlessSince = time.Time{}
				// A child that has ended is reported at once rather than after
				// the whole start timeout. Both checks are needed: the direct
				// one is exact, and the host check covers a child that somehow
				// outlived its reaper.
				if exited != nil && exited(ctx) {
					return 0, exitcode.New(exitcode.Failure,
						"DSH Web 进程 (pid=%d) 已退出，端口 %d 始终没有就绪(详见日志)", expectedPID, s.Settings.Port)
				}
				if !s.Host.Alive(ctx, expectedPID) {
					return 0, exitcode.New(exitcode.Failure,
						"DSH Web 进程 (pid=%d) 已退出，端口 %d 始终没有就绪", expectedPID, s.Settings.Port)
				}
			}
		} else if observation.pid == expectedPID || s.descendsFromSpawned(expectedPID, observation.pid) {
			ownerlessSince = time.Time{}
			// Something holds the port and it belongs to the group this start
			// created. It is the server when it answers; until then the shared
			// deadline check and poll sleep below pace the retry — the check
			// must never be bypassed, or a server that binds but never answers
			// would spin forever instead of timing out.
			if observation.ready {
				return observation.pid, nil
			}
		} else {
			// A listener outside the spawned group is a race with another
			// process and is refused.
			return 0, exitcode.New(exitcode.Preflight,
				"端口 %d 被另一个进程占用 (pid=%d: %s)",
				s.Settings.Port, observation.pid, describeFacts(observation.facts))
		}
		if time.Now().After(deadline) {
			// A port that kept answering without a nameable owner is reported as
			// what it is, rather than as a bare timeout.
			if !ownerlessSince.IsZero() {
				return 0, exitcode.New(exitcode.Preflight,
					"端口 %d 已有监听，但平台探测工具未报告其归属进程，无法确认它是本次启动的服务", s.Settings.Port)
			}
			if lastErr != nil {
				return 0, exitcode.Wrap(exitcode.Failure, lastErr)
			}
			return 0, exitcode.New(exitcode.Failure,
				"等待端口 %d 就绪超时 (%s)", s.Settings.Port, timeout)
		}
		if err := s.sleep(ctx, s.poll); err != nil {
			return 0, err
		}
	}
}

// descendsFromSpawned reports whether a process belongs to the tree the spawned
// wrapper created.
//
// It is the evidence that a listener which is not the spawned pid is still ours:
// dshctl launches a package script, and the process that binds the port is a
// descendant of what it spawned. Unix answers through the process group the
// wrapper leads, Windows through the parent-pid chain — one question, one
// method, both platforms.
func (s *Service) descendsFromSpawned(spawnedPID, pid int) bool {
	if spawnedPID <= 0 || pid <= 0 {
		return false
	}
	return s.Host.DescendsFrom(spawnedPID, pid)
}

// waitForStopped waits until the port no longer accepts connections.
func (s *Service) waitForStopped(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := s.Host.Listening(ctx, s.Settings.Port)
		if err != nil {
			return exitcode.Wrap(exitcode.Preflight, err)
		}
		if !result.Listening {
			return nil
		}
		if time.Now().After(deadline) {
			return exitcode.New(exitcode.Failure,
				"端口 %d 仍被 pid=%d 占用，停止超时", s.Settings.Port, result.PID)
		}
		if err := s.sleep(ctx, s.poll); err != nil {
			return err
		}
	}
}

// withLock runs fn while holding the operation lock, provisioning the state
// directory first so that reporting commands can stay free of side effects.
func (s *Service) withLock(ctx context.Context, fn func() error) error {
	_, err := withLockValue(ctx, s, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// withLockValue runs fn under the operation lock for operations with a result.
func withLockValue[T any](ctx context.Context, s *Service, fn func() (T, error)) (T, error) {
	var zero T
	held, err := lock.Acquire(ctx, s.Settings.LockFile(), s.Settings.LockTimeout)
	if err != nil {
		return zero, exitcode.Wrap(exitcode.LockTimeout, err)
	}
	defer held.Release()
	if err := provision(s); err != nil {
		return zero, err
	}
	return fn()
}

// provision creates the state directory and the settings document.
func provision(s *Service) error {
	if err := paths.EnsureDir(s.Settings.StateDir); err != nil {
		return err
	}
	if err := s.Settings.Provision(); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	return nil
}

// statusSummary explains an unactionable state in one line.
func statusSummary(status Status) string {
	switch status.State {
	case StateRunning:
		return "运行中"
	case StateStarting:
		return "启动中"
	case StateForeign:
		return fmt.Sprintf("端口 %d 被其他程序占用 (pid=%d)", status.Port, status.ListenerPID)
	case StateOrphan:
		if status.Survivor {
			return fmt.Sprintf("端口 %d 上是上次启动被中断后仍存活的服务 (pid=%d)", status.Port, status.ListenerPID)
		}
		return fmt.Sprintf("端口 %d 被一个 dshctl 无法确认归属的进程占用 (pid=%d), 运行记录缺失或与之矛盾", status.Port, status.ListenerPID)
	default:
		if status.RecordLive {
			return fmt.Sprintf("未监听端口 %d(记录中的 pid=%d 仍然存活)", status.Port, status.RecordedPID)
		}
		return "未运行"
	}
}
