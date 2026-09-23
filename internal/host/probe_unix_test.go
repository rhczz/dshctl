//go:build unix

package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This file pins the Unix-only behaviour that probe_test.go does not reach: what
// a ps line means, what happens when a probe tool exists but cannot be run, and
// how the probe chain in Listening combines an answer with a failure.
//
// Everything here is either a pure parser or a stub, so nothing in it reads the
// machine it runs on. The tests were verified to behave identically on darwin and
// are written so that the Linux /proc path asserts its own expectations rather
// than being assumed absent.

// TestParsePSFactsReadsOnePSLine pins the shape of a `ps -o etime= -o args=` row.
//
// ps is the only source of process facts on a Unix without /proc, and it is the
// only source of the command line everywhere, so a row that is read wrongly
// produces a wrong start time or a wrong command — the two things the state
// record is compared against.
func TestParsePSFactsReadsOnePSLine(t *testing.T) {
	cases := []struct {
		name        string
		output      string
		wantCommand string
		wantElapsed time.Duration
		wantStarted bool
		wantZombie  bool
	}{
		{
			name:        "ordinary row",
			output:      "01:02:03 node --import tsx/esm apps/cli/src/bin.ts web --port 3080\n",
			wantCommand: "node --import tsx/esm apps/cli/src/bin.ts web --port 3080",
			wantElapsed: time.Hour + 2*time.Minute + 3*time.Second,
			wantStarted: true,
		},
		{
			name:        "short elapsed form",
			output:      "00:10 node\n",
			wantCommand: "node",
			wantElapsed: 10 * time.Second,
			wantStarted: true,
		},
		{
			// ps prints an empty argument column for a process whose argv it may
			// not read; that is an empty command, not a parse failure.
			name:        "no command column",
			output:      "00:10\n",
			wantCommand: "",
			wantElapsed: 10 * time.Second,
			wantStarted: true,
		},
		{
			name:        "trailing space after the elapsed column",
			output:      "00:10 \n",
			wantCommand: "",
			wantElapsed: 10 * time.Second,
			wantStarted: true,
		},
		{
			// A header can only appear if the caller asked for a column without the
			// `=` suffix, and the call site deliberately uses `etime=`/`args=` to
			// suppress it. If one appears anyway, the elapsed column is unreadable
			// and no start time may be invented from it.
			name:        "header line",
			output:      "ELAPSED ARGS\n",
			wantCommand: "ARGS",
			wantStarted: false,
		},
		{
			name:        "header before the row",
			output:      "ELAPSED ARGS\n00:10 node\n",
			wantCommand: "ARGS",
			wantStarted: false,
		},
		{
			// Only the first line is read: the query names one pid, so a second line
			// belongs to some other process and must never be mixed into this one's
			// facts.
			name:        "only the first line is used",
			output:      "00:10 first\n00:20 second\n",
			wantCommand: "first",
			wantElapsed: 10 * time.Second,
			wantStarted: true,
		},
		{
			name:        "empty output",
			output:      "",
			wantCommand: "",
			wantStarted: false,
		},
		{
			name:        "blank line",
			output:      "\n",
			wantCommand: "",
			wantStarted: false,
		},
		{
			// A zombie's argument column carries the marker; that is the only
			// evidence ps offers that a pid which still answers signal 0 has in
			// fact exited.
			name:        "zombie marker",
			output:      "00:10 node <defunct>\n",
			wantCommand: "node <defunct>",
			wantElapsed: 10 * time.Second,
			wantStarted: true,
			wantZombie:  true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parsePSFacts(testCase.output)
			if got.command != testCase.wantCommand {
				t.Errorf("command = %q, want %q", got.command, testCase.wantCommand)
			}
			if got.zombie != testCase.wantZombie {
				t.Errorf("zombie = %v, want %v", got.zombie, testCase.wantZombie)
			}
			if !testCase.wantStarted {
				if got.startedAt != 0 {
					t.Errorf("startedAt = %d, want 0: the elapsed column could not be read", got.startedAt)
				}
				return
			}
			if got.startedAt == 0 {
				t.Fatalf("startedAt = 0, want a start time %s before now", testCase.wantElapsed)
			}
			// The start time is derived from the elapsed column, so it must sit the
			// reported age before now and nowhere else.
			age := time.Duration(timeNow()-got.startedAt) * time.Second
			if age < testCase.wantElapsed-time.Second || age > testCase.wantElapsed+time.Second {
				t.Errorf("startedAt is %s before now, want about %s", age, testCase.wantElapsed)
			}
		})
	}
}

