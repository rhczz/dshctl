package app

import (
	"context"

	"github.com/rhczz/dshctl/internal/host"
)

// OsHost is the operating-system surface the lifecycle depends on.
//
// It is an interface rather than the concrete host.Host so that the whole
// lifecycle can be driven against a fictional machine: every decision this
// package makes — is the server mine, is the port free, may I signal this pid —
// is then testable without touching the real process table.
type OsHost interface {
	// Listening reports whether something listens on a loopback port.
	Listening(ctx context.Context, port int) (host.PortResult, error)
	// Inspect reports the facts of a process.
	Inspect(ctx context.Context, pid int) host.Facts
	// Alive reports whether a process exists.
	Alive(ctx context.Context, pid int) bool
	// Signal delivers a termination request to one process.
	Signal(pid int, request host.Request) error
	// DescendsFrom reports whether a process descends from the one dshctl
	// spawned — process-group membership on Unix, the parent-pid chain on
	// Windows. It is the evidence that a listener which is not the spawned pid
	// is still ours, and it is the same question on both platforms.
	DescendsFrom(ancestor, pid int) bool
	// GroupExists reports whether any process is left in the tree the pid
	// leads. It is not the same question as DescendsFrom: a tree whose root was
	// reaped still exists while a member lives.
	GroupExists(pid int) bool
	// SignalGroup delivers a termination request to a whole process group.
	SignalGroup(pid int, request host.Request) error
	// KillGroup ends a whole process group without a graceful step.
	KillGroup(pid int) error
}
