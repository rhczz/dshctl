package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/service"
)

// This file is the shell's second rendering of a mutating command.
//
// The service layer does not print: it returns its result and emits events. The
// text rendering is one front-end implementation (service.TextEmitter); this is
// the other, and it is why `--json` costs the core nothing — a supervisor asks
// for the same run as one document instead of a story.

// jsonEvent is one thing that happened during the run.
type jsonEvent struct {
	// Kind is narrative, output, warning or error.
	Kind string `json:"kind"`
	// Text is the line as the text rendering would have shown it.
	Text string `json:"text"`
}

// jsonEmitter collects events for the document and drops the streams.
//
// Raw output is not part of the document: a build's console output belongs in
// the log file, and a caller parsing JSON does not want it inlined. The events
// carry what the operation said; the result carries what it did.
type jsonEmitter struct {
	events []jsonEvent
}

func (j *jsonEmitter) Emit(event service.Event) {
	j.events = append(j.events, jsonEvent{Kind: kindName(event.Kind), Text: event.Text})
}

func (j *jsonEmitter) Stream() io.Writer      { return io.Discard }
func (j *jsonEmitter) Diagnostics() io.Writer { return io.Discard }

func (j *jsonEmitter) list() []jsonEvent {
	if j.events == nil {
		return []jsonEvent{}
	}
	return j.events
}

func kindName(kind service.EventKind) string {
	switch kind {
	case service.EventOutput:
		return "output"
	case service.EventWarning:
		return "warning"
	case service.EventError:
		return "error"
	default:
		return "narrative"
	}
}

// mutatingDocument is what `--json` prints for a mutating command: whether the
// run succeeded, what it returned, and what it said along the way.
type mutatingDocument struct {
	// Command is the command name, so a log of documents says what ran.
	Command string `json:"command"`
	// OK reports whether the command succeeded. It is the same answer the exit
	// code gives, in the document, so a caller reading only stdout can branch.
	OK bool `json:"ok"`
	// Result is the command's own value, when it has one.
	Result any `json:"result,omitempty"`
	// Error is the failure, when there was one.
	Error string `json:"error,omitempty"`
	// Events are the lines the text rendering would have printed.
	Events []jsonEvent `json:"events"`
}

// runJSON runs one mutating command with event collection and prints its
// document.
//
// A failure is reported inside the document and as the process exit code, with
// no prose on standard error: a caller that asked for JSON asked for one
// document, and splitting the answer between a document and a sentence is how a
// parser ends up guessing.
//
// settle is for the commands whose *answer* can be partial even when the run
// completed — a stop that could not confirm every instance — so the document and
// the exit code agree with the text rendering instead of contradicting it.
func runJSON(env *Env, name string, run func(*service.Service) (any, error), settle func(any) error) error {
	events := &jsonEmitter{}
	application := newApp(env)
	application.Emit = events
	result, err := run(application)
	if err == nil && settle != nil {
		err = settle(result)
	}
	document := mutatingDocument{Command: name, OK: err == nil, Result: result, Events: events.list()}
	if err != nil {
		document.Error = err.Error()
	}
	if printErr := printJSON(env.Stdout, document); printErr != nil {
		return printErr
	}
	if err != nil {
		// A cancelled run answers 130 in both renderings. The text path checks
		// this before it classifies the error, because a cancellation wrapped by
		// whichever call was running is still a cancellation.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return exitcode.SilentExit(exitcode.Interrupted)
		}
		return exitcode.SilentExit(exitcode.Of(err))
	}
	return nil
}

// jsonFlag registers the flag every mutating command accepts.
func jsonFlag(flags *flag.FlagSet) *bool {
	return flags.Bool("json", false, "以 JSON 输出(把整次运行作为一份文档)")
}
