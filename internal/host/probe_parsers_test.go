package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The functions pinned here are the platform-neutral half of the package: the
// parsers in probe.go and the probe bound in host.go are compiled unchanged on
// every target, and the same tool output reaches them on every target. Their
// matrices therefore live in a file without a build tag: a parser expectation
// hidden behind `//go:build unix` is a parser nothing verifies on the platform
// whose output it will be fed.

// TestParsePIDs pins the lsof -t reader, including its tolerance of noise.
//
// The reader is the only thing standing between lsof's output and the pid a
// caller may later signal, so what it accepts and what it drops is a safety
// property: a row it invents becomes a signal aimed at the wrong process, and a
// row it drops makes an occupied port look free.
func TestParsePIDs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		limit int
		want  []int
	}{
		{"single", "48737\n", 1, []int{48737}},
		{"first of several", "48737\n51234\n", 1, []int{48737}},
		{"both of several", "48737\n51234\n", 2, []int{48737, 51234}},
		{"empty", "", 1, nil},
		{"noise", "not-a-pid\n42\n", 1, []int{42}},
		{"zero is not a pid", "0\n7\n", 2, []int{7}},
		{"negative is not a pid", "-1\n7\n", 2, []int{7}},
		{"leading spaces", "  42  \n", 1, []int{42}},
		// lsof prints one pid per line and repeats a pid when the process holds
		// several matching sockets, so duplicates carry information: collapsing
		// them would hide that the tool matched more than one row.
		{"duplicate pids are kept", "42\n42\n", 2, []int{42, 42}},
		// A number too large for an int cannot name a process on any platform.
		// Dropping it must not drop the rows after it.
		{"overflowing pid is skipped", "99999999999999999999\n7\n", 2, []int{7}},
		// Tools differ on line endings and on how much whitespace they pad with.
		{"crlf", "42\r\n7\r\n", 2, []int{42, 7}},
		{"tabs", "\t42\t\n7\n", 2, []int{42, 7}},
		{"whitespace-only line", "   \n7\n", 2, []int{7}},
		// Only the first field is read, which is what lets a wider row (a ps or
		// netstat line) be tolerated instead of parsed as one long number.
		{"first field of a wider row", "42 43\n", 1, []int{42}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parsePIDs(testCase.input, testCase.limit)
			if len(got) != len(testCase.want) {
				t.Fatalf("parsePIDs(%q, %d) = %v, want %v", testCase.input, testCase.limit, got, testCase.want)
			}
			for index := range testCase.want {
				if got[index] != testCase.want[index] {
					t.Fatalf("parsePIDs(%q, %d) = %v, want %v", testCase.input, testCase.limit, got, testCase.want)
				}
			}
		})
	}
}

// TestParsePIDsNeverReturnsMoreThanTheLimit pins the "at most limit" reading of
// the limit parameter.
//
// A caller that asks for no pids must not be handed one: "at most 0" is zero, not
// "one, then stop". The parameter is an upper bound on how many process ids the
// caller is willing to consider, and a reader that overshoots it silently hands
// the caller an owner it never asked about — which on the stop path is a pid that
// gets signalled. The only production caller passes 1 today, so the bound is
// pinned where it is cheapest to get wrong: zero and below.
func TestParsePIDsNeverReturnsMoreThanTheLimit(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		limit int
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := parsePIDs("42\n7\n", testCase.limit)
			// "At most limit" with a negative limit can only mean zero pids, since
			// no count is below a negative bound.
			want := testCase.limit
			if want < 0 {
				want = 0
			}
			if len(got) > want {
				t.Errorf("parsePIDs(%q, %d) = %v, want at most %d pids: the limit is an upper bound, not a budget for one row",
					"42\n7\n", testCase.limit, got, want)
			}
		})
	}
}

// TestParsePIDsKeepsReadingAfterAnOversizedLine pins what one unreadable row
// must not cost.
//
// The walk hands its callback lines of at most 4 MiB (probe.go, scanLineBytes).
// A line that exceeds the cap is dropped, but the walk has to resynchronize at
// the next line rather than stop: everything after an oversized row — a
// truncated write, a corrupt file, a tool that dumped a binary blob — would
// otherwise be lost, and a lost row is indistinguishable from a tool that
// printed nothing, which is exactly how an occupied port comes to look free.
func TestParsePIDsKeepsReadingAfterAnOversizedLine(t *testing.T) {
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
			output := strings.Repeat("x", testCase.size) + "\n42\n"
			got := parsePIDs(output, 1)
			if len(got) != 1 || got[0] != 42 {
				t.Errorf("parsePIDs(<%d-byte line>+\"\n42\n\", 1) = %v, want [42]: a line that cannot be read must not hide the rows after it",
					testCase.size, got)
			}
		})
	}
}

