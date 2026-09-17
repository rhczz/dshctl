// Package service implements the lifecycle of the DeepSeek Harness Web server:
// observing it, starting it, stopping it, restarting it, building and updating
// the checkout, and the diagnostics the command line exposes.
//
// Three invariants shape every operation in this package.
//
//  1. Ownership comes from the runtime record, never from a guess. dshctl stops
//     a process only when the record names that pid *and* the process start time
//     still matches, so a recycled pid can never be signalled.
//  2. "Cannot look" is never reported as a fact. A port probe that failed leaves
//     the service in an unknown state, and every operation refuses to act on an
//     unknown state instead of assuming the port is free.
//  3. A service that is listening but not owned by dshctl is reported and left
//     alone. dshctl never kills a process it did not start.
//  4. The port is a *selector*, not the identity of the installation. One state
//     directory manages every server dshctl started, one record per port: naming
//     a port on the command line, or in the environment, acts on that instance
//     alone, and leaving it out acts on all of them. Treating the configured
//     port as the only instance is how a server started with `--port` kept
//     serving while `status` reported nothing running and `stop` refused to
//     touch it — an orphan nothing in the tool could name again.
//
// Rules 1-3 are per instance and rule 4 is about which instances an operation
// covers; an operation that covers several holds one lock for all of them, so no
// other command sees half of it.
package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/logfile"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/repo"
	"github.com/rhczz/dshctl/internal/run"
	"github.com/rhczz/dshctl/internal/state"
	"github.com/rhczz/dshctl/internal/version"
)

// Service carries everything the lifecycle needs. Every host effect is a field
// so that a test can substitute a fictional machine.
type Service struct {
	// Settings is the effective configuration.
	Settings config.Settings
	// Exec runs external commands.
	Exec run.Executor
	// Host is the operating-system surface.
	Host OsHost
	// Repo is the managed checkout.
	Repo repo.Repo
	// Node resolves the Node runtime.
	Node *nodejs.Resolver
	// Log is the shared log file.
	Log *logfile.Logger
	// Record is the runtime record of the server dshctl started on this port.
	Record state.Store
	// Out is the human-facing output stream.
	Out io.Writer
	// Err carries warnings and errors.
	Err io.Writer
	// Dial reports whether the loopback port accepts a connection. It is a field
	// so tests can decide when a fictional server is ready.
	Dial func(ctx context.Context, port int) bool
	// Spawn starts the detached server and returns its pid.
	Spawn Launcher
	// LookPath resolves an executable on PATH.
	LookPath func(string) (string, error)
	// Getenv reads the environment.
	Getenv func(string) string
	// BuildInfo is the running binary's build metadata.
	BuildInfo version.Info

	// sleep waits for a duration or until the context is cancelled, and poll is
	// how often the wait loops re-check the port. They are unexported because
	// they are the package's own pacing, not a knob for callers.
	sleep func(context.Context, time.Duration) error
	poll  time.Duration
	// grace is how long a stop waits after a graceful request before forcing.
	grace time.Duration
	// fingerprint is how long a start waits for the process start time every
	// later ownership decision is checked against. It is a field for the same
	// reason as the two above: the test that reaches the degraded mode has to
	// exhaust the budget, and spending the production budget to get there makes
	// the suite slow without asserting anything about how long the wait is —
	// that the production value is generous is pinned on its own. Zero means the
	// production value, so a value built by hand stays correct.
	fingerprint time.Duration
	// port is the instance this value acts on. One state directory may manage
	// several servers, so "the port" is part of an operation rather than of the
	// configuration: atPort derives a value bound to another instance, and
	// everything below it — the probe, the spawn arguments, the record — follows
	// that one port. Every field around it is shared on purpose: the lock
	// serializes the whole directory, and the log carries every instance.
	//
	// Zero means "unset", and port() then answers the configured port, which is
	// the shape a value built by hand (a test, or code that predates several
	// instances) has.
	port int
}

