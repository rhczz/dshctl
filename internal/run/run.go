// Package run executes external commands behind one small interface, so every
// caller can be driven by a fake in tests.
//
// Cancellation is part of the contract: a cancelled run ends the child and
// everything it started. Commands that lead helper processes (pnpm drives tsc
// and vite) would otherwise leave compilers behind holding the build directory.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// DefaultOutputCap bounds how much of a command's output is kept in memory.
const DefaultOutputCap = 8 << 20

// Command describes one external process invocation.
type Command struct {
	// Name is the executable to run, resolved through PATH.
	Name string
	// Args are the arguments after Name.
	Args []string
	// Dir is the working directory; empty inherits dshctl's.
	Dir string
	// Env replaces the process environment when non-nil.
	Env []string
	// Stdin, Stdout and Stderr are the standard streams; nil discards output.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// String renders the invocation for diagnostics.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Executor runs commands.
type Executor interface {
	// Run executes cmd and waits for it to exit.
	Run(ctx context.Context, cmd Command) error
}

// Capturer runs a command and collects both streams.
//
// It is a separate interface from Executor because collecting output is a
// different capability from running a command: a fake that answers "what does
// git say" implements this one, while a fake that only records invocations
// implements Executor. Separating them is what keeps a test double from
// silently falling back to the real tool.
type Capturer interface {
	// Capture executes cmd and returns its collected streams.
	Capture(ctx context.Context, cmd Command) Result
}

// Outputer runs a command and returns its trimmed standard output.
type Outputer interface {
	// Output executes cmd and returns its trimmed standard output.
	Output(ctx context.Context, cmd Command) (string, error)
}

// Collector adapts an execution capability into output collection.
//
// A *Runner implements all three interfaces, so production code passes one
// directly. A test double that implements Capturer is honoured as it is.
func Collector(ex Executor) Outputer {
	if collector, ok := ex.(Outputer); ok {
		return collector
	}
	if capturer, ok := ex.(Capturer); ok {
		return captureAdapter{capturer}
	}
	return runAdapter{ex}
}

// NewCollector adapts a Capturer into an Outputer.
//
// A component that only needs to read a command's output takes this directly,
// so its test double never has to implement command execution it does not use.
func NewCollector(capturer Capturer) Outputer {
	if capturer == nil {
		return NewRunner()
	}
	return captureAdapter{capturer}
}

// captureAdapter serves Output from a Capturer.
type captureAdapter struct{ capturer Capturer }

// Output implements Outputer.
func (a captureAdapter) Output(ctx context.Context, cmd Command) (string, error) {
	result := a.capturer.Capture(ctx, cmd)
	if result.Err == nil {
		return result.Stdout, nil
	}
	if result.Stderr != "" {
		return result.Stdout, fmt.Errorf("%w: %s", result.Err, result.Stderr)
	}
	return result.Stdout, result.Err
}

// runAdapter serves Output from a plain Executor.
//
// A plain Executor can run a command but has no way to hand back what it wrote,
// so the output is empty and the failure is the executor's own. What matters is
// that the command goes where the caller pointed it: reaching for a real runner
// here would run a real process behind an injected executor's back and make
// every test above it measure the machine instead of the code.
type runAdapter struct{ ex Executor }

// Output implements Outputer.
func (a runAdapter) Output(ctx context.Context, cmd Command) (string, error) {
	if a.ex == nil {
		return "", errors.New("there is no command executor, so output cannot be collected")
	}
	return "", a.ex.Run(ctx, cmd)
}

// Result is what one captured command produced.
type Result struct {
	// Stdout is the command's standard output, trimmed.
	Stdout string
	// Stderr is the command's standard error, trimmed.
	Stderr string
	// Err is nil on a zero exit, an *ExitError on a non-zero exit, and the start
	// failure when the command never ran.
	Err error
	// Truncated reports that more output arrived than the cap allows.
	Truncated bool
}

// ExitError reports a command that ran and exited non-zero.
type ExitError struct {
	// Command is the command line that failed.
	Command string
	// Code is the process exit status.
	Code int
}

// Error implements error.
func (e *ExitError) Error() string {
	return fmt.Sprintf("%s: exit status %d", e.Command, e.Code)
}

// Runner executes commands with os/exec.
type Runner struct {
	// OutputCap bounds how much output Capture keeps; 0 uses DefaultOutputCap.
	OutputCap int
}

// NewRunner returns a Runner with the default output cap.
func NewRunner() *Runner { return &Runner{OutputCap: DefaultOutputCap} }

// Run executes cmd with the caller's streams and waits for it.
//
// Parameters:
//   - ctx: cancellation kills the child's whole process tree.
//   - cmd: invocation to run.
//
// Returns:
//   - a context error when ctx was cancelled, an *ExitError when the command
//     exited non-zero, or a start failure when it never ran.
func (r *Runner) Run(ctx context.Context, cmd Command) error {
	if r == nil {
		r = &Runner{}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", cmd.String(), err)
	}
	process := exec.Command(cmd.Name, cmd.Args...)
	process.Dir = cmd.Dir
	process.Env = cmd.Env
	process.Stdin = cmd.Stdin
	process.Stdout = cmd.Stdout
	process.Stderr = cmd.Stderr
	applyProcessGroup(process)

	if err := process.Start(); err != nil {
		return fmt.Errorf("%s: %w", cmd.String(), err)
	}

	exited := make(chan struct{})
	guard := &reapedGuard{}
	go func() {
		select {
		case <-ctx.Done():
			guard.killIfLive(func() { killTree(process) })
		case <-exited:
		}
	}()

	err := process.Wait()
	guard.markReaped()
	close(exited)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", cmd.String(), ctxErr)
	}
	if err != nil {
		var status *exec.ExitError
		if errors.As(err, &status) {
			return &ExitError{Command: cmd.String(), Code: status.ExitCode()}
		}
		return fmt.Errorf("%s: %w", cmd.String(), err)
	}
	return nil
}

