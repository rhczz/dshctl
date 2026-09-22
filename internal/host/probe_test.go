//go:build unix

package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The pure parsers shared by every platform — parsePIDs, parseElapsed,
// parsePortAddress and the probe bound in host.go — are pinned in
// probe_parsers_test.go, which carries no build tag so that Windows runs them
// too. What remains here is the Unix-only half: the output dialects of the tools
// this platform actually ships.

// TestParseSSPID pins the pid embedded in ss process output.
//
// ss prints the owning pid inside a users:(("name",pid=N,fd=M)) list, which is the
// only attribution the Linux fallback can offer. The pid is read out of text, so
// what the reader accepts decides which process a later stop signals.
func TestParseSSPID(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{`LISTEN 0 511 127.0.0.1:3080 0.0.0.0:* users:(("node",pid=48737,fd=23))`, 48737},
		{`LISTEN 0 511 *:3080 *:*`, 0},
		{`LISTEN 0 511 *:3080 *:* users:(("node",pid=abc))`, 0},
		{`users:(("node",pid=`, 0},
		// A process can hold the port through several sockets, so the list can
		// repeat: the first entry is the documented answer, and it must be a real
		// entry rather than a mixture of the two.
		{`LISTEN 0 511 *:3080 *:* users:(("node",pid=48737,fd=23),("node",pid=51234,fd=24))`, 48737},
		// A pid that cannot be read is reported as "unknown", never as a number
		// assembled from a fragment: a negative pid names nothing, and a value too
		// large for an int cannot be signalled unambiguously.
		{`LISTEN 0 511 *:3080 *:* users:(("node",pid=-1,fd=3))`, 0},
		{`LISTEN 0 511 *:3080 *:* users:(("node",pid=99999999999999999999,fd=3))`, 0},
		// A backslash-escaped quote inside the name must not end the quoted
		// region early: the reader honours the escape, so the field after the
		// real closing quote is still the entry's own.
		{`LISTEN 0 511 *:3080 *:* users:(("node \"pid=9\"",pid=48737,fd=23))`, 48737},
		// A truncated line leaves the name unterminated, and then the rest of the
		// line cannot be told apart from the inside of a name: the pid is
		// reported as unknown rather than guessed at.
		{`LISTEN 0 511 *:3080 *:* users:(("node,pid=48737,fd=23)`, 0},
	}
	for _, testCase := range cases {
		if got := parseSSPID(testCase.line); got != testCase.want {
			t.Fatalf("parseSSPID(%q) = %d, want %d", testCase.line, got, testCase.want)
		}
	}
}

// TestParseSSPIDIgnoresANameThatLooksLikePid pins that the pid is taken from the
// entry's own `pid=` field and not from the first `pid=` anywhere in the line.
//
// ss prints a user list of users:(("name",pid=N,fd=M)) entries, and the program
// name is arbitrary: a process may be called "pid=9", or be started with a path
// that contains that text. Reading the first occurrence would attribute the
// socket to a pid that was never in the entry — and a stop that trusts it
// signals a process dshctl does not own. The reader therefore skips the quoted
// name and reads the field only where it appears outside it.
func TestParseSSPIDIgnoresANameThatLooksLikeAPid(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{
			name: "program name contains the field name",
			line: `LISTEN 0 4096 *:3080 *:* users:(("pid=9",pid=48737,fd=3))`,
			want: 48737,
		},
		{
			name: "path contains the field name",
			line: `LISTEN 0 4096 *:3080 *:* users:(("/opt/pid=7/bin/node",pid=51234,fd=9))`,
			want: 51234,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := parseSSPID(testCase.line); got != testCase.want {
				t.Errorf("parseSSPID(%q) = %d, want %d: the entry's own field carries the pid", testCase.line, got, testCase.want)
			}
		})
	}
}