// TestParseElapsed pins the elapsed-time reader for both ps dialects.
//
// ps reports how long a process has been running rather than when it started, so
// this conversion is the fingerprint that decides whether a recorded server is
// still the process the record describes.
func TestParseElapsed(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
		ok    bool
	}{
		{"05:30", 5*time.Minute + 30*time.Second, true},
		{"01:05:30", time.Hour + 5*time.Minute + 30*time.Second, true},
		{"2-01:05:30", 2*24*time.Hour + time.Hour + 5*time.Minute + 30*time.Second, true},
		{"00:00", 0, true},
		{" 03:04 ", 3*time.Minute + 4*time.Second, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1:2:3:4", 0, false},
		{"1:99", 0, false},
		{"x-01:00", 0, false},
		// A component may not carry a sign: "-1:00" is read as "day count -1" and
		// then fails on the malformed remainder, and "00:-5" is rejected the same
		// way, so a negative elapsed time cannot come out of the reader.
		{"-1:00", 0, false},
		{"00:-5", 0, false},
		// The largest whole-day value that fits in a time.Duration (about 292
		// years) is still accepted with its exact value, which is the boundary the
		// overflow test below must not overstep.
		{"106751-00:00:00", 2562024 * time.Hour, true},
		// 100000 days fits as well: the audit that requested these cases listed it
		// as an overflow, but time.Duration holds about 106751 days.
		{"100000-00:00:00", 2400000 * time.Hour, true},
	}
	for _, testCase := range cases {
		got, ok := parseElapsed(testCase.input)
		if ok != testCase.ok || got != testCase.want {
			t.Fatalf("parseElapsed(%q) = (%s, %v), want (%s, %v)",
				testCase.input, got, ok, testCase.want, testCase.ok)
		}
	}
}

// TestParseElapsedRejectsADurationThatCannotBeRepresented pins the rows where a
// value must be refused instead of wrapped.
//
// The reader multiplies the parsed fields into a time.Duration, and Duration
// arithmetic is unchecked: a field large enough to overflow would wrap into a
// negative number. A negative elapsed time makes parsePSFacts compute a start
// time in the future, so the fingerprint that decides whether a recorded pid is
// still the server it described becomes a number no process can match —
// ownership would then fail open or closed for reasons that have nothing to do
// with the process. The values are reachable only from a damaged or hostile ps,
// which is exactly why the reader must not trust them.
func TestParseElapsedRejectsADurationThatCannotBeRepresented(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "days overflow", input: "9223372036854775807-00:00:00"},
		{name: "days overflow into a negative duration", input: "106752-00:00:00"},
		{name: "hours overflow", input: "9999999999999:00:00"},
		{name: "hours overflow into a negative duration", input: "292471208677:00:00"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := parseElapsed(testCase.input)
			if ok {
				t.Errorf("parseElapsed(%q) = (%s, true), want ok == false: the value does not fit in a time.Duration and has wrapped",
					testCase.input, got)
			}
		})
	}
}

