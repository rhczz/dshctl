package domain

import (
	"strings"
	"testing"
)

// TestDescribe pins the diagnostic line, including the runtime the recorded
// server was started with.
//
// The release belongs here because it is the one fact the record carries that
// nothing else can answer afterwards: --node can differ from the settings
// document, so a server already running has no other place to say what it runs.
// A record written by an older build carries none, which the empty case pins.

func TestDescribe(t *testing.T) {
	record := Record{
		PID: 42, StartedAt: 1_700_000_000, Port: 3080, Phase: PhaseRunning,
		URL: "http://x", NodeVersion: "24.20.0", NodePath: "/opt/node/bin/node",
		RepoDir: "/srv/deepseek-harness",
	}
	text := record.Describe()
	for _, want := range []string{"pid=42", "port=3080", "phase=running", "http://x", "node=24.20.0", "repo=/srv/deepseek-harness"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Describe = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "node=") && strings.Contains(text, "/opt/node/bin/node") {
		t.Fatalf("Describe = %q, want the release rather than the whole path on one line", text)
	}

	// The checkout is a fact about the instance rather than a detail of the
	// runtime, so a record that carries none must not look as if it served from
	// somewhere: an empty path in the line would read as a directory named "".
	withoutRuntime := Record{PID: 42, StartedAt: 1_700_000_000, Port: 3080, Phase: PhaseRunning}
	for _, absent := range []string{"node=", "repo="} {
		if strings.Contains(withoutRuntime.Describe(), absent) {
			t.Fatalf("Describe = %q, want no %q for a record that carries none", withoutRuntime.Describe(), absent)
		}
	}
}