// TestParseNetstatListener pins both output dialects.
//
// netstat is the last resort used when neither lsof nor ss exists, and on a host
// without those tools its verdict is the only thing between a start and a second
// server on the same port, so both the BSD and the Linux row layout have to be
// read correctly.
func TestParseNetstatListener(t *testing.T) {
	bsd := `Active Internet connections (including servers)
Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)
tcp4       0      0  127.0.0.1.3080         *.*                    LISTEN
tcp4       0      0  127.0.0.1.5432         *.*                    LISTEN
`
	linux := `Active Internet connections (servers and established)
Proto Recv-Q Send-Q Local Address           Foreign Address         State
tcp        0      0 127.0.0.1:3080          0.0.0.0:*               LISTEN
tcp        0      0 127.0.0.1:5432          0.0.0.0:*               LISTEN
`
	cases := []struct {
		name   string
		output string
		port   int
		want   bool
	}{
		{"bsd listening", bsd, 3080, true},
		{"bsd other port", bsd, 9999, false},
		{"linux listening", linux, 3080, true},
		{"linux other port", linux, 9999, false},
		{"empty", "", 3080, false},
		{"not listening", "tcp4 0 0 127.0.0.1.3080 1.2.3.4.5 ESTABLISHED\n", 3080, false},
		// An IPv6 listener names the same port after a dot, so the dot form has to
		// work on a row whose address is full of colons.
		{"bsd ipv6", "tcp6 0 0 ::1.3080 *.* LISTEN\n", 3080, true},
		{"bsd ipv6 other port", "tcp6 0 0 ::1.3080 *.* LISTEN\n", 3081, false},
		// macOS reports a dual-stack listener as tcp46; the protocol column is
		// matched by prefix, so the variant must not hide the row.
		{"bsd dual stack wildcard", "tcp46 0 0 *.* *.* LISTEN\n", 3080, false},
		{"bsd dual stack named port", "tcp46 0 0 *.3080 *.* LISTEN\n", 3080, true},
		// The state column is compared case-insensitively because the dialects
		// disagree on its spelling.
		{"lowercase listen", "tcp4 0 0 127.0.0.1.3080 *.* listen\n", 3080, true},
		{"listening", "tcp4 0 0 127.0.0.1.3080 *.* LISTENING\n", 3080, true},
		// Only the local address column may match: a port that appears only in the
		// foreign column belongs to the peer, and treating it as local would report
		// a port occupied because some unrelated connection mentions it.
		{"port only in the foreign column", "tcp4 0 0 127.0.0.1.5432 1.2.3.4.3080 ESTABLISHED\n", 3080, false},
		{"foreign port on a listening row", "tcp4 0 0 *.5432 *.* LISTEN\n", 3080, false},
		// Windows-style line endings reach the parser whenever the output was
		// captured or filtered somewhere.
		{"crlf", "tcp4 0 0 127.0.0.1.3080 *.* LISTEN\r\n", 3080, true},
		{"whitespace only", "   \n\t\n \r\n", 3080, false},
		// A tool that died mid-write leaves a truncated last line; a row without
		// the full column set cannot be read as a listener.
		{"truncated last line", "tcp4 0 0 127.0.0.1.3080", 3080, false},
		{"truncated to three columns", "tcp4 0 0", 3080, false},
		// Out-of-range ports are rejected before a probe runs (config.Validate
		// keeps a port in 1..65535), so the reader only compares the parsed number
		// with the one it was asked about.
		{"out of range port argument", "tcp4 0 0 127.0.0.1.70000 *.* LISTEN\n", 70000, true},
		{"out of range other port", "tcp4 0 0 127.0.0.1.70000 *.* LISTEN\n", 3080, false},
		// The protocol column is matched with a case-sensitive prefix because both
		// dialects print it in lower case; a row that shouts is not a row either
		// tool produces, and guessing at it would be inventing output.
		{"uppercase protocol", "TCP4 0 0 127.0.0.1.3080 *.* LISTEN\n", 3080, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			listening := parseNetstatListener(testCase.output, testCase.port)
			if listening != testCase.want {
				t.Fatalf("parseNetstatListener = %v, want %v", listening, testCase.want)
			}
		})
	}
}

