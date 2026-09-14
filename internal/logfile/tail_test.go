package logfile

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLines creates a log whose lines are numbered, so a missing or duplicated
// line can be named rather than merely counted.
func writeNumberedLog(t *testing.T, total, width int) (string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	lines := make([]string, 0, total)
	var builder strings.Builder
	for index := 0; index < total; index++ {
		line := fmt.Sprintf("line-%06d", index)
		if pad := width - len(line) - 1; pad > 0 {
			line += strings.Repeat("-", pad)
		}
		lines = append(lines, line)
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return path, lines
}

// TestTailFromPrintsExactlyTheRequestedLineCount is the property `dshctl logs`
// and `dshctl logs --follow` rest on: "the last N lines" means N lines.
//
// It is checked across the size where the reader stops reading forwards and
// starts reading backwards in chunks, because that switch is where a tail either
// keeps its contract or quietly drops a line. The file is only a few hundred
// kilobytes: the behaviour does not depend on the file being huge, only on it
// being larger than one read chunk.
func TestTailFromPrintsExactlyTheRequestedLineCount(t *testing.T) {
	cases := []struct {
		name      string
		total     int
		width     int
		requested int
	}{
		{"small file", 40, 12, 5},
		{"small file, everything", 40, 12, 500},
		{"one chunk exactly", 5461, 12, 5},
		{"more than one chunk", 20000, 6, 5},
		{"more than one chunk, a large request", 20000, 6, 200},
		{"several chunks", 50000, 12, 200},
		{"several chunks, one line", 50000, 12, 1},
		{"several chunks, everything", 50000, 12, 60000},
		{"long lines, few of them", 300, 4096, 7},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path, lines := writeNumberedLog(t, testCase.total, testCase.width)

			want := testCase.requested
			if want > len(lines) {
				want = len(lines)
			}
			wantText := strings.Join(lines[len(lines)-want:], "\n") + "\n"

			var buffer bytes.Buffer
			written, position, err := TailFrom(path, testCase.requested, &buffer)
			if err != nil {
				t.Fatalf("TailFrom: %v", err)
			}
			if written != want {
				t.Fatalf("TailFrom(%d) wrote %d lines, want %d\nfirst line: %q",
					testCase.requested, written, want, firstLine(buffer.String()))
			}
			if buffer.String() != wantText {
				t.Fatalf("TailFrom(%d) printed %q, want the last %d lines",
					testCase.requested, firstLine(buffer.String()), want)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("stat: %v", statErr)
			}
			if position != info.Size() {
				t.Fatalf("TailFrom(%d) stopped at %d, want the end of the file (%d)",
					testCase.requested, position, info.Size())
			}

			// Tail is the same read without the position, and it must agree.
			buffer.Reset()
			written, err = Tail(path, testCase.requested, &buffer)
			if err != nil {
				t.Fatalf("Tail: %v", err)
			}
			if written != want || buffer.String() != wantText {
				t.Fatalf("Tail(%d) wrote %d lines (%q), want %d", testCase.requested, written, firstLine(buffer.String()), want)
			}
		})
	}
}