// TestIsDefunctRecognisesTheZombieMarker pins the positive half of the zombie
// check: the marker both ps dialects print for a process that has exited.
//
// A zombie still answers signal 0, so this marker is the only thing that keeps a
// dead server from being reported as a live one — and a server reported as live is
// one that never gets restarted.
func TestIsDefunctRecognisesTheZombieMarker(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"bsd and gnu marker after the name", "node <defunct>", true},
		{"bare marker", "<defunct>", true},
		{"bracketed name", "[node] <defunct>", true},
		{"ordinary command", "node --import tsx/esm apps/cli/src/bin.ts web --port 3080", false},
		{"empty command", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefunct(testCase.command); got != testCase.want {
				t.Fatalf("isDefunct(%q) = %v, want %v", testCase.command, got, testCase.want)
			}
		})
	}
}

// TestParsePSFactsDoesNotCallALiveProcessAZombie pins that only the marker means
// "zombie", not the word appearing anywhere in a command line.
//
// The cost of this confusion is asymmetric and severe: Inspect turns a zombie into
// Alive == false, so calling a live process a zombie reports a running server as
// gone. A process whose *arguments* merely mention the word — a grep for a log
// line, an editor with a file named defunct, a build tool with a --defunct flag —
// is exactly as alive as any other, and the reader must not end it on paper. Only
// the <defunct> marker ps prints in place of the command counts.
func TestParsePSFactsDoesNotCallALiveProcessAZombie(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{name: "the word as an argument", command: "grep defunct /var/log/x"},
		{name: "the word as a word", command: "defunct"},
		{name: "the word inside a flag", command: "node --check-defunct src/x.ts"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			facts := parsePSFacts("00:10 " + testCase.command + "\n")
			if facts.zombie {
				t.Errorf("parsePSFacts(%q).zombie = true, want false: %q is a live process whose arguments contain the word",
					"00:10 "+testCase.command+"\n", testCase.command)
			}
		})
	}
}

// TestParseNetstatListenerKeepsRowsAfterAnOversizedLine pins what one unreadable
// row must not cost the netstat fallback.
//
// The walk drops a line longer than 4 MiB (probe.go, scanLineBytes) and starts
// over at the next one. Stopping there instead would drop every row after it, and
// "no listener row found" is netstat's final verdict that the port is free — so a
// single oversized line (a truncated write, a corrupt table dump) would make an
// occupied port look free, which is the one mistake that puts a second server on
// the same port.
func TestParseNetstatListenerKeepsRowsAfterAnOversizedLine(t *testing.T) {
	// The cap scanLines sets: scanLineBytes.
	const tokenCap = 4 * 1024 * 1024
	cases := []struct {
		name string
		size int
	}{
		{name: "just under the cap", size: tokenCap - 1},
		{name: "over the cap", size: tokenCap},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output := strings.Repeat("x", testCase.size) + "\ntcp4 0 0 127.0.0.1.3080 *.* LISTEN\n"
			if !parseNetstatListener(output, 3080) {
				t.Errorf("parseNetstatListener(<%d-byte line>+\"<listener row>\", 3080) = false, want true: a line that cannot be read must not hide the listener after it",
					testCase.size)
			}
		})
	}
}

