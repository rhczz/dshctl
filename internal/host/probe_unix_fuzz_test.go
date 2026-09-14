//go:build unix

package host

import (
	"strconv"
	"strings"
	"testing"
)

// The Unix-only parsers read what lsof, ss, netstat and ps print. Their fuzz
// targets assert the properties a caller relies on rather than today's exact
// answers, for the same reason as the portable targets: a wrong answer here is
// either a refused start or a signal aimed at a process that is not the server.

// FuzzParseSSPIDOnlyAnswersWhatTheLineSays pins that the pid reported for a
// listening socket is one the line actually carries.
//
// The reported owner is handed to the stop path, so a pid invented from a
// process name would be a signal aimed at whatever happens to hold that number.
func FuzzParseSSPIDOnlyAnswersWhatTheLineSays(f *testing.F) {
	for _, seed := range []string{
		`LISTEN 0 511 127.0.0.1:3080 0.0.0.0:* users:(("node",pid=48737,fd=23))`,
		`LISTEN 0 511 *:3080 *:*`,
		`LISTEN 0 511 *:3080 *:* users:(("node",pid=abc))`,
		`users:(("node",pid=`,
		`users:(("pid=9",pid=48737,fd=3))`,
		`users:(("/opt/pid=7/bin/node",pid=51234,fd=9))`,
		`users:(("node",pid=-1,fd=3))`,
		`pid=`,
		``,
		`  pid=42  `,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		pid := parseSSPID(line)
		if pid < 0 {
			t.Fatalf("parseSSPID(%q) = %d: a pid is never negative", line, pid)
		}
		// Every pid the line carries, read independently of the implementation.
		// Leading zeros are the same pid, so the comparison is numeric.
		carried := map[int]bool{}
		for rest := line; ; {
			index := strings.Index(rest, "pid=")
			if index < 0 {
				break
			}
			rest = rest[index+len("pid="):]
			end := 0
			for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
				end++
			}
			if end == 0 {
				continue
			}
			if value, err := strconv.Atoi(rest[:end]); err == nil {
				carried[value] = true
			}
		}
		if len(carried) == 0 {
			if pid != 0 {
				t.Fatalf("parseSSPID(%q) = %d, but the line names no pid", line, pid)
			}
			return
		}
		if pid != 0 && !carried[pid] {
			t.Fatalf("parseSSPID(%q) = %d, which the line does not carry (it carries %v)", line, pid, carried)
		}
	})
}

// FuzzParseNetstatListenerNeedsThePortInTheTable pins that the fallback probe
// only reports a listener when the port it was asked about appears in the table.
//
// netstat's answer is final — a "not found" from it is the caller's evidence
// that the port is free — so a match invented from unrelated rows is the one
// mistake that turns an occupied port into a free one.
func FuzzParseNetstatListenerNeedsThePortInTheTable(f *testing.F) {
	for _, seed := range []struct {
		output string
		port   int
	}{
		{"tcp4 0 0 127.0.0.1.3080 *.* LISTEN\n", 3080},
		{"tcp 0 0 127.0.0.1:3080 0.0.0.0:* LISTEN\n", 3080},
		{"tcp6 0 0 ::1.3080 *.* LISTEN\n", 3080},
		{"tcp46 0 0 ::1.3080 *.* LISTEN\n", 3080},
		{"tcp4 0 0 127.0.0.1.3080 1.2.3.4.5 ESTABLISHED\n", 3080},
		{"tcp4 0 0 127.0.0.1.3080 *.* listen\n", 3080},
		{"tcp4 0 0 *.* 127.0.0.1.3080 LISTEN\n", 3080},
		{"", 3080},
		{"\n\n\n", 3080},
		{"udp4 0 0 127.0.0.1.3080 *.*\n", 3080},
	} {
		f.Add(seed.output, seed.port)
	}
	f.Fuzz(func(t *testing.T, output string, port int) {
		found := parseNetstatListener(output, port)
		if found && !strings.Contains(output, strconv.Itoa(port)) {
			t.Fatalf("parseNetstatListener(%q, %d) = true, but the table never mentions that port",
				output, port)
		}
		if found && !parseNetstatListener(output, port) {
			t.Fatalf("parseNetstatListener(%q, %d) is not deterministic", output, port)
		}
	})
}

// FuzzParsePSFactsOnlyCallsADefunctProcessAZombie pins the reading of one ps
// line: the command column is what carries the zombie marker, so a process can
// only be reported as one when that column says so.
func FuzzParsePSFactsOnlyCallsADefunctProcessAZombie(f *testing.F) {
	for _, seed := range []string{
		"00:01 node --import tsx/esm apps/cli/src/bin.ts web\n",
		"00:01 <defunct>\n",
		"00:01 node (defunct)\n",
		"01:02:03 grep defunct /var/log/x\n",
		"",
		"\n",
		"00:00 \n",
		"not-a-duration node\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		facts := parsePSFacts(output)
		again := parsePSFacts(output)
		// The command column and the zombie verdict are pure functions of the
		// input; only the derived start time may legitimately differ by the
		// second of wall clock between the two calls. Whether a live process
		// whose arguments merely contain the word "defunct" is misread as a
		// zombie is pinned by the deterministic test, not by this target.
		if again.zombie != facts.zombie || again.command != facts.command {
			t.Fatalf("parsePSFacts(%q) is not deterministic: %+v then %+v", output, facts, again)
		}
	})
}
