package logfile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAllMatchesReportsWhenTheWindowMissedTheMatch pins the third answer
// AllMatches can give, next to "here are the matches" and "there are none".
//
// The scan only walks the trailing maxMatchBytes of a log, so on a large file
// the absence of a match is a fact about the window, not about the file. The
// service reads a server address this way and has to tell "this log holds no
// address" — where waiting longer cannot help — from "the address is older than
// what was searched", where it should say so instead of claiming there is none.
func TestAllMatchesReportsWhenTheWindowMissedTheMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	pattern := regexp.MustCompile(`(?m)^address: (\S+)$`)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The only matching line sits at the very beginning, far outside the
	// window the scan will read.
	if _, err := file.WriteString("address: http://127.0.0.1:3080/?token=older-than-the-window\n"); err != nil {
		_ = file.Close()
		t.Fatalf("write match: %v", err)
	}
	// Filler with no chance of matching, taken past the window.
	block := strings.Repeat(strings.Repeat("y", 4095)+"\n", 64)
	for written := int64(0); written < maxMatchBytes+int64(len(block)); written += int64(len(block)) {
		if _, err := file.WriteString(block); err != nil {
			_ = file.Close()
			t.Fatalf("write filler: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	matches, truncated, err := AllMatches(path, pattern)
	if err != nil {
		t.Fatalf("AllMatches: %v", err)
	}
	if !truncated {
		t.Fatal("a file larger than the scan window must report a truncated window")
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %v, want none inside the window", matches)
	}
}

// TestStreamFromReadsAShorterReplacementFromItsBeginning pins what a position
// means when the file it described is gone.
//
// The caller's position says "I have already printed this much of the log in
// front of you". When the log is replaced by a shorter one, that position is
// past the end of the new file, and it describes content the follower never
// read: continuing from it would skip the head of the replacement — exactly the
// lines a rotation is meant to preserve — while reading from the beginning is
// the only answer that loses nothing.
func TestStreamFromReadsAShorterReplacementFromItsBeginning(t *testing.T) {
	handoff := int64(500)
	first := strings.Repeat("A", 1000)
	replacement := strings.Repeat("B", 120)
	cases := []struct {
		name  string
		write func(t *testing.T, path, content string)
	}{
		{
			name: "replaced by rename",
			write: func(t *testing.T, path, content string) {
				temp := path + ".replacement"
				if err := os.WriteFile(temp, []byte(content), 0o600); err != nil {
					t.Fatalf("write replacement: %v", err)
				}
				if err := os.Rename(temp, path); err != nil {
					t.Fatalf("rename: %v", err)
				}
			},
		},
		{
			name: "truncated in place",
			write: func(t *testing.T, path, content string) {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatalf("rewrite: %v", err)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dsh-web.log")
			logger := New(path, 0)
			logger.SetPollInterval(5 * time.Millisecond)
			if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}

			sink := &syncBuffer{}
			ctx, cancel := newCancelContext()
			done := make(chan error, 1)
			go func() { done <- logger.StreamFrom(ctx, sink, handoff) }()
			waitForText(t, sink, strings.Repeat("A", 500))

			testCase.write(t, path, replacement)
			waitForText(t, sink, replacement)

			cancel()
			<-done
			if got := sink.String(); got != strings.Repeat("A", 500)+replacement {
				t.Fatalf("the follower printed %d bytes, want the handoff half and the whole replacement (%d bytes)",
					len(got), len(strings.Repeat("A", 500))+len(replacement))
			}
		})
	}
}

// failingWriter refuses every write and counts the attempts.
type failingWriter struct {
	err   error
	calls int
}

// Write implements io.Writer.
func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

// TestStreamStopsWhenTheWriterFails pins that a broken destination ends the
// follow with the writer's own error.
//
// `dshctl logs --follow` writes to a pipe the operator may close at any moment,
// and follow loops forever by design. A write error that is swallowed turns that
// loop into a busy spin against a destination that will never work again, so the
// error has to travel back to the caller and the follow has to stop.
func TestStreamStopsWhenTheWriterFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("A", 200)), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	logger := New(path, 0)
	logger.SetPollInterval(5 * time.Millisecond)

	writeErr := errors.New("the destination refused the write")
	sink := &failingWriter{err: writeErr}
	done := make(chan error, 1)
	go func() { done <- logger.StreamFrom(context.Background(), sink, 50) }()

	select {
	case err := <-done:
		if !errors.Is(err, writeErr) {
			t.Fatalf("StreamFrom = %v, want the writer's error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the follow kept running after its writer failed")
	}
	if sink.calls != 1 {
		t.Fatalf("the writer was called %d times, want 1: a failed write must end the follow", sink.calls)
	}
}

