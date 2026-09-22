// Package host is the only place in dshctl that talks to the operating system.
//
// Process liveness, process facts, port ownership, signal delivery and detached
// process creation all have different shapes on Unix and on Windows. Each of
// them is defined here once and implemented per platform behind build tags, so
// the lifecycle code above this package never contains an "if windows".
//
// Two contracts matter more than the rest:
//
//   - "cannot look" is never reported as a fact. Every probe returns an error
//     when the operating system refused to answer, so a caller can fail closed
//     instead of concluding that a port is free or that a process is gone.
//   - nothing here decides whether a process is dshctl's server. Ownership comes
//     from the state record plus the start-time fingerprint; this package only
//     reports what exists and delivers what it is told to deliver.
package host

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// ErrUnsupported reports that a probe cannot be answered on this platform with
// the tools available. Callers must treat it as "unknown", never as "free".
var ErrUnsupported = errors.New("this probe cannot be answered on this platform")

// Request names the strength of a termination request.
//
// The strength is a request, not a guarantee: Windows has no portable graceful
// signal, so both strengths end the process there, while Unix delivers SIGTERM
// and SIGKILL respectively. Callers must therefore treat Graceful as "ask
// nicely where the platform can" and verify the outcome themselves.
type Request int

const (
	// Graceful asks the process to shut down and flush its state.
	Graceful Request = iota
	// Force ends the process without giving it a chance to clean up.
	Force
)

// Facts describes a process as far as the operating system will say.
type Facts struct {
	// PID is the process that was inspected.
	PID int
	// Alive reports whether the process exists.
	Alive bool
	// StartedAt is the process start time in Unix seconds, or 0 when unknown.
	StartedAt int64
	// Command is the command line, or empty when it could not be read.
	Command string
	// Source names the tool that answered, for diagnostics.
	Source string
}

// Tools names the external programs a probe may use, in the order they are
// trusted.
//
// Which inventory tools an installation has, and which of them a product is
// willing to trust, is not this package's decision: the parsers here know each
// dialect, but only the caller knows what it has tested and what it prefers
// first. The mechanism therefore asks for the list, and an empty one means
// "cannot look" rather than a guessed default.
type Tools struct {
	// Port names the port-probing tools in trust order. The names this build can
	// parse are "lsof", "ss" and "netstat"; a name it cannot parse is reported
	// as a failed probe rather than ignored silently.
	Port []string
	// Process names the tool that reads process facts on Unix, e.g. "ps".
	Process string
}

// Host is the operating-system surface dshctl depends on.
type Host struct {
	lookPath func(string) (string, error)
	tools    Tools
}

// New returns a Host bound to the real machine.
func New(tools Tools) *Host {
	return &Host{lookPath: exec.LookPath, tools: tools}
}

// NewWithLookPath returns a Host whose executable lookups are substituted. The
// port probes still use the real operating system; tests use it to decide which
// of them report themselves as available.
func NewWithLookPath(lookPath func(string) (string, error), tools Tools) *Host {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return &Host{lookPath: lookPath, tools: tools}
}

// PortResult is what a port probe learned.
type PortResult struct {
	// Listening reports whether something is listening.
	Listening bool
	// PID is the listening process, or 0 when the platform did not report one.
	PID int
}

// CtxTimeout bounds a probe that a platform tool answers.
const probeTimeout = 10 * time.Second

// probeContext derives a bounded context for one probe.
//
// A nil context is treated as the background context so that a probe can never
// panic a caller that did not bother to pass one.
func probeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, probeTimeout)
}
