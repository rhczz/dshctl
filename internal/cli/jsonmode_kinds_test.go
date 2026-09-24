package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rhczz/dshctl/internal/service"
)

// TestKindNamesEveryEvent pins the mapping the JSON document's `kind` field
// promises: every event kind a run can produce has a name, and none of them
// falls back to "narrative" by accident — a supervisor reading `kind: "error"`
// has to be able to trust that it means an error.
func TestKindNamesEveryEvent(t *testing.T) {
	cases := []struct {
		kind service.EventKind
		want string
	}{
		{service.EventNarrative, "narrative"},
		{service.EventOutput, "output"},
		{service.EventWarning, "warning"},
		{service.EventError, "error"},
	}
	for _, testCase := range cases {
		if got := kindName(testCase.kind); got != testCase.want {
			t.Fatalf("kindName(%d) = %q, want %q", testCase.kind, got, testCase.want)
		}
	}
}

// TestJSONDocumentNamesWarningsAndErrors runs the document path over the
// non-narrative kinds through the real emitter wiring: an event the shell
// forgot to name would come out as "narrative" here, which is exactly the
// silent rewrite this test is here to catch.
func TestJSONDocumentNamesWarningsAndErrors(t *testing.T) {
	events := &jsonEmitter{}
	events.Emit(service.Event{Kind: service.EventWarning, Text: "careful"})
	events.Emit(service.Event{Kind: service.EventError, Text: "broken"})

	document := mutatingDocument{
		Command: "update",
		OK:      false,
		Error:   "the switch failed",
		Events:  events.list(),
	}
	var out bytes.Buffer
	if err := printJSON(&out, document); err != nil {
		t.Fatalf("printJSON: %v", err)
	}
	var parsed struct {
		Events []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"events"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("document is not JSON: %v\n%s", err, out.String())
	}
	if len(parsed.Events) != 2 {
		t.Fatalf("events = %+v, want both", parsed.Events)
	}
	if parsed.Events[0].Kind != "warning" || parsed.Events[0].Text != "careful" {
		t.Fatalf("event 0 = %+v, want the warning with its text", parsed.Events[0])
	}
	if parsed.Events[1].Kind != "error" || parsed.Events[1].Text != "broken" {
		t.Fatalf("event 1 = %+v, want the error with its text", parsed.Events[1])
	}
}
