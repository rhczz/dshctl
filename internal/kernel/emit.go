package kernel

import "io"

// This file is the port a front-end watches an operation through.
//
// The lifecycle does not print: it emits what happened, and whoever called it
// decides how that becomes bytes. The command line renders events as the text an
// operator has always read; a second front-end — an HTTP or gRPC shell over the
// same use cases — renders the same events as structured frames; a test records
// them and asserts on the model instead of on the presentation.
//
// This is why the package has no stdout of its own: a writer here would be a
// command line inside the core, and the core would have to be rewritten the day
// a request arrives over the network instead of over a terminal.

// EventKind separates the three things an operation produces.
type EventKind int

const (
	// EventNarrative is a sentence about what the operation is doing: the line
	// an operator reads on standard output.
	EventNarrative EventKind = iota
	// EventOutput is raw output rather than a sentence: the log file being
	// followed, or a child process writing while it runs. A shell forwards it
	// as bytes; nothing adds punctuation to it.
	EventOutput
	// EventWarning is something the operator has to know that does not stop the
	// operation. A shell frames it as advice rather than as a result.
	EventWarning
	// EventError is a diagnostic whose text the emitter already framed, so a
	// shell does not prefix it a second time.
	EventError
)

// Event is one thing that happened during an operation.
type Event struct {
	Kind EventKind
	Text string
}

// Emitter is where a front-end watches an operation.
//
// It is deliberately two methods and not one per operation: Emit carries
// anything that reads as a line, and Stream carries anything that does not — a
// build's output, a followed log — because copying a byte stream through strings
// to call it structured would buy nothing and lose the streaming.
//
// A front-end that wants structure subscribes to the events it cares about: the
// use cases return their results as values (StartResult, domain.Status, TimelineReport,
// and the rest), so nothing has to be recovered from the text.
type Emitter interface {
	// Emit delivers one event.
	Emit(Event)
	// Stream returns the writer raw output is copied to: a child's standard
	// output, a log file being followed.
	Stream() io.Writer
	// Diagnostics returns the writer raw standard error is copied to: a child's
	// standard error, the log tail shown after a failed start. It is a stream
	// and not an event because it is not ours to punctuate.
	Diagnostics() io.Writer
}

// discardEmitter is what a Service without an emitter uses. It is not a silent
// failure: the caller configured no front-end, which is the shape a test that
// only inspects return values has.
type discardEmitter struct{}

func (discardEmitter) Emit(Event)             {}
func (discardEmitter) Stream() io.Writer      { return io.Discard }
func (discardEmitter) Diagnostics() io.Writer { return io.Discard }

// emitter answers the emitter to use for one call, so no call site has to ask
// whether a front-end was configured.
func (s *Service) emitter() Emitter {
	if s.Emit == nil {
		return discardEmitter{}
	}
	return s.Emit
}

// narrate emits a sentence about the operation.
func (s *Service) narrate(text string) {
	s.emitter().Emit(Event{Kind: EventNarrative, Text: text})
}

// output emits raw output.
func (s *Service) output(text string) {
	s.emitter().Emit(Event{Kind: EventOutput, Text: text})
}

// warning emits advice that does not stop the operation.
func (s *Service) warning(text string) {
	s.emitter().Emit(Event{Kind: EventWarning, Text: text})
}

// failure emits a diagnostic the caller already framed.
func (s *Service) failure(text string) {
	s.emitter().Emit(Event{Kind: EventError, Text: text})
}