// firstLine renders the first line of output for a failure message.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// TestTailLinesAreContiguousAndOrdered pins that a tail is the tail: no line is
// duplicated, dropped in the middle, or printed out of order.
//
// The count assertion above would still pass if the reader returned the right
// number of the wrong lines, which is exactly what a chunk that is assembled in
// the wrong order produces.
func TestTailLinesAreContiguousAndOrdered(t *testing.T) {
	path, lines := writeNumberedLog(t, 30000, 12)

	var buffer bytes.Buffer
	written, _, err := TailFrom(path, 500, &buffer)
	if err != nil {
		t.Fatalf("TailFrom: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n")
	if written != 500 || len(got) != 500 {
		t.Fatalf("wrote %d lines, printed %d", written, len(got))
	}
	if got[0] != lines[len(lines)-500] || got[499] != lines[len(lines)-1] {
		t.Fatalf("tail starts at %q and ends at %q, want %q .. %q",
			got[0], got[499], lines[len(lines)-500], lines[len(lines)-1])
	}
	for index := 1; index < len(got); index++ {
		if got[index-1] == got[index] {
			t.Fatalf("line %q was printed twice at %d", got[index], index)
		}
	}
}

// TestTailFromPropertyOverManyFileShapes checks the same contract over a
// deterministic sweep of file sizes, line widths and requested counts.
//
// The interesting sizes are the ones that straddle a read chunk, and a sweep is
// what covers them all instead of the two or three a hand-written case can name.
// The seed is fixed, so a failure is reproducible.
func TestTailFromPropertyOverManyFileShapes(t *testing.T) {
	random := rand.New(rand.NewSource(20260203))
	for iteration := 0; iteration < 40; iteration++ {
		total := random.Intn(20000) + 1
		width := random.Intn(30) + 2
		requested := random.Intn(400) + 1
		// A width of 1 cannot hold the numbered prefix; the writer widens short
		// lines, so the requested shape is still honoured.
		path, lines := writeNumberedLog(t, total, width)

		want := requested
		if want > len(lines) {
			want = len(lines)
		}
		var buffer bytes.Buffer
		written, _, err := TailFrom(path, requested, &buffer)
		if err != nil {
			t.Fatalf("iteration %d (total=%d width=%d requested=%d): %v", iteration, total, width, requested, err)
		}
		if written != want {
			t.Fatalf("iteration %d (total=%d width=%d requested=%d): wrote %d lines, want %d",
				iteration, total, width, requested, written, want)
		}
		wantText := strings.Join(lines[len(lines)-want:], "\n") + "\n"
		if buffer.String() != wantText {
			t.Fatalf("iteration %d (total=%d width=%d requested=%d): printed %q, want %q",
				iteration, total, width, requested, firstLine(buffer.String()), firstLine(wantText))
		}
	}
}

// TestTailFromWithNoLinesAsksForNothing pins the documented meaning of a
// non-positive count: nothing is printed, and nothing is claimed about where a
// follow should continue from.
func TestTailFromWithNoLinesAsksForNothing(t *testing.T) {
	path, _ := writeNumberedLog(t, 100, 12)
	for _, requested := range []int{0, -1, -100} {
		var buffer bytes.Buffer
		written, position, err := TailFrom(path, requested, &buffer)
		if err != nil {
			t.Fatalf("TailFrom(%d): %v", requested, err)
		}
		if written != 0 || position != 0 || buffer.Len() != 0 {
			t.Fatalf("TailFrom(%d) = (%d, %d, %q), want nothing at all",
				requested, written, position, buffer.String())
		}
	}
}

// TestTailPreservesBytesItCannotInterpret pins that a log is bytes, not text:
// the tail must not mangle a line that holds a NUL or an invalid UTF-8 sequence,
// because a build writes whatever its tools wrote.
func TestTailPreservesBytesItCannotInterpret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.log")
	content := []byte{0xff, 0xfe, '\n', 0x00, 'x', '\n', 'o', 'k', '\n'}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buffer bytes.Buffer
	written, _, err := TailFrom(path, 3, &buffer)
	if err != nil {
		t.Fatalf("TailFrom: %v", err)
	}
	if written != 3 {
		t.Fatalf("wrote %d lines, want 3", written)
	}
	if !bytes.Equal(buffer.Bytes(), content) {
		t.Fatalf("TailFrom printed %q, want the bytes %q", buffer.Bytes(), content)
	}
}

// TestTailOfALineLongerThanTheWindowPrintsNothing pins the ceiling: a single
// line larger than the reader is willing to walk cannot be printed, and printing
// its tail would be worse than printing nothing, because half a line is
// indistinguishable from a whole one.
func TestTailOfALineLongerThanTheWindowPrintsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge-line.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// One line, longer than the 8 MiB window, with no newline at all.
	if _, err := file.WriteString(strings.Repeat("x", maxTailBytes+1024)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var buffer bytes.Buffer
	written, position, err := TailFrom(path, 5, &buffer)
	if err != nil {
		t.Fatalf("TailFrom: %v", err)
	}
	if written != 0 || buffer.Len() != 0 {
		t.Fatalf("TailFrom printed %d lines (%d bytes) of an unreadable line", written, buffer.Len())
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat: %v", statErr)
	}
	if position != info.Size() {
		t.Fatalf("position = %d, want the end of the file (%d)", position, info.Size())
	}
}

// TestTailReportsAPathItCannotRead pins that a file the process may not read is
// an error, never an empty answer: "no output" and "could not look" are the two
// things this codebase refuses to confuse.
func TestTailReportsAPathItCannotRead(t *testing.T) {
	// A directory at the path: stat succeeds, and reading it cannot produce
	// lines. Whatever the platform reports, it must be reported as an error.
	dir := t.TempDir()
	if _, err := Tail(dir, 5, &bytes.Buffer{}); err == nil {
		t.Fatal("a directory at the log path must be reported as an error")
	}
	if _, _, err := TailFrom(dir, 5, &bytes.Buffer{}); err == nil {
		t.Fatal("TailFrom must report the same failure")
	}
}
