//go:build unix && !linux

package host

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// This file pins the ps-based process facts, which is the path Unix platforms
// without a direct kernel read rely on. Linux answers through /proc instead and
// takes precedence over ps (see facts_linux_test.go), so the expectations here —
// a start time derived from ps's elapsed column — only hold where that read does
// not exist. The build tag is what keeps the two apart: a darwin expectation in a
// plain `unix` file is a test that fails on Linux, which is worse than no test at
// all because it hides every real failure behind it.

// TestInspectReadsThePSFallback pins the ps-based path: the elapsed time becomes
// a start time, and the command line comes along for diagnostics.
func TestInspectReadsThePSFallback(t *testing.T) {
	dir := t.TempDir()
	ps := stubTool(t, dir, "ps", "01:02:03 node --import tsx/esm apps/cli/src/bin.ts web --port 3080\n")
	host := &Host{lookPath: func(name string) (string, error) {
		if name == "ps" {
			return ps, nil
		}
		return "", fmt.Errorf("no %s", name)
	}}

	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("this process must be reported as alive")
	}
	if facts.Command == "" || facts.Command[:4] != "node" {
		t.Fatalf("Command = %q, want the process command line", facts.Command)
	}
	if facts.StartedAt <= 0 {
		t.Fatalf("StartedAt = %d, want a start time derived from the elapsed time", facts.StartedAt)
	}
	if facts.Source != "ps" {
		t.Fatalf("Source = %q, want ps: no other probe exists here", facts.Source)
	}
	// 1h2m3s ago, within a minute of measurement.
	now := timeNow()
	delta := now - facts.StartedAt
	if delta < 3600 || delta > 3900 {
		t.Fatalf("StartedAt is %ds before now, want about 3723s", delta)
	}
}

// TestInspectKeepsAProcessAliveWhenPSDescribesNothing pins that an unhelpful ps
// answer is not evidence of death. The process exists — signal 0 proves it — and
// saying otherwise makes a live server look gone.
func TestInspectKeepsAProcessAliveWhenPSDescribesNothing(t *testing.T) {
	dir := t.TempDir()
	ps := stubTool(t, dir, "ps", "")
	host := &Host{lookPath: func(string) (string, error) { return ps, nil }}

	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatalf("facts = %+v, want the process kept alive", facts)
	}
	if facts.Command != "" || facts.StartedAt != 0 {
		t.Fatalf("facts = %+v, want no invented details", facts)
	}
}

// TestAliveDoesNotDependOnPATH is the regression test for a live server being
// reported as gone because the process probe could not find ps.
func TestAliveDoesNotDependOnPATH(t *testing.T) {
	t.Setenv("PATH", "")
	host := New()
	if !host.Alive(context.Background(), os.Getpid()) {
		t.Fatal("a live process must be reported as alive without any tool on PATH")
	}
	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatalf("Inspect = %+v, want the process kept alive when no tool is reachable", facts)
	}
	if facts.Command != "" || facts.StartedAt != 0 {
		t.Fatalf("Inspect = %+v, want no invented details", facts)
	}
}

// TestInspectFallsBackWhenPSIsDenied pins the sandbox case: ps refuses, and the
// result is still a live process with whatever facts are available, never a
// confident "gone".
func TestInspectFallsBackWhenPSIsDenied(t *testing.T) {
	host := &Host{lookPath: func(string) (string, error) {
		return "", fmt.Errorf("tool unavailable")
	}}
	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("a process that exists must stay alive when ps cannot be consulted")
	}
	if facts.StartedAt != 0 {
		t.Fatalf("StartedAt = %d, want 0 when nothing could be read", facts.StartedAt)
	}
}