// TestOpenAppendCreatesTheLogPath pins that writing the log creates the
// directory it belongs to, with the modes the rest of the state uses.
//
// The log lives in the state directory, which exists on a machine that has run
// dshctl before and does not on a fresh one. A first command that fails because
// its own directory is missing would look like a broken installation.
func TestOpenAppendCreatesTheLogPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "deeper", "dsh-web.log")
	logger := New(path, 0)

	handle, err := logger.OpenAppend()
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat log directory: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("log directory mode = %v, want a directory", dirInfo.Mode())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if !fileInfo.Mode().IsRegular() {
		t.Fatalf("log path is not a regular file: %v", fileInfo.Mode())
	}
	// The 0700/0600 permission bits are a Unix concept: Windows reports 0777
	// for a directory and 0666 for a writable file whatever was asked for.
	if runtime.GOOS != "windows" {
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("log directory mode = %v, want 0700", dirInfo.Mode())
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("log mode = %v, want 0600", fileInfo.Mode())
		}
	}
}

// TestRotationTriggersOnlyAboveTheThreshold pins the exact boundary of
// RotateBytes: the size that equals the threshold is not over it.
//
// RotateBytes is documented as "the size at which the file rolls", and an
// operator who sets it to a log's expected size would otherwise get a rotation
// on every write that reaches it — and, because rotation copies before it
// truncates, a backup of a file that never grew past its limit.
func TestRotationTriggersOnlyAboveTheThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	content := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	logger := New(path, int64(len(content)))

	rotated, err := logger.RotateIfNeeded()
	if err != nil {
		t.Fatalf("RotateIfNeeded: %v", err)
	}
	if rotated {
		t.Fatal("a log exactly at the threshold must not rotate")
	}
	if got := readFileOrEmpty(t, path); got != string(content) {
		t.Fatalf("live log changed at the threshold (%d bytes)", len(got))
	}
	if _, err := os.Stat(logger.BackupPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a backup was written for a log at the threshold: %v", err)
	}

	// One byte more is past the threshold.
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := handle.Write([]byte("y")); err != nil {
		_ = handle.Close()
		t.Fatalf("append: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	rotated, err = logger.RotateIfNeeded()
	if err != nil {
		t.Fatalf("RotateIfNeeded: %v", err)
	}
	if !rotated {
		t.Fatal("a log one byte over the threshold must rotate")
	}
	if got := readFileOrEmpty(t, path); got != "" {
		t.Fatalf("live log = %q, want it emptied by the rotation", got)
	}
	if backup := readFileOrEmpty(t, logger.BackupPath()); !strings.Contains(backup, string(content)) {
		t.Fatalf("backup holds %d bytes, want the previous generation", len(backup))
	}
}

// TestExistsAndSizeOnADirectoryAtTheLogPath pins the two accessors against a
// path that is not a file.
//
// Exists applies the regular-file test, so it answers false. Size does not: it
// reports the directory's own size with no error, which is why Size is only
// meaningful after Exists returned true — the caller in the service does exactly
// that, and this test records the pair's disagreement so nobody starts treating
// a bare Size as proof that a log is there.
func TestExistsAndSizeOnADirectoryAtTheLogPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logdir")
	if err := os.MkdirAll(filepath.Join(dir, "keep"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logger := New(dir, 0)

	if logger.Exists() {
		t.Fatal("a directory must not be reported as an existing log file")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	size, err := logger.Size()
	if err != nil {
		t.Fatalf("Size reported an error for a directory: %v", err)
	}
	if size != info.Size() {
		t.Fatalf("Size = %d, want the directory's own size %d", size, info.Size())
	}
}

// TestSectionRoundTripsATitleContainingAnEqualsSign pins that "=" in a section
// title is legal and survives the marker format.
//
// Titles name the run a section belongs to, and "=" is the natural separator for
// a qualified name such as env=prod. The marker's title field is non-whitespace
// text, so the character is carried verbatim; what must not happen is a title
// that Section accepts and ParseSection cannot read back, because the section
// would then be invisible to LastSection and to every consumer of the log.
func TestSectionRoundTripsATitleContainingAnEqualsSign(t *testing.T) {
	titles := []string{"a=b", "=", "=build", "build=", "a==b", "release=1.2.3", "env=prod,region=eu"}
	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			if err := ValidateTitle(title); err != nil {
				t.Fatalf("ValidateTitle(%q) = %v, want it accepted", title, err)
			}
			logger := newLogger(t)
			if err := logger.Section(title); err != nil {
				t.Fatalf("Section(%q): %v", title, err)
			}
			body := "body of " + title
			if err := logger.Line(body); err != nil {
				t.Fatalf("Line: %v", err)
			}

			lines := strings.Split(strings.TrimRight(readFileOrEmpty(t, logger.Path), "\n"), "\n")
			if len(lines) != 3 {
				t.Fatalf("log holds %d lines (%q), want a blank line, a marker and a body", len(lines), lines)
			}
			marker := lines[len(lines)-2]
			got, ok := ParseSection(marker)
			if !ok || got != title {
				t.Fatalf("ParseSection(%q) = (%q, %v), want (%q, true)", marker, got, ok, title)
			}
			sections, outcome, err := LastSection(logger.Path, []string{title})
			if err != nil {
				t.Fatalf("LastSection: %v", err)
			}
			if outcome != Found || strings.Join(sections, "\n") != body {
				t.Fatalf("LastSection = (%#v, %d, %v), want the section body %q", sections, outcome, err, body)
			}
		})
	}
}