// Capture runs cmd and collects both streams, teeing them to the caller's
// writers when it supplied any.
//
// Parameters:
//   - ctx: cancellation kills the child.
//   - cmd: invocation to run; Capture supplies the streams, teeing to the
//     stdout and stderr the caller set.
//
// Returns:
//   - the trimmed streams, the exit status, and whether output was capped.
func (r *Runner) Capture(ctx context.Context, cmd Command) Result {
	if r == nil {
		r = &Runner{}
	}
	limit := r.OutputCap
	if limit <= 0 {
		limit = DefaultOutputCap
	}
	var stdout, stderr boundedBuffer
	stdout.limit = limit
	stderr.limit = limit

	outSink := io.Writer(&stdout)
	if cmd.Stdout != nil {
		outSink = io.MultiWriter(cmd.Stdout, &stdout)
	}
	errSink := io.Writer(&stderr)
	if cmd.Stderr != nil {
		errSink = io.MultiWriter(cmd.Stderr, &stderr)
	}
	captured := cmd
	captured.Stdout = outSink
	captured.Stderr = errSink

	err := r.Run(ctx, captured)
	return Result{
		Stdout:    strings.TrimSpace(stdout.String()),
		Stderr:    strings.TrimSpace(stderr.String()),
		Err:       err,
		Truncated: stdout.truncated || stderr.truncated,
	}
}

// Output runs cmd and returns its trimmed standard output. A non-zero exit
// returns the tool's own diagnostic inside the error, so callers can report it
// instead of a bare exit status.
func (r *Runner) Output(ctx context.Context, cmd Command) (string, error) {
	if r == nil {
		r = &Runner{}
	}
	result := r.Capture(ctx, cmd)
	if result.Err == nil {
		return result.Stdout, nil
	}
	if result.Stderr != "" {
		return result.Stdout, fmt.Errorf("%w: %s", result.Err, result.Stderr)
	}
	return result.Stdout, result.Err
}

// IsExit reports whether err is the exit status of a command that ran and
// returned code.
func IsExit(err error, code int) bool {
	var status *ExitError
	return errors.As(err, &status) && status.Code == code
}

// IsNotFound reports whether err means the executable does not exist.
func IsNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// LookPath reports where name resolves on the current PATH.
func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// withPathPrefix prepends dir to the PATH entry of env, appending one when the
// environment has none.
//
// The key is matched case-insensitively: Windows shells usually spell it
// "Path", and Go's exec resolves duplicate environment keys case-insensitively
// there, keeping the last. A case-sensitive search would miss the entry and
// append a second one — which would then win and silently replace the child's
// whole PATH with dir alone.
func withPathPrefix(env []string, dir string) []string {
	for index, entry := range env {
		equal := strings.IndexByte(entry, '=')
		if equal < 0 || !strings.EqualFold(entry[:equal], "PATH") {
			continue
		}
		env[index] = entry[:equal+1] + dir + string(os.PathListSeparator) + entry[equal+1:]
		return env
	}
	return append(env, "PATH="+dir)
}

// WithPathPrefix returns the current environment with dir prepended to PATH.
//
// It runs a child with the Node version dshctl resolved without mutating
// dshctl's own PATH. When the environment has no PATH at all the result is dir
// alone, which is the only value that keeps a broken environment honest.
func WithPathPrefix(dir string) []string {
	env := os.Environ()
	if dir == "" {
		return env
	}
	return withPathPrefix(env, dir)
}

// reapedGuard guards the cancellation kill.
//
// A child's process group id is its pid, so once the child has been waited for
// that id may already belong to somebody else: killing "its" group would reach
// a stranger. The guard cannot make the decision atomic with the signal — that
// is a kernel-level race nothing in userspace can close — but it removes the
// exposure that came from a cancellation waking up after the child was reaped,
// which is the window that actually opens in practice.
type reapedGuard struct {
	mu     sync.Mutex
	reaped bool
}

// markReaped records that the child has been waited for.
func (g *reapedGuard) markReaped() {
	g.mu.Lock()
	g.reaped = true
	g.mu.Unlock()
}

// killIfLive runs kill unless the child has already been reaped.
func (g *reapedGuard) killIfLive(kill func()) {
	g.mu.Lock()
	reaped := g.reaped
	g.mu.Unlock()
	if !reaped {
		kill()
	}
}

// boundedBuffer keeps at most limit bytes and remembers that it dropped some.
type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

// Write implements io.Writer, dropping everything past the cap.
func (b *boundedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	switch {
	case remaining <= 0:
		b.truncated = true
		return len(data), nil
	case len(data) > remaining:
		if _, err := b.buffer.Write(data[:remaining]); err != nil {
			return 0, err
		}
		b.truncated = true
		return len(data), nil
	default:
		return b.buffer.Write(data)
	}
}

// String returns what was kept.
func (b *boundedBuffer) String() string { return b.buffer.String() }