// Dependencies are the process-wide values a Service runs with.
type Dependencies struct {
	// Exec runs external commands.
	Exec run.Executor
	// Out and Err are the human-facing streams.
	Out, Err io.Writer
	// Version is the running build's metadata.
	Version version.Info
}

// Launcher starts the detached server.
//
// It returns the child's pid and a function that reports whether the child has
// ended. Reporting the end matters as much as the pid: a child that exited and
// was never collected stays visible as a zombie, so "the process still exists"
// is not the same question as "the server is still coming up".
type Launcher func(path string, args []string, dir string, env []string, log *os.File) (int, func(context.Context) bool, error)

// New wires a Service from resolved settings, the real host and the real
// environment.
func New(settings config.Settings, deps Dependencies) *Service {
	return &Service{
		Settings: settings,
		Exec:     deps.Exec,
		Host:     host.New(),
		Repo: repo.Repo{
			Dir:                  settings.RepoDir,
			Ex:                   deps.Exec,
			ManifestRel:          config.ServerManifestRel,
			WorkspaceManifestRel: config.WorkspaceManifestRel,
			BuildRecordRel:       buildRecordRel,
		},
		Node:      nodejs.NewResolver(),
		Log:       logfile.New(settings.LogPath, settings.LogRotateBytes),
		Record:    state.Store{Path: settings.StateFile()},
		Out:       deps.Out,
		Err:       deps.Err,
		Dial:      dialPort,
		Spawn:     spawnDetached,
		LookPath:  run.LookPath,
		Getenv:    os.Getenv,
		BuildInfo: deps.Version,
		sleep:     sleepCtx,
		poll:      pollInterval,
		grace:     terminateGrace,
		// The production budget: a test shortens it through the field, and
		// TestFingerprintTimeoutIsGenerous pins that this value stays generous.
		fingerprint: fingerprintTimeout,
		port:        settings.Port,
	}
}

// port is the instance this value acts on, falling back to the configured port
// for a value that was not built by New.
func (s *Service) boundPort() int {
	if s.port > 0 {
		return s.port
	}
	return s.Settings.Port
}

// fingerprintBudget is how long a start waits for the process start time,
// falling back to the production value for a value that was not built by New.
func (s *Service) fingerprintBudget() time.Duration {
	if s.fingerprint > 0 {
		return s.fingerprint
	}
	return fingerprintTimeout
}

// atPort derives a value bound to another instance of the same installation.
//
// Everything a command must share stays shared: the state directory, the log,
// the lock, the checkout and the host. Only the port moves, and with it the
// record, the spawn arguments and every probe. The record store is carried over
// explicitly rather than re-derived, because the configured port is not the port
// this value is about any more.
func (s *Service) atPort(port int) *Service {
	bound := *s
	bound.port = port
	bound.Settings.Port = port
	bound.Record = state.Store{Path: bound.Settings.StateFile()}
	return &bound
}

// selection resolves the instances a command acts on.
func (s *Service) selection() (config.StateSelection, error) {
	return s.Settings.StateSelection()
}

// buildRecordRel mirrors config's build marker for the repo package.
const buildRecordRel = ".dsh-build/client-build-environment.json"

// environment returns the injected environment lookup.
func (s *Service) environment() paths.Getenv {
	if s.Getenv != nil {
		return s.Getenv
	}
	return os.Getenv
}

// noteWriter is the reporter used by operations that stream long output.
type note func(string)

// report writes a line to the console and the log.
func (s *Service) report(line string) {
	fmt.Fprintln(s.Out, line)
	s.note(line)
}

// note appends a line to the log, ignoring a log failure that would otherwise
// mask the operation's own result.
func (s *Service) note(line string) {
	_ = s.Log.Line(line)
}

// warn writes a diagnostic that does not stop the operation.
func (s *Service) warn(format string, args ...any) {
	fmt.Fprintf(s.Err, "警告: "+format+"\n", args...)
}

// errorf writes an error line that does not stop the operation.
func (s *Service) errorf(format string, args ...any) {
	fmt.Fprintf(s.Err, format+"\n", args...)
}

// sleepCtx waits for d or until ctx is cancelled, reporting the cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
