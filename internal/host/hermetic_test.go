//go:build unix

package host

import (
	"context"
	"os"
	"testing"
)

// TestInspectionDoesNotDisturbTheProcess pins that reading process facts has no
// side effect on the process being read.
func TestInspectionDoesNotDisturbTheProcess(t *testing.T) {
	host := New()
	// Inspecting a pid that exists must not change its state.
	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("inspection reported this process as gone")
	}
	if !host.Alive(context.Background(), os.Getpid()) {
		t.Fatal("this process disappeared during inspection")
	}
}
