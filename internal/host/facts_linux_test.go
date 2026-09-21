//go:build linux

package host

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestParseStartTicks pins the /proc field extraction, including the process
// name that may itself contain spaces and parentheses.
func TestParseStartTicks(t *testing.T) {
	cases := []struct {
		name string
		stat string
		want int64
		ok   bool
	}{
		{
			name: "ordinary",
			stat: "1234 (node) S 1 1234 1234 0 -1 4194560 100 0 0 0 5 6 0 0 20 0 11 0 987654 123 456 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0",
			want: 987654,
			ok:   true,
		},
		{
			name: "name with spaces and parentheses",
			stat: "1234 (weird (name) here) S 1 1234 1234 0 -1 4194560 100 0 0 0 5 6 0 0 20 0 11 0 4242 123 456 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0",
			want: 4242,
			ok:   true,
		},
		{name: "no name", stat: "1234 S 1 2 3", ok: false},
		{name: "too short", stat: "1234 (node) S 1 2 3", ok: false},
		{name: "empty", stat: "", ok: false},
		{name: "not a number", stat: "1234 (node) S 1 1234 1234 0 -1 4194560 100 0 0 0 5 6 0 0 20 0 11 0 abc 123 456 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0", ok: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := parseStartTicks(testCase.stat)
			if ok != testCase.ok || got != testCase.want {
				t.Fatalf("parseStartTicks = (%d, %v), want (%d, %v)", got, ok, testCase.want, testCase.ok)
			}
		})
	}
}

// TestStartTimeFromProcReadsThisProcess pins that the kernel path answers for a
// process that plainly exists, and that the value is plausible.
func TestStartTimeFromProcReadsThisProcess(t *testing.T) {
	startedAt, ok := startTimeFromProc(os.Getpid())
	if !ok {
		t.Skip("/proc is unavailable in this environment")
	}
	now := time.Now().Unix()
	if startedAt <= 0 || startedAt > now+60 {
		t.Fatalf("startedAt = %d, which is not a plausible start time (now = %d)", startedAt, now)
	}
	// This test process started seconds ago at most.
	if now-startedAt > 3600 {
		t.Fatalf("startedAt = %d is more than an hour before now = %d", startedAt, now)
	}
}

// TestStartTimeFromProcRejectsNonsense pins that an impossible pid yields no
// fingerprint instead of a wrong one.
func TestStartTimeFromProcRejectsNonsense(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if _, ok := startTimeFromProc(pid); ok {
			t.Fatalf("startTimeFromProc(%d) reported a fingerprint", pid)
		}
	}
}

// TestInspectPrefersProcOverPS pins which of the two answers wins on Linux.
//
// /proc needs no external tool and works in a container that ships no ps, so it
// is the authoritative fingerprint and ps only supplies the command line. The
// stub ps reports a process that started ten hours ago; a reading that agrees
// with it means the kernel value was ignored, and every ownership decision that
// rests on the fingerprint would then be comparing a coarse number against a
// precise one.
func TestInspectPrefersProcOverPS(t *testing.T) {
	dir := t.TempDir()
	// A stub that answers the way ps would for a process started ten hours ago.
	ps := stubTool(t, dir, "ps", "10:00:00 node --import tsx/esm apps/cli/src/bin.ts web --port 3080\n")
	host := &Host{tools: testTools, lookPath: func(name string) (string, error) {
		if name == "ps" {
			return ps, nil
		}
		return "", errTestLookup
	}}

	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatal("this process must be reported as alive")
	}
	if facts.Source != "proc" {
		t.Fatalf("Source = %q, want proc: the kernel read comes first on Linux", facts.Source)
	}
	now := time.Now().Unix()
	if facts.StartedAt <= 0 || now-facts.StartedAt > 3600 {
		t.Fatalf("StartedAt = %d (now = %d), want the kernel start time, not ps's elapsed column",
			facts.StartedAt, now)
	}
	if facts.Command == "" {
		t.Fatal("ps must still supply the command line for diagnostics")
	}
}
