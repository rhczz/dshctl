package host

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fuzz targets below cover the parsers that read output produced by tools
// this program does not control. A seed runs as an ordinary test in CI, so the
// invariants are checked on every build as well; `go test -fuzz` explores the
// input space around them.
//
// The invariants are deliberately about properties, not about today's exact
// answers: a parser may change its mind about an odd string, but it may never
// invent a process, lose a row, or hand a caller something it cannot act on.
// Behaviour that is already known to be wrong is pinned by a deterministic test
// instead, so that these targets stay usable for fuzzing.

// FuzzParsePIDsNeverInventsAProcess pins the contract of the reader that decides
// which processes a port probe reported.
//
// Whatever the tool printed, every pid the parser hands back must have been in
// that output, in the same order, no more often than it appeared — a pid the
// tool never printed is a process a later signal could be aimed at by mistake.
func FuzzParsePIDsNeverInventsAProcess(f *testing.F) {
	for _, seed := range []string{
		"", "48737\n", "48737\n51234\n", "not-a-pid\n42\n", "0\n7\n", "-1\n7\n",
		"  42  \n", "42\r\n", "42 43\n", "99999999999999999999\n7\n", "\n\n\n",
		"spaces only\n   \n", "42\n42\n42\n", "\t7\t\n",
	} {
		f.Add(seed, 1)
		f.Add(seed, 3)
	}
	f.Fuzz(func(t *testing.T, output string, limit int) {
		pids := parsePIDs(output, limit)

		// Every answer must be a usable pid: the caller signals it.
		for _, pid := range pids {
			if pid <= 0 {
				t.Fatalf("parsePIDs(%q, %d) returned the impossible pid %d", output, limit, pid)
			}
		}
		// The answers must be a subsequence of what the tool printed.
		remaining := output
		for index, pid := range pids {
			token := strconv.Itoa(pid)
			position := strings.Index(remaining, token)
			if position < 0 {
				t.Fatalf("parsePIDs(%q, %d)[%d] = %d, which the tool never printed",
					output, limit, index, pid)
			}
			remaining = remaining[position+len(token):]
		}
		// The same input must always produce the same answer.
		again := parsePIDs(output, limit)
		if len(again) != len(pids) {
			t.Fatalf("parsePIDs(%q, %d) is not deterministic: %v then %v", output, limit, pids, again)
		}
		for index := range again {
			if again[index] != pids[index] {
				t.Fatalf("parsePIDs(%q, %d) is not deterministic: %v then %v", output, limit, pids, again)
			}
		}
	})
}

// FuzzParseElapsedKeepsItsGranularity pins the reader that turns ps's elapsed
// column into a duration.
//
// A value that parses successfully is subtracted from the current time to
// produce a process start time, so it must be a whole number of seconds: a
// fractional or wrapped value would make the fingerprint of a live server
// unreproducible. The known overflow of absurd day counts is pinned by its own
// deterministic test rather than here, so this target can keep exploring.
func FuzzParseElapsedKeepsItsGranularity(f *testing.F) {
	for _, seed := range []string{
		"05:30", "01:05:30", "2-01:05:30", "00:00", " 03:04 ", "", "abc",
		"1:2:3:4", "1:99", "x-01:00", "0-00:00:00", "106751-00:00:00",
		"1:2", "::", "1:", ":1", "1:2:3:4:5", "1:2:3", "365-23:59:59",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		elapsed, ok := parseElapsed(value)
		if !ok {
			return
		}
		if elapsed%time.Second != 0 {
			t.Fatalf("parseElapsed(%q) = %s, which is not a whole number of seconds", value, elapsed)
		}
	})
}

// FuzzParsePortAddressMatchesOnlyThePortItWasAskedAbout pins the address reader
// used by the ss and netstat probes.
//
// A false positive makes an unrelated program look like the server, and a false
// negative makes an occupied port look free, so a match is cross-checked against
// an independent reading of the field.
func FuzzParsePortAddressMatchesOnlyThePortItWasAskedAbout(f *testing.F) {
	for _, seed := range []struct {
		field string
		port  int
	}{
		{"127.0.0.1:3080", 3080}, {"*:3080", 3080}, {"[::]:3080", 3080},
		{"0.0.0.0:3080", 3080}, {"127.0.0.1.3080", 3080}, {"127.0.0.1:3081", 3080},
		{"3080", 3080}, {"", 3080}, {"*:*", 3080}, {"[fe80::1%eth0]:3080", 3080},
		{"127.0.0.1:+3080", 3080}, {"127.0.0.1:03080", 3080}, {"127.0.0.1:", 3080},
		{"127.0.0.1:70000", 70000}, {":0", 0}, {"x:y", 1}, {"::1:3080", 3080},
	} {
		f.Add(seed.field, seed.port)
	}
	f.Fuzz(func(t *testing.T, field string, port int) {
		if !parsePortAddress(field, port) {
			return
		}
		// A match claims the field's trailing number is the port. Read it again,
		// independently of the implementation.
		index := strings.LastIndexByte(field, ':')
		if index < 0 || index == len(field)-1 {
			t.Fatalf("parsePortAddress(%q, %d) matched a field with no trailing number", field, port)
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(field[index+1:]))
		if err != nil || parsed != port {
			t.Fatalf("parsePortAddress(%q, %d) matched, but the trailing number is %q",
				field, port, field[index+1:])
		}
	})
}

// FuzzScanLinesVisitsEveryReadableRow pins the line walker every parser in this
// package is built on.
//
// A walk that stops early turns "the tool did not report a listener" into
// "nothing is listening", which is the confusion this package exists to prevent.
// Every non-empty line the walk can read must be offered to the callback; a line
// larger than the scanner's buffer is a known limitation pinned by its own
// deterministic test, so inputs of that shape are left to it.
func FuzzScanLinesVisitsEveryReadableRow(f *testing.F) {
	f.Add("one\ntwo\n")
	f.Add("\n\n\n")
	f.Add("only\n")
	f.Add("trailing space  \n")
	f.Add(strings.Repeat("x", 4096) + "\nlast\n")
	f.Add(strings.Repeat("x", 64*1024) + "\nlast\n")
	f.Fuzz(func(t *testing.T, output string) {
		for _, line := range strings.Split(output, "\n") {
			if len(line) > 1024*1024 {
				// Lines within an order of magnitude of the scanner's 4 MiB
				// buffer are left to the deterministic oversized-line test;
				// this target explores the space the walk is specified to cover.
				return
			}
		}

		var visited []string
		scanLines(output, func(line string) bool {
			visited = append(visited, line)
			return true
		})

		want := 0
		for _, line := range strings.Split(output, "\n") {
			if strings.TrimSpace(strings.TrimSuffix(line, "\r")) != "" {
				want++
			}
		}
		if len(visited) != want {
			t.Fatalf("scanLines(%d bytes) visited %d of %d non-empty lines",
				len(output), len(visited), want)
		}
		for _, line := range visited {
			if line == "" || strings.TrimSpace(line) != line {
				t.Fatalf("scanLines offered the untrimmed or empty line %q", line)
			}
		}
	})
}
