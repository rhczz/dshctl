//go:build darwin

package host

import (
	"context"
	"os"
	"testing"
)

// This file pins the darwin side of the platform split in facts_other_unix.go: the
// platform has no /proc, so the kernel read that Linux relies on cannot answer
// here, and the ps fallback is what supplies the fingerprint. Without that fallback
// every darwin record would carry StartedAt == 0, and the start time is the only
// thing that tells a reused pid apart from the server the record describes.

// TestStartTimeFromProcIsUnavailableOnDarwin pins that the direct kernel read
// declines rather than inventing a value.
//
// A stub that answered with a wrong number would be worse than one that answers
// nothing: StartedAt is compared against the recorded fingerprint, so an invented
// value makes a live server look like a stranger — and the caller then refuses to
// stop it, or worse, stops the wrong process.
func TestStartTimeFromProcIsUnavailableOnDarwin(t *testing.T) {
	pid := os.Getpid()
	startedAt, ok := startTimeFromProc(pid)
	if ok {
		t.Fatalf("startTimeFromProc(%d) = (%d, true), want ok == false: darwin has no /proc to read", pid, startedAt)
	}
	if startedAt != 0 {
		t.Fatalf("startTimeFromProc(%d) = %d, want 0 when it cannot answer", pid, startedAt)
	}
}

// TestInspectFallsBackToPSOnDarwin pins the consequence for a live process: no
// fact may come from the source that cannot answer.
//
// Source is the diagnostic that tells an operator which evidence produced the
// facts, so it must name the path that really ran — "proc" on a machine without
// /proc would point every later investigation at a read that never happened.
func TestInspectFallsBackToPSOnDarwin(t *testing.T) {
	pid := os.Getpid()
	facts := New(testTools).Inspect(context.Background(), pid)
	if !facts.Alive {
		t.Fatalf("Inspect(%d) = %+v, want this process reported as alive", pid, facts)
	}
	if facts.Source == "proc" {
		t.Fatalf("Source = %q, want the ps fallback: darwin has no /proc", facts.Source)
	}
	// ps is the fallback, and "signal" means ps could not be run at all — the
	// documented degradation, in which the kernel's existence check is the only
	// evidence left. Anything else would name a source that does not exist.
	if facts.Source != "ps" && facts.Source != "signal" {
		t.Fatalf("Source = %q, want ps (or signal when ps cannot be run)", facts.Source)
	}
	if facts.Source == "ps" {
		if facts.StartedAt <= 0 {
			t.Fatalf("Source = ps but StartedAt = %d, want the start time ps's elapsed column implies", facts.StartedAt)
		}
		if facts.Command == "" {
			t.Fatal("Source = ps but the command line is empty")
		}
	}
}