// TestToolResolution pins that a probe reports itself as unavailable when its
// tool is missing, which is what makes a caller fail closed instead of
// concluding the port is free.
func TestToolResolution(t *testing.T) {
	missing := NewWithLookPath(func(string) (string, error) { return "", errTestLookup }, testTools)
	if _, ok := missing.tool("lsof"); ok {
		t.Fatal("a missing tool must not be reported as available")
	}
	present := NewWithLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil }, testTools)
	path, ok := present.tool("lsof")
	if !ok || path != "/usr/bin/lsof" {
		t.Fatalf("tool = (%q, %v), want the resolved path", path, ok)
	}
	if _, err := missing.Listening(context.Background(), 3080); err == nil {
		t.Fatal("with no probe tools available the port must be reported as unknown")
	}
}

// TestListenViaLsofClassifiesExitStatuses is the regression test for the
// classification that keeps "cannot look" from being read as "the port is
// free": only status 0 and status 1 ("nothing matched") are answers, and every
// other status — including a probe that was killed, which reports itself as
// status -1 — is an error.
func TestListenViaLsofClassifiesExitStatuses(t *testing.T) {
	cases := []struct {
		name    string
		script  string
		want    PortResult
		wantErr bool
	}{
		{
			name:    "nothing matched",
			script:  "#!/bin/sh\nexit 1\n",
			want:    PortResult{},
			wantErr: false,
		},
		{
			name:    "matched, no output",
			script:  "#!/bin/sh\nexit 0\n",
			want:    PortResult{},
			wantErr: false,
		},
		{
			name:    "listener",
			script:  "#!/bin/sh\necho 48737\nexit 0\n",
			want:    PortResult{Listening: true, PID: 48737},
			wantErr: false,
		},
		{
			name:    "real failure",
			script:  "#!/bin/sh\nexit 2\n",
			want:    PortResult{},
			wantErr: true,
		},
		{
			// A probe killed by its own timeout reports status -1. Reading that
			// as "nothing matched" made an occupied port look free, which is
			// the one mistake this package exists to prevent.
			name:    "probe killed",
			script:  "#!/bin/sh\nkill -KILL $$\n",
			want:    PortResult{},
			wantErr: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			lsof := filepath.Join(dir, "lsof")
			if err := os.WriteFile(lsof, []byte(testCase.script), 0o755); err != nil {
				t.Fatalf("write lsof stub: %v", err)
			}
			h := NewWithLookPath(func(name string) (string, error) {
				if name == "lsof" {
					return lsof, nil
				}
				return "", errTestLookup
			}, testTools)
			result, handled, err := h.listenViaLsof(context.Background(), 3080)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("listenViaLsof = (%+v, %v), want an error", result, handled)
				}
				return
			}
			if err != nil {
				t.Fatalf("listenViaLsof: %v", err)
			}
			if result.Listening != testCase.want.Listening || result.PID != testCase.want.PID {
				t.Fatalf("listenViaLsof = %+v, want %+v", result, testCase.want)
			}
			if result.Listening {
				if !handled {
					t.Fatal("a listener must be a final answer")
				}
				return
			}
			// "Nothing matched" is never final from lsof: it answers per query
			// and may omit sockets it is not allowed to inspect, so the chain
			// continues to the probes that read the whole table.
			if handled {
				t.Fatal("lsof's free-port verdict must not end the probe chain unconfirmed")
			}
		})
	}
}

// stubScript writes an executable that runs the given script verbatim.
func stubScript(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return path
}

// TestListeningConfirmsLsofWithTheWholeTableProbes pins the probe chain, not
// just the classification inside one probe: lsof's "nothing matched" must not
// end the search, because an unprivileged lsof silently omits sockets it may
// not inspect. A listener only ss can see has to be found.
func TestListeningConfirmsLsofWithTheWholeTableProbes(t *testing.T) {
	dir := t.TempDir()
	lsof := stubScript(t, dir, "lsof", "#!/bin/sh\nexit 1\n")
	ss := stubScript(t, dir, "ss", "#!/bin/sh\n"+
		"echo 'LISTEN 0 511 127.0.0.1:3080 0.0.0.0:* users:(\"node\",pid=48737,fd=23))'\n")
	h := NewWithLookPath(func(name string) (string, error) {
		switch name {
		case "lsof":
			return lsof, nil
		case "ss":
			return ss, nil
		}
		return "", errTestLookup
	}, testTools)
	result, err := h.Listening(context.Background(), 3080)
	if err != nil {
		t.Fatalf("Listening: %v", err)
	}
	if !result.Listening || result.PID != 48737 {
		t.Fatalf("Listening = %+v, want the listener only ss could see", result)
	}
}