// TestParsePortAddress pins the address forms netstat and ss print.
//
// A false positive here makes a free port look occupied and a false negative makes
// an occupied port look free, so every form a real tool emits has to be recognised
// and every field that does not name the port has to be refused.
func TestParsePortAddress(t *testing.T) {
	cases := []struct {
		field string
		port  int
		want  bool
	}{
		{"127.0.0.1:3080", 3080, true},
		{"*:3080", 3080, true},
		{"[::]:3080", 3080, true},
		{"0.0.0.0:3080", 3080, true},
		{"127.0.0.1.3080", 3080, false},
		{"127.0.0.1:3081", 3080, false},
		{"127.0.0.1:", 3080, false},
		{"3080", 3080, false},
		{"", 3080, false},
		// The port follows the last colon, so an IPv6 scope id in the host part
		// cannot shift what is compared.
		{"[fe80::1%eth0]:3080", 3080, true},
		// A wildcard port has no number to compare against.
		{"*:*", 3080, false},
		{"[::]:*", 3080, false},
		// A NUL is not a digit, so a truncated or padded field does not match.
		{"127.0.0.1:3080\x00", 3080, false},
		// The comparison is on the port text only: bytes that are invalid UTF-8 in
		// the host leave the numeric suffix intact, and bytes inside the suffix
		// make it unparsable.
		{"127.0.0.1\xff:3080", 3080, true},
		{"127.0.0.1:308\xff0", 3080, false},
		// A bare number without the separator is not an address field, even though
		// the BSD dot form is handled by addressEndsWithPort, not here.
		{"+3080", 3080, false},
		{"03080", 3080, false},
		{"::1.3080", 3080, false},
		// Atoi reads a sign and leading zeros, so a field that literally ends in
		// "+3080" or "03080" matches 3080. No tool prints those forms, and the two
		// readings can only differ in the safe direction: a spurious match makes a
		// port look occupied, never free.
		{"127.0.0.1:+3080", 3080, true},
		{"127.0.0.1:03080", 3080, true},
		// Out-of-range and zero ports are not this function's policy: config.Validate
		// keeps a port in 1..65535 before any probe runs (config.go MinPort/MaxPort),
		// so the reader only answers whether the text equals the number it was asked
		// about. A negative port can never match a printed address because no tool
		// prints a negative port.
		{"127.0.0.1:70000", 70000, true},
		{"127.0.0.1:70000", 3080, false},
		{"127.0.0.1:0", 0, true},
		{"127.0.0.1:0", 3080, false},
		{"127.0.0.1:3080", -1, false},
	}
	for _, testCase := range cases {
		if got := parsePortAddress(testCase.field, testCase.port); got != testCase.want {
			t.Errorf("parsePortAddress(%q, %d) = %v, want %v", testCase.field, testCase.port, got, testCase.want)
		}
	}
}

// TestProbeContextIsBounded pins that a probe cannot hang an operation.
func TestProbeContextIsBounded(t *testing.T) {
	ctx, cancel := probeContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a probe without a deadline must get one")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > probeTimeout {
		t.Fatalf("the probe deadline is %s away, want it within (0, %s]", remaining, probeTimeout)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("a fresh probe context is already done: %v", err)
	}
}

// TestProbeContextTreatsNilAsBackground pins the panic guard: a caller that
// passed no context at all must get a working bound, not a nil dereference.
func TestProbeContextTreatsNilAsBackground(t *testing.T) {
	ctx, cancel := probeContext(context.TODO())
	defer cancel()
	if ctx == nil {
		t.Fatal("probeContext(nil) returned a nil context")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("a nil context must still be bounded")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("probeContext(nil).Err() = %v, want nil", err)
	}
}

// TestProbeContextKeepsTheCallersDeadline pins that the caller's bound is not
// widened.
//
// The probe bound exists to stop a hung tool from hanging dshctl, so it must never
// hand a probe more time than the caller allowed: a shutdown that has two seconds
// left must not spend ten in a probe. The value is kept exactly rather than
// re-derived, so a caller that is already counting down still gets its own
// deadline.
func TestProbeContextKeepsTheCallersDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelParent()
	parentDeadline, _ := parent.Deadline()

	ctx, cancel := probeContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the derived context lost the deadline the caller set")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %s, want the caller's %s", deadline, parentDeadline)
	}
}

// TestProbeContextLeavesALongerCallersDeadlineAlone documents the other half of
// the same rule: the bound is only supplied when the caller supplied none.
//
// A caller that allowed an hour keeps an hour, so probeTimeout does not cap every
// tool invocation — it caps only the ones whose caller expressed no bound of its
// own. That is what the code does deliberately (it returns a cancel-only context
// whenever a deadline exists), and it is asserted here so the decision is visible:
// if probeTimeout is ever meant to bound a probe unconditionally, this is the
// expectation that has to change.
func TestProbeContextLeavesALongerCallersDeadlineAlone(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), time.Hour)
	defer cancelParent()
	parentDeadline, _ := parent.Deadline()

	ctx, cancel := probeContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the derived context lost the deadline the caller set")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %s, want the caller's %s: a longer caller deadline is not shortened to %s",
			deadline, parentDeadline, probeTimeout)
	}
}

// TestProbeContextKeepsACancelledContextCancelled pins that a probe cannot run
// after its caller gave up.
//
// An operation that has been cancelled — a shutdown, a signal — must not start a
// tool process that keeps running after the command returned. The derived context
// therefore has to inherit the cancellation rather than replace it with a fresh
// bound.
func TestProbeContextKeepsACancelledContextCancelled(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	ctx, cancel := probeContext(parent)
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("probeContext(cancelled).Err() = %v, want context.Canceled", ctx.Err())
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the derived context is not done although its parent was cancelled")
	}
}
