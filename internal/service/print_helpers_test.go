package service

import (
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/host"
)

// hostFacts builds the facts a probe would report.
func hostFacts(command, source string) host.Facts {
	return host.Facts{Command: command, Source: source}
}

// TestDescribeFacts pins the diagnostic rendering of a process.
func TestDescribeFacts(t *testing.T) {
	if got := describeFacts(hostFacts("node server.js", "ps")); got != "node server.js" {
		t.Fatalf("describeFacts = %q", got)
	}
	if got := describeFacts(hostFacts("", "signal")); !strings.Contains(got, "signal") {
		t.Fatalf("describeFacts = %q, want the source named", got)
	}
	if got := describeFacts(hostFacts("", "")); got != "unknown process" {
		t.Fatalf("describeFacts = %q", got)
	}
}
