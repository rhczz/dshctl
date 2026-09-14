// Package detach creates the background server.
//
// A detached child must survive the shell that ran dshctl and must not be
// killed by a signal aimed at a terminal: starting the Web server and then
// closing the terminal has to leave it running. How that is expressed differs
// per platform, so it lives behind a build tag.
//
// The child is also reaped. Reaping looks like the opposite of detaching, and it
// is easy to leave out — but a child that exited and was never waited for stays
// in the process table as a zombie, and a zombie still answers `signal 0`. Every
// "is the process I started still alive?" check would then answer yes forever,
// which turns a fast failure into a full timeout.
package detach

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// Command builds a command that is detached from dshctl's terminal.
//
// On Unix the child becomes a session leader; on Windows it is created in its
// own process group without a console. In both cases dshctl does not wait for it
// in the caller's flow.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = sysProcAttr()
	return cmd
}

// Describe renders the platform's detachment mode for diagnostics.
func Describe() string { return detachment }

// Process is a started child: its pid, and a way to learn when it is gone.
type Process struct {
	// PID is the child's process id.
	PID int

	command *exec.Cmd
	done    chan struct{}
	once    sync.Once
}

// Start launches cmd and returns the running child.
//
// The child is left running and is reaped by a background wait. Reaping is what
// keeps a child that exited from lingering as a zombie: without it, existence
// checks keep reporting the dead process as alive, and every lifecycle decision
// built on them is wrong.
func Start(cmd *exec.Cmd) (*Process, error) {
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("无法启动 %s: %w", cmd.Path, err)
	}
	process := &Process{
		PID:     cmd.Process.Pid,
		command: cmd,
		done:    make(chan struct{}),
	}
	// The goroutine outlives every caller: it owns only the child handle and
	// ends when the child does. Cancelling a start attempt must not leave the
	// child unreaped, so it is deliberately not tied to the caller's context.
	go process.reap()
	return process, nil
}

// reap waits for the child so the operating system can release it.
func (p *Process) reap() {
	_ = p.command.Wait()
	p.once.Do(func() { close(p.done) })
}

// Exited reports whether the child has ended, without blocking.
//
// A child that has ended is gone for good: this is the answer a caller needs to
// fail fast instead of waiting out a timeout. An exit that has already been
// reaped therefore outranks a context that is already done — the process is
// gone whatever the caller's deadline says, and reporting a dead server as
// running turns a fast failure into a full timeout.
//
// Parameters:
//   - ctx: cancellation ends the wait, not the child.
//
// Returns:
//   - true when the child is gone.
//   - false while the child has not been observed to end, which includes the
//     case where ctx was cancelled first.
func (p *Process) Exited(ctx context.Context) bool {
	if p == nil {
		return true
	}
	if p.reaped() {
		return true
	}
	// Give a process that ended microseconds ago a bounded chance to be reaped,
	// so a wait loop does not need a poll interval to notice.
	select {
	case <-p.done:
		return true
	case <-ctx.Done():
		return false
	case <-newTimer(reapGrace):
		return false
	}
}

// Wait blocks until the child is gone or ctx is cancelled.
//
// Returns:
//   - true when the child ended.
func (p *Process) Wait(ctx context.Context) bool {
	if p == nil {
		return true
	}
	if p.reaped() {
		return true
	}
	select {
	case <-p.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// reaped reports whether the child's exit has already been observed.
//
// The check is separate from the selects below on purpose: when both a closed
// done channel and a cancelled context are ready, a select chooses between them
// at random, so an already-reaped child would be reported as running about half
// the time — an answer that flips while nothing about the process has changed.
func (p *Process) reaped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}