// TestListeningAnswersFreeWhenOnlyLsofExists pins the documented downgrade: when
// no probe that reads the whole table is installed, lsof's word is the best
// evidence the host has, and reporting "unknown" for every free port would make
// the tool unusable there.
func TestListeningAnswersFreeWhenOnlyLsofExists(t *testing.T) {
	dir := t.TempDir()
	lsof := stubScript(t, dir, "lsof", "#!/bin/sh\nexit 1\n")
	h := NewWithLookPath(func(name string) (string, error) {
		if name == "lsof" {
			return lsof, nil
		}
		return "", errTestLookup
	}, testTools)
	result, err := h.Listening(context.Background(), 3080)
	if err != nil {
		t.Fatalf("a host whose only tool is lsof must still answer: %v", err)
	}
	if result.Listening {
		t.Fatalf("Listening = %+v, want a free port", result)
	}
}

// TestListeningReportsAnErrorWhenTheTableProbeFails pins the fail-closed half:
// lsof said nothing, the table-reading probe could not look, and nothing else is
// installed — that is "unknown", never "free".
func TestListeningReportsAnErrorWhenTheTableProbeFails(t *testing.T) {
	dir := t.TempDir()
	lsof := stubScript(t, dir, "lsof", "#!/bin/sh\nexit 1\n")
	ss := stubScript(t, dir, "ss", "#!/bin/sh\nexit 2\n")
	h := NewWithLookPath(func(name string) (string, error) {
		switch name {
		case "lsof":
			return lsof, nil
		case "ss":
			return ss, nil
		}
		return "", errTestLookup
	}, testTools)
	if _, err := h.Listening(context.Background(), 3080); err == nil {
		t.Fatal("an unconfirmable port must be reported as unknown, not as free")
	}
}

// TestTheInventoryOrderDecidesWhichProbeAnswers pins the caller's order as the
// authority: the first installed tool that can answer does, so the product that
// trusts lsof gets the owner it names even when a table-reading tool that cannot
// name anybody is installed beside it.
func TestTheInventoryOrderDecidesWhichProbeAnswers(t *testing.T) {
	dir := t.TempDir()
	// `lsof -ti` answers with the pid alone, and the probe asks it that way.
	lsof := stubTool(t, dir, "lsof", "1111\n")
	ss := stubTool(t, dir, "ss", "LISTEN 0 4096 127.0.0.1:3080 0.0.0.0:* users:(\"node\",pid=2222,fd=3)\n")
	lookPath := func(name string) (string, error) {
		switch name {
		case "lsof":
			return lsof, nil
		case "ss":
			return ss, nil
		}
		return "", errTestLookup
	}

	lsofFirst := NewWithLookPath(lookPath, Tools{Port: []string{"lsof", "ss"}, Process: "ps"})
	result, err := lsofFirst.Listening(context.Background(), 3080)
	if err != nil || result.PID != 1111 {
		t.Fatalf("lsof first: result = %+v, err = %v, want the pid lsof named", result, err)
	}

	ssFirst := NewWithLookPath(lookPath, Tools{Port: []string{"ss", "lsof"}, Process: "ps"})
	result, err = ssFirst.Listening(context.Background(), 3080)
	if err != nil || result.PID != 2222 {
		t.Fatalf("ss first: result = %+v, err = %v, want the pid ss named", result, err)
	}
}

// TestAnUnknownProbeNameIsReportedNotSkipped pins the fail-closed reading of a
// configuration mistake: a tool this build cannot parse makes the probe unable
// to look, which is never the same answer as "the port is free".
func TestAnUnknownProbeNameIsReportedNotSkipped(t *testing.T) {
	host := NewWithLookPath(func(string) (string, error) { return "/usr/bin/true", nil },
		Tools{Port: []string{"fuser"}, Process: "ps"})
	if _, err := host.Listening(context.Background(), 3080); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Listening = %v, want ErrUnsupported for an unreadable tool", err)
	}
}
