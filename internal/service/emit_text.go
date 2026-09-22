package service

import (
	"fmt"
	"io"
)

// TextEmitter renders events as the lines the command line prints.
//
// It is the reference implementation of Emitter and the one the command line
// installs. The mapping is the whole interface between the core and an operator:
// a narrative line on standard output, a warning on standard error with its
// prefix, a caller-framed diagnostic on standard error as it stands, and raw
// output forwarded without punctuation.
//
// A second front-end does not use this: it implements Emitter and renders the
// same events as the frames its protocol speaks.
type TextEmitter struct {
	Out io.Writer
	Err io.Writer
}

// Emit renders one event.
func (t TextEmitter) Emit(event Event) {
	switch event.Kind {
	case EventWarning:
		fmt.Fprintf(t.err(), i18nLine(MsgTextWarning), event.Text)
	case EventError:
		fmt.Fprintln(t.err(), event.Text)
	case EventOutput:
		fmt.Fprint(t.out(), event.Text)
	default:
		fmt.Fprintln(t.out(), event.Text)
	}
}

// Stream is where raw output goes.
func (t TextEmitter) Stream() io.Writer { return t.out() }

// Diagnostics is where raw standard error goes.
func (t TextEmitter) Diagnostics() io.Writer { return t.err() }

// A front-end is sometimes built without one of its streams — a library caller
// that only wants results, a test that only asserts on stdout. Writing to a nil
// writer would panic inside fmt, which is not a failure the caller can act on.
func (t TextEmitter) out() io.Writer {
	if t.Out == nil {
		return io.Discard
	}
	return t.Out
}

func (t TextEmitter) err() io.Writer {
	if t.Err == nil {
		return io.Discard
	}
	return t.Err
}
