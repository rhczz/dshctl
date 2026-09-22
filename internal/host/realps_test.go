//go:build unix

package host

import (
	"context"
	"os"
	"testing"
)

// TestInspectReadsARealProcess pins the ps path against the kernel itself. It
// skips where ps is denied, which is the environment the fallback exists for.
func TestInspectReadsARealProcess(t *testing.T) {
	facts := New(testTools).Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("this process must be reported as alive")
	}
	if facts.Source == "signal" {
		t.Skip("ps is unavailable here; the fallback path is covered separately")
	}
	if facts.StartedAt <= 0 {
		t.Fatalf("source = %q but no start time was read", facts.Source)
	}
	if facts.Command == "" {
		t.Fatalf("source = %q but no command line was read", facts.Source)
	}
}
