package service

import (
	"strings"
	"testing"
)

// TestTextEmitterRendersEveryKind pins the whole interface between the core and
// an operator reading a terminal: which sink each kind reaches, and which of
// them the emitter frames itself.
func TestTextEmitterRendersEveryKind(t *testing.T) {
	var out, errOut strings.Builder
	emitter := TextEmitter{Out: &out, Err: &errOut}

	emitter.Emit(Event{Kind: EventNarrative, Text: "state: running"})
	emitter.Emit(Event{Kind: EventOutput, Text: "raw bytes without a newline"})
	emitter.Emit(Event{Kind: EventWarning, Text: "the remote could not be fetched"})
	emitter.Emit(Event{Kind: EventError, Text: "error: update failed: timed out"})

	if got, want := out.String(), "state: running\nraw bytes without a newline"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got, want := errOut.String(), "warning: the remote could not be fetched\nerror: update failed: timed out\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// TestTextEmitterStreamsRawOutput keeps the streaming half honest: a build's
// output is bytes, not a sentence, and the emitter must hand out the same sink
// for both streams it forwards.
func TestTextEmitterStreamsRawOutput(t *testing.T) {
	var out, errOut strings.Builder
	emitter := TextEmitter{Out: &out, Err: &errOut}
	if _, err := emitter.Stream().Write([]byte("pnpm output")); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if _, err := emitter.Diagnostics().Write([]byte("pnpm stderr")); err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if got := out.String(); got != "pnpm output" {
		t.Errorf("stdout = %q", got)
	}
	if got := errOut.String(); got != "pnpm stderr" {
		t.Errorf("stderr = %q", got)
	}
}

// TestTextEmitterWithoutStreamsDoesNotPanic pins the shape a caller that only
// wants results has: no stream configured means output goes nowhere, not that
// the process dies inside fmt.
func TestTextEmitterWithoutStreamsDoesNotPanic(t *testing.T) {
	emitter := TextEmitter{}
	emitter.Emit(Event{Kind: EventNarrative, Text: "ignored"})
	emitter.Emit(Event{Kind: EventWarning, Text: "ignored"})
	if _, err := emitter.Stream().Write([]byte("ignored")); err != nil {
		t.Fatalf("stream: %v", err)
	}
}