// TestRunCaptureReportsAToolThatCannotBeExecuted pins the difference between a
// tool that answered "no" and a tool that never ran.
//
// A file without an execute bit is what a broken installation, a wrong
// architecture or a restrictive mount looks like. The exit status is then
// meaningless, so runCapture reports -1 and an error instead of inventing a
// status, and every caller that sees the error must refuse to conclude anything
// about the process or the port: "I could not look" is never "it is gone" or
// "it is free".
func TestRunCaptureReportsAToolThatCannotBeExecuted(t *testing.T) {
	dir := t.TempDir()
	// No execute bit on purpose.
	unrunnable := filepath.Join(dir, "lsof")
	if err := os.WriteFile(unrunnable, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("write the unrunnable tool: %v", err)
	}

	output, status, err := runCapture(context.Background(), unrunnable, "-nP", "-ti", "tcp:3080")
	if err == nil {
		t.Fatalf("runCapture = (%q, %d, nil), want an error for a tool that cannot be executed", output, status)
	}
	if status != -1 {
		t.Fatalf("runCapture status = %d, want -1: no exit status exists for a tool that never ran", status)
	}
	if !strings.Contains(err.Error(), "could not be executed") {
		t.Errorf("error = %q, want it to name the failure to execute", err)
	}

	lookPath := func(name string) (string, error) {
		if name == "lsof" {
			return unrunnable, nil
		}
		return "", errTestLookup
	}

	// Inspect: the kernel still knows the process exists, so a ps that cannot run
	// must not turn into "the server is gone".
	host := &Host{tools: testTools, lookPath: lookPath}
	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatalf("Inspect = %+v, want a live process: a tool that cannot be run says nothing", facts)
	}
	if facts.Command != "" {
		t.Fatalf("Inspect.Command = %q, want nothing invented by an unusable tool", facts.Command)
	}
	if !host.Alive(context.Background(), os.Getpid()) {
		t.Fatal("the process must still be alive after Inspect")
	}

	// Listening: every probe exists but none can run, so the port state is
	// unknown. It must be an error — never a silent "free".
	listeningHost := NewWithLookPath(lookPath, testTools)
	result, err := listeningHost.Listening(context.Background(), 3080)
	if err == nil {
		t.Fatalf("Listening = %+v, nil, want an error when no probe could be executed", result)
	}
	if result.Listening {
		t.Fatalf("Listening = %+v, want nothing when no probe could look", result)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("error = %v, want an execution failure: a probe was installed but could not run, which is not an unsupported platform", err)
	}
	if !strings.Contains(err.Error(), unrunnable) {
		t.Errorf("error = %q, want it to name the probe that could not be executed", err)
	}
}

// TestInspectKeepsTheProcessWhenPSExitsNonZero pins that a ps refusal is not a
// death certificate.
//
// ps exits non-zero when it was asked about a pid it will not describe: the pid may
// have exited, but the tool may also have been denied, and the two are
// indistinguishable from the status alone. Existence is therefore re-confirmed with
// the kernel, and no fact ps did not supply — a command line, a start time — may be
// invented to fill the gap.
func TestInspectKeepsTheProcessWhenPSExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	ps := stubScript(t, dir, "ps", "#!/bin/sh\nexit 1\n")
	host := &Host{tools: testTools, lookPath: func(name string) (string, error) {
		if name == "ps" {
			return ps, nil
		}
		return "", errTestLookup
	}}

	ctx := context.Background()
	pid := os.Getpid()
	// Which source answers depends on whether this Unix has /proc; asking the same
	// function the implementation asks keeps the expectation honest on both.
	_, hasProc := startTimeFromProc(pid)

	facts := host.Inspect(ctx, pid)
	if !facts.Alive {
		t.Fatalf("Inspect = %+v, want a live process: ps declining to describe a pid is not proof that it is gone", facts)
	}
	if facts.Command != "" {
		t.Errorf("Command = %q, want nothing: the unhelpful tool supplied none", facts.Command)
	}
	if hasProc {
		if facts.Source != "proc" {
			t.Errorf("Source = %q, want proc: the kernel read does not depend on ps", facts.Source)
		}
		if facts.StartedAt > time.Now().Unix() {
			t.Errorf("StartedAt = %d is in the future", facts.StartedAt)
		}
		return
	}
	if facts.Source != "signal" {
		t.Errorf("Source = %q, want signal: no kernel read exists and ps refused", facts.Source)
	}
	if facts.StartedAt != 0 {
		t.Errorf("StartedAt = %d, want 0: ps exited non-zero, so it reported no elapsed time", facts.StartedAt)
	}
}

// TestListeningKeepsGoingWhenAFailingProbeIsFollowedByAnAnswer pins that a probe
// failing early does not veto a later probe's answer.
//
// The chain exists because the tools differ in what they can see: lsof names owners
// on every Unix but may refuse a query, ss reads the whole table on Linux. A start
// that treated the first failure as final would refuse to see a listener that a
// perfectly good probe had already found — and the port would then be handed to a
// second server.
func TestListeningKeepsGoingWhenAFailingProbeIsFollowedByAnAnswer(t *testing.T) {
	dir := t.TempDir()
	lsof := stubScript(t, dir, "lsof", "#!/bin/sh\nexit 2\n")
	ss := stubTool(t, dir, "ss", "LISTEN 0 511 127.0.0.1:3080 0.0.0.0:* users:((\"node\",pid=48737,fd=23))\n")
	host := NewWithLookPath(func(name string) (string, error) {
		switch name {
		case "lsof":
			return lsof, nil
		case "ss":
			return ss, nil
		}
		return "", errTestLookup
	}, testTools)
	result, err := host.Listening(context.Background(), 3080)
	if err != nil {
		t.Fatalf("Listening: %v, want the listener the later probe reported", err)
	}
	if !result.Listening || result.PID != 48737 {
		t.Fatalf("Listening = %+v, want the listener ss reported", result)
	}
}

// TestListeningReportsEveryFailingProbe pins the failure message when no probe
// could answer.
//
// The caller decides what to do about an unknown port state — retry, warn, refuse
// to start — and that decision needs the evidence, so every tool that failed has
// to be named rather than replaced by a generic "unknown". The error must also stay
// distinct from ErrUnsupported: an unsupported platform is a permanent condition,
// while a tool that exited non-zero is something the operator can fix.
func TestListeningReportsEveryFailingProbe(t *testing.T) {
	dir := t.TempDir()
	lsof := stubScript(t, dir, "lsof", "#!/bin/sh\nexit 2\n")
	ss := stubScript(t, dir, "ss", "#!/bin/sh\nexit 2\n")
	host := NewWithLookPath(func(name string) (string, error) {
		switch name {
		case "lsof":
			return lsof, nil
		case "ss":
			return ss, nil
		}
		return "", errTestLookup
	}, testTools)
	_, err := host.Listening(context.Background(), 3080)
	if err == nil {
		t.Fatal("two probes that could not look must not be reported as a free port")
	}
	message := err.Error()
	for _, want := range []string{"lsof exited with status 2", "ss exited with status 2", "3080"} {
		if !strings.Contains(message, want) {
			t.Errorf("error = %q, want it to contain %q", message, want)
		}
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("error = %v, want an execution failure rather than ErrUnsupported: the tools are installed, they failed", err)
	}
}

// TestListenViaNetstatReportsItsExitStatus pins the last probe's classification.
//
// netstat is the only probe on a host without lsof and ss, and its negative verdict
// is final, so a non-zero status must never be read as "no listener": that would
// turn a tool that could not read the table into a decision that the port is free.
func TestListenViaNetstatReportsItsExitStatus(t *testing.T) {
	dir := t.TempDir()
	netstat := stubScript(t, dir, "netstat", "#!/bin/sh\nexit 2\n")
	host := NewWithLookPath(func(name string) (string, error) {
		if name == "netstat" {
			return netstat, nil
		}
		return "", errTestLookup
	}, testTools)
	result, handled, err := host.listenViaNetstat(context.Background(), 3080)
	if err == nil {
		t.Fatalf("listenViaNetstat = (%+v, %v, nil), want an error for a netstat that exited non-zero", result, handled)
	}
	if handled {
		t.Fatal("a probe that could not read the table must not give a final answer")
	}
	if !strings.Contains(err.Error(), "netstat exited with status 2") {
		t.Errorf("error = %q, want it to report the exit status", err)
	}
}
