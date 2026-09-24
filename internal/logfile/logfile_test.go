package logfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// newLogger returns a logger over a fresh file with rotation disabled.
func newLogger(t *testing.T) *Logger {
	t.Helper()
	logger := New(filepath.Join(t.TempDir(), "dsh-web.log"), 0, testFormat)
	logger.Now = func() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }
	return logger
}

// readFileOrEmpty returns a file's contents, or an empty string when it is gone.
func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestRotationKeepsTheRunningWritersDescriptor is the regression test for log
// rotation stranding the detached server: the server holds an append handle for
// its whole life, so rotation must truncate the same file instead of renaming
// it. A rename would send every later server line into the backup, and the next
// rotation would delete them.
func TestRotationKeepsTheRunningWritersDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")

	// The server opens the live log once, exactly as start hands it the handle.
	server, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open server handle: %v", err)
	}
	defer server.Close()
	if _, err := server.WriteString("server: before rotation\n"); err != nil {
		t.Fatalf("server write: %v", err)
	}

	logger := New(path, 64, testFormat)
	for index := 0; index < 10; index++ {
		if err := logger.Line("dshctl filler line to exceed the threshold"); err != nil {
			t.Fatalf("filler: %v", err)
		}
	}
	rotated, err := logger.RotateIfNeeded()
	if err != nil {
		t.Fatalf("RotateIfNeeded: %v", err)
	}
	if !rotated {
		t.Fatalf("expected rotation above the threshold; live=%q", readFileOrEmpty(t, path))
	}

	// The server keeps writing through the handle it already holds.
	if _, err := server.WriteString("server: after rotation\n"); err != nil {
		t.Fatalf("server write after rotation: %v", err)
	}
	live := readFileOrEmpty(t, path)
	if !strings.Contains(live, "server: after rotation") {
		t.Fatalf("live log = %q, want the post-rotation server line", live)
	}
	backup := readFileOrEmpty(t, path+".old")
	if !strings.Contains(backup, "server: before rotation") {
		t.Fatalf("backup = %q, want the pre-rotation content", backup)
	}
	if strings.Contains(backup, "server: after rotation") {
		t.Fatalf("backup = %q, want the running server's later output to stay live", backup)
	}

	// Two more rotations must not lose the live file's own history either.
	for round := 0; round < 2; round++ {
		for index := 0; index < 10; index++ {
			if err := logger.Line("more filler to grow the log again"); err != nil {
				t.Fatalf("filler round %d: %v", round, err)
			}
		}
		if _, err := logger.RotateIfNeeded(); err != nil {
			t.Fatalf("rotate round %d: %v", round, err)
		}
	}
	if _, err := server.WriteString("server: third generation\n"); err != nil {
		t.Fatalf("server write: %v", err)
	}
	if !strings.Contains(readFileOrEmpty(t, path), "server: third generation") {
		t.Fatal("the running server's output stopped reaching the live log")
	}
}

// TestRotationRefusesADirectory pins that a log path pointing at a directory is
// reported instead of being renamed away.
func TestRotationRefusesADirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "logdir")
	if err := os.MkdirAll(filepath.Join(target, "keep"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logger := New(target, 1, testFormat)
	if _, err := logger.RotateIfNeeded(); err == nil {
		t.Fatal("rotating a directory must fail")
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatalf("the directory was modified: %v", err)
	}
}

// TestRotationIsDisabledByZero pins the documented switch.
func TestRotationIsDisabledByZero(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "dsh-web.log"), 0, testFormat)
	if err := logger.Line(strings.Repeat("x", 4096)); err != nil {
		t.Fatalf("Line: %v", err)
	}
	rotated, err := logger.RotateIfNeeded()
	if err != nil || rotated {
		t.Fatalf("RotateIfNeeded = (%v, %v), want no rotation", rotated, err)
	}
}

// TestRotationLeavesTheLogIntactWhenTheBackupCannotBeWritten pins that a failed
// rotation costs history rather than the log itself.
func TestRotationLeavesTheLogIntactWhenTheBackupCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")
	logger := New(path, 16, testFormat)
	if err := logger.Line("important build output that must survive"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	// A directory where the backup belongs makes the copy fail.
	if err := os.MkdirAll(path+".old", 0o755); err != nil {
		t.Fatalf("seed backup dir: %v", err)
	}
	if _, err := logger.RotateIfNeeded(); err == nil {
		t.Fatal("expected the rotation to fail")
	}
	if !strings.Contains(readFileOrEmpty(t, path), "important build output") {
		t.Fatal("a failed rotation destroyed the live log")
	}
}

// TestSectionAndLine pins the marker format and the newline handling that keeps
// concurrent writers from gluing lines together.
func TestSectionAndLine(t *testing.T) {
	logger := newLogger(t)
	if err := logger.Section("build"); err != nil {
		t.Fatalf("Section: %v", err)
	}
	if err := logger.Line("first"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	// A partial line from a child process must not swallow the next record.
	if err := os.WriteFile(logger.Path, append([]byte(readFileOrEmpty(t, logger.Path)), []byte("partial")...), 0o600); err != nil {
		t.Fatalf("seed partial line: %v", err)
	}
	if err := logger.Line("dshctl note"); err != nil {
		t.Fatalf("Line: %v", err)
	}

	text := readFileOrEmpty(t, logger.Path)
	if !strings.Contains(text, "===== 2026-02-03 04:05:06 dshctl build =====") {
		t.Fatalf("marker missing or malformed:\n%s", text)
	}
	if !strings.Contains(text, "partial\ndshctl note\n") {
		t.Fatalf("a record was glued onto a partial line:\n%q", text)
	}
}

// TestSectionRejectsATitleTheMarkerCannotCarry pins that a title with a space is
// refused instead of producing a marker that cannot be parsed back.
func TestSectionRejectsATitleTheMarkerCannotCarry(t *testing.T) {
	logger := newLogger(t)
	if err := logger.Section("build retry"); err == nil {
		t.Fatal("a title with whitespace must be rejected")
	}
	if err := logger.Section(""); err == nil {
		t.Fatal("an empty title must be rejected")
	}
}

// TestParseSectionIsStrict pins that ordinary log lines can never be mistaken
// for a section boundary.
func TestParseSectionIsStrict(t *testing.T) {
	cases := []struct {
		line  string
		title string
		ok    bool
	}{
		{"===== 2026-02-03 04:05:06 dshctl build =====", "build", true},
		{"===== 2026-02-03 04:05:06 dshctl update =====", "update", true},
		{"plain log line", "", false},
		{"===== a b dshctl deploy =====", "", false},
		{"===== 2026-02-03 04:05:06 dshctl build =====", "build", true},
		{"===== 2026-02-03 04:05:06 other build =====", "", false},
		{"===== 2026-02-03 04:05:06 dshctl build ====", "", false},
		{"prefix ===== 2026-02-03 04:05:06 dshctl build =====", "", false},
		{"===== 2026-02-03 04:05:06 dshctl build ====== extra", "", false},
	}
	for _, testCase := range cases {
		title, ok := testFormat.Section(testCase.line)
		if ok != testCase.ok || title != testCase.title {
			t.Fatalf("testFormat.Section(%q) = (%q, %v), want (%q, %v)",
				testCase.line, title, ok, testCase.title, testCase.ok)
		}
	}
}

// TestSectionReadsAByteExactForgeryAsABoundary pins the marker contract's known
// limit: the format carries no writer identity, so a line that is byte for byte
// a valid marker is a section boundary whoever wrote it — the child process's
// output goes through the same file. The strict parse exists for lines that
// only look like a marker; telling the writers of an append-only log apart is
// not something a line can answer, and this pins the limit so a future
// hardening argues with the record instead of discovering it.
func TestSectionReadsAByteExactForgeryAsABoundary(t *testing.T) {
	marker := strings.TrimSpace(testFormat.Marker("build", time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)))
	title, ok := testFormat.Section(marker)
	if !ok || title != "build" {
		t.Fatalf("Section(%q) = (%q, %v), want the exact marker read as a boundary", marker, title, ok)
	}
}

// TestParseSectionToleratesCarriageReturns pins Windows-written logs.
func TestParseSectionToleratesCarriageReturns(t *testing.T) {
	title, ok := testFormat.Section("===== 2026-02-03 04:05:06 dshctl build =====\r")
	if !ok || title != "build" {
		t.Fatalf("ParseSection with CR = (%q, %v), want (build, true)", title, ok)
	}
}

// TestLastSectionReturnsTheLastMatchingBody pins the extraction of a run.
func TestLastSectionReturnsTheLastMatchingBody(t *testing.T) {
	logger := newLogger(t)
	for _, step := range []struct{ title, body string }{
		{"start", "server output"},
		{"build", "first build output"},
		{"start", "server output again"},
		{"build", "second build output"},
	} {
		if err := logger.Section(step.title); err != nil {
			t.Fatalf("Section: %v", err)
		}
		if err := logger.Line(step.body); err != nil {
			t.Fatalf("Line: %v", err)
		}
	}

	body, outcome, err := LastSection(logger.Path, testFormat, []string{"build"})
	if err != nil {
		t.Fatalf("LastSection: %v", err)
	}
	if outcome != Found {
		t.Fatalf("outcome = %d, want Found", outcome)
	}
	if strings.Join(body, "\n") != "second build output" {
		t.Fatalf("body = %#v, want the last build body", body)
	}

	// A section still open at end of file is returned as well.
	if err := logger.Section("update"); err != nil {
		t.Fatalf("Section: %v", err)
	}
	if err := logger.Line("update in progress"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	body, outcome, err = LastSection(logger.Path, testFormat, []string{"update"})
	if err != nil || outcome != Found || strings.Join(body, "\n") != "update in progress" {
		t.Fatalf("LastSection = (%#v, %d, %v), want the open update section", body, outcome, err)
	}
}

// TestLastSectionDistinguishesMissingFromTruncated pins the honest answer when a
// section is larger than the scan window.
func TestLastSectionDistinguishesMissingFromTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	logger := New(path, 0, testFormat)
	if err := logger.Section("build"); err != nil {
		t.Fatalf("Section: %v", err)
	}
	// A section larger than the scan window.
	chunk := strings.Repeat("y", 64<<10)
	for written := 0; written < maxScanBytes+(4<<20); written += len(chunk) {
		if err := logger.Line(chunk); err != nil {
			t.Fatalf("Line: %v", err)
		}
	}

	_, outcome, err := LastSection(path, testFormat, []string{"build"})
	if err != nil {
		t.Fatalf("LastSection: %v", err)
	}
	if outcome == NotFound {
		t.Fatal("a section that exists but is out of the window was reported as missing")
	}
	if outcome != Truncated {
		t.Fatalf("outcome = %d, want Truncated", outcome)
	}
}

// TestLastSectionMissingFileIsNotAnError pins the first-run behaviour.
func TestLastSectionMissingFileIsNotAnError(t *testing.T) {
	body, outcome, err := LastSection(filepath.Join(t.TempDir(), "absent"), testFormat, []string{"build"})
	if err != nil {
		t.Fatalf("LastSection: %v", err)
	}
	if outcome != NotFound || body != nil {
		t.Fatalf("LastSection = (%#v, %d), want (nil, NotFound)", body, outcome)
	}
}

// TestTail pins the trailing-line reader, including the partial first line.
func TestTail(t *testing.T) {
	logger := newLogger(t)
	for _, line := range []string{"one", "two", "three", "four"} {
		if err := logger.Line(line); err != nil {
			t.Fatalf("Line: %v", err)
		}
	}

	var buffer bytes.Buffer
	written, err := Tail(logger.Path, 2, &buffer)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if written != 2 || buffer.String() != "three\nfour\n" {
		t.Fatalf("Tail wrote %d lines: %q", written, buffer.String())
	}

	buffer.Reset()
	written, err = Tail(logger.Path, 10, &buffer)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if written != 4 || buffer.String() != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("Tail wrote %d lines: %q", written, buffer.String())
	}

	// A missing file prints nothing and is not an error.
	buffer.Reset()
	if _, err := Tail(filepath.Join(t.TempDir(), "absent"), 5, &buffer); err != nil {
		t.Fatalf("a missing log must not fail: %v", err)
	}
	if buffer.Len() != 0 {
		t.Fatalf("missing log produced output: %q", buffer.String())
	}

	// Lying about the line count is not an error either.
	if _, err := Tail(logger.Path, 0, &buffer); err != nil {
		t.Fatalf("Tail(0): %v", err)
	}
}

// TestTailDropsAPartialFirstLine pins that a window cut mid-file never prints
// half a line.
func TestTailDropsAPartialFirstLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	var builder strings.Builder
	for index := 0; index < 40000; index++ {
		builder.WriteString(strings.Repeat("z", 200))
		builder.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buffer bytes.Buffer
	if _, err := Tail(path, 5, &buffer); err != nil {
		t.Fatalf("Tail: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n") {
		if len(line) != 200 {
			t.Fatalf("line of length %d printed: %.40q", len(line), line)
		}
	}
}

// TestTailHandlesCarriageReturns pins Windows-written logs.
func TestTailHandlesCarriageReturns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.log")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buffer bytes.Buffer
	if _, err := Tail(path, 10, &buffer); err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if buffer.String() != "alpha\nbeta\n" {
		t.Fatalf("Tail = %q, want the CR stripped", buffer.String())
	}
}

// TestAllMatches pins pattern extraction, including the truncation fact a
// caller needs to tell "the file holds no such line" from "the line is older
// than the scan window".
func TestAllMatches(t *testing.T) {
	logger := newLogger(t)
	if err := logger.Line("dsh web: http://127.0.0.1:3080/?token=first"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	if err := logger.Line("dsh web: opening the default browser; pass --no-open to disable"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	if err := logger.Line("dsh web: http://127.0.0.1:3080/?token=second"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	pattern := regexp.MustCompile(`(?m)^dsh web:[ \t]+(https?://\S+)[ \t]*$`)

	matches, truncated, err := AllMatches(logger.Path, pattern)
	if err != nil {
		t.Fatalf("AllMatches: %v", err)
	}
	if truncated {
		t.Fatal("a small file must not report a truncated window")
	}
	if len(matches) != 2 || matches[0] != "http://127.0.0.1:3080/?token=first" ||
		matches[1] != "http://127.0.0.1:3080/?token=second" {
		t.Fatalf("AllMatches = %v, want both addresses oldest first", matches)
	}

	// A diagnostic that merely mentions a URL is not the server's address.
	other := newLogger(t)
	if err := other.Line("see https://example.com/help for details"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	if matches, _, err := AllMatches(other.Path, pattern); err != nil || len(matches) != 0 {
		t.Fatalf("AllMatches = (%v, %v), want no match for an unrelated URL", matches, err)
	}

	if matches, _, err := AllMatches(filepath.Join(t.TempDir(), "absent"), pattern); err != nil || len(matches) != 0 {
		t.Fatalf("a missing file must report no match: %v %v", matches, err)
	}
}

// TestStreamFollowsNewContentAcrossRotation pins tail -F behaviour across the
// copy-truncate rotation this package performs.
func TestStreamFollowsNewContentAcrossRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	logger := New(path, 0, testFormat)
	logger.SetPollInterval(5 * time.Millisecond)
	if err := logger.Line("history"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	attached := awaitSettled(t, logger)
	go func() { done <- logger.Stream(ctx, sink) }()
	// An existing file is followed from its end, so the follower has to have
	// attached before the line it must observe is written.
	attached()

	if err := logger.Line("first"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	waitForText(t, sink, "first")

	// Rotation truncates in place: the follower must start over from the
	// beginning of the new content.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := logger.Line("second"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	waitForText(t, sink, "second")

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stream error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not stop after cancellation")
	}
	if strings.Contains(sink.String(), "history") {
		t.Fatalf("the follower replayed pre-existing content: %q", sink.String())
	}
}

// TestStreamFollowsAFileCreatedLater pins that a follow on a missing log waits
// and then streams from the beginning.
func TestStreamFollowsAFileCreatedLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.log")
	logger := New(path, 0, testFormat)
	logger.SetPollInterval(5 * time.Millisecond)

	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	settled := awaitSettled(t, logger)
	go func() { done <- logger.Stream(ctx, sink) }()

	// The follower has to have decided that the file does not exist yet: writing
	// before that decision is a race the test would lose, not a defect in the
	// code.
	settled()
	if err := logger.Line("born"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	waitForText(t, sink, "born")
	cancel()
	<-done
}

// TestStreamFollowsAReplacedFile pins replacement detection: a rename-style
// rotation hands the follower a different inode.
func TestStreamFollowsAReplacedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")
	logger := New(path, 0, testFormat)
	logger.SetPollInterval(5 * time.Millisecond)
	if err := logger.Line("original"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	attached := awaitSettled(t, logger)
	go func() { done <- logger.Stream(ctx, sink) }()
	// Attach at the end of the existing file first.
	attached()
	if err := logger.Line("live"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	waitForText(t, sink, "live")

	renameOnto(t, path, path+".old")
	if err := os.WriteFile(path, []byte("replacement\n"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	waitForText(t, sink, "replacement")

	cancel()
	<-done
}

// TestStreamFromConsumesItsPositionOnce is the regression test for a stale
// handoff position. The caller's position applies to the first attach only:
// when the log is replaced afterwards — rotation by rename — the replacement
// must be read from its beginning, not from the position that belonged to the
// previous generation.
func TestStreamFromConsumesItsPositionOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")
	logger := New(path, 0, testFormat)
	logger.SetPollInterval(5 * time.Millisecond)

	first := strings.Repeat("A", 1000)
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatalf("write first generation: %v", err)
	}

	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	go func() { done <- logger.StreamFrom(ctx, sink, 500) }()
	waitForText(t, sink, strings.Repeat("A", 500))

	// Atomically replace the log with a shorter file that is still longer than
	// the handoff position. The whole replacement is new content and must all
	// be delivered; jumping to the stale position would skip its first half.
	replacement := strings.Repeat("B", 800)
	temp := filepath.Join(dir, "replacement")
	if err := os.WriteFile(temp, []byte(replacement), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	renameOnto(t, temp, path)
	waitForText(t, sink, replacement)

	cancel()
	<-done
}

// TestStreamFromDoesNotApplyAPositionToANewGeneration is the regression test
// for a handoff position that survived the file it belonged to. When the log
// the tail read is gone and a new file appears in its place, the position must
// not skip the beginning of the new generation.
func TestStreamFromDoesNotApplyAPositionToANewGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")
	logger := New(path, 0, testFormat)
	logger.SetPollInterval(50 * time.Millisecond)

	// The log existed when the tail ran and was gone by the time the follow
	// started, so the follower observes the absence before the replacement
	// appears.
	if err := os.WriteFile(path, []byte(strings.Repeat("A", 30)), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	settled := awaitSettled(t, logger)
	go func() { done <- logger.StreamFrom(ctx, sink, 10) }()
	settled() // the follower has observed the absence and dropped the position

	replacement := strings.Repeat("B", 21)
	if err := os.WriteFile(path, []byte(replacement), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	waitForText(t, sink, replacement)

	cancel()
	<-done
}

// TestRotationPreservesEverythingWrittenBeforeItStarted pins the concurrent
// writer case: build rotates the shared log while the server keeps appending,
// and every byte written before the rotation call must survive in the backup.
func TestRotationPreservesEverythingWrittenBeforeItStarted(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "dsh-web.log"), 4<<10, testFormat)
	for index := 0; index < 500; index++ {
		if err := logger.Line(fmt.Sprintf("before-%03d", index)); err != nil {
			t.Fatalf("seed line %d: %v", index, err)
		}
	}

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		// A bounded, throttled burst: enough to overlap the rotation, and it
		// ends on its own so the rotation's stable-copy loop always converges.
		for index := 0; index < 3000; index++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = logger.Line(fmt.Sprintf("during-%05d", index))
			time.Sleep(200 * time.Microsecond)
		}
	}()

	rotated, err := logger.RotateIfNeeded()
	close(stop)
	writer.Wait()
	if err != nil {
		t.Fatalf("RotateIfNeeded: %v", err)
	}
	if !rotated {
		t.Fatal("the log should have rotated")
	}
	backup, err := os.ReadFile(logger.BackupPath())
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Contains(backup, []byte("before-499")) {
		t.Fatal("a line written before the rotation started is missing from the backup")
	}
}

// TestRotationKeepsTheLinesWrittenWhileItCopies pins the stable copy: the
// rotation repeats its copy until the file stops growing, so a line the server
// appends while the copy runs is carried into the backup instead of vanishing
// between the copy and the truncate.
//
// The seeded log is large enough that the copy cannot finish before the marker
// is written; that is what makes the property observable rather than a race the
// test might win by luck. A rotation that copies once loses the marker.
func TestRotationKeepsTheLinesWrittenWhileItCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh-web.log")
	logger := New(path, 1<<20, testFormat)

	seed, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open seed: %v", err)
	}
	block := bytes.Repeat([]byte("x"), 1<<20)
	for index := 0; index < 32; index++ {
		if _, err := seed.Write(block); err != nil {
			_ = seed.Close()
			t.Fatalf("seed block %d: %v", index, err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed: %v", err)
	}

	marker := "written-while-the-copy-ran"
	appended := make(chan error, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		appended <- logger.Line(marker)
	}()

	rotated, err := logger.RotateIfNeeded()
	if err != nil {
		t.Fatalf("RotateIfNeeded: %v", err)
	}
	if !rotated {
		t.Fatal("the log should have rotated")
	}
	if err := <-appended; err != nil {
		t.Fatalf("Line: %v", err)
	}

	backup, err := os.ReadFile(logger.BackupPath())
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Contains(backup, []byte(marker)) {
		t.Fatal("a line written while the rotation copied is missing from the backup")
	}
}

// TestStreamEndsOnCancellation pins that the follow is interruptible.
func TestStreamEndsOnCancellation(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "dsh-web.log"), 0, testFormat)
	logger.SetPollInterval(5 * time.Millisecond)
	ctx, cancel := newCancelContext()
	cancel()
	if err := logger.Stream(ctx, &syncBuffer{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stream error = %v, want context.Canceled", err)
	}
}

// TestExistsAndSize pins the small accessors.
func TestExistsAndSize(t *testing.T) {
	logger := newLogger(t)
	if logger.Exists() {
		t.Fatal("a fresh logger must not report an existing file")
	}
	size, err := logger.Size()
	if err != nil || size != 0 {
		t.Fatalf("Size = (%d, %v), want (0, nil)", size, err)
	}
	if err := logger.Line("abcd"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	if !logger.Exists() {
		t.Fatal("the log should exist after a write")
	}
	size, err = logger.Size()
	if err != nil || size != 5 {
		t.Fatalf("Size = (%d, %v), want (5, nil)", size, err)
	}
}

// TestOpenAppendRotatesFirst pins that opening the log never appends after a
// rotation was due.
func TestOpenAppendRotatesFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	logger := New(path, 32, testFormat)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 64)), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	handle, err := logger.OpenAppend()
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	defer handle.Close()
	if _, err := handle.WriteString("fresh\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	live := readFileOrEmpty(t, path)
	if live != "fresh\n" {
		t.Fatalf("live log = %q, want only the new content", live)
	}
	if !strings.Contains(readFileOrEmpty(t, path+".old"), "xxxx") {
		t.Fatal("the previous generation was not kept")
	}
}

// TestTailFromReportsWhereItStopped pins the handoff that makes `logs --follow`
// gapless.
//
// The tail and the follow are two steps, and printing the tail takes time. A line
// written in between belongs to neither step if the follow simply attaches at the
// current end of the file, so the tail reports the offset it reached and the
// follow continues from there.
func TestTailFromReportsWhereItStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	logger := New(path, 0, testFormat)
	for _, line := range []string{"one", "two", "three"} {
		if err := logger.Line(line); err != nil {
			t.Fatalf("Line: %v", err)
		}
	}

	var buffer bytes.Buffer
	written, position, err := TailFrom(path, 2, &buffer)
	if err != nil {
		t.Fatalf("TailFrom: %v", err)
	}
	if written != 2 || buffer.String() != "two\nthree\n" {
		t.Fatalf("TailFrom wrote %d lines: %q", written, buffer.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if position != info.Size() {
		t.Fatalf("position = %d, want the end of the file %d", position, info.Size())
	}

	// A line written after the tail, followed from that position, is delivered.
	if err := logger.Line("four"); err != nil {
		t.Fatalf("Line: %v", err)
	}
	sink := &syncBuffer{}
	ctx, cancel := newCancelContext()
	done := make(chan error, 1)
	go func() { done <- logger.StreamFrom(ctx, sink, position) }()
	waitForText(t, sink, "four")
	cancel()
	<-done
	if strings.Contains(sink.String(), "three") {
		t.Fatalf("the follow repeated content the tail already printed: %q", sink.String())
	}
}

// TestReadTailLinesReportsTheSizeItRead pins the contract TailFrom builds on:
// the reported size is the one the read itself observed, so the follow starts
// exactly where the tail stopped even when the file grew while the tail ran.
func TestReadTailLinesReportsTheSizeItRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh-web.log")
	content := "one\ntwo\nthree\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, size, err := readTailLines(path, 2, maxTailBytes)
	if err != nil {
		t.Fatalf("readTailLines: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}
	if string(data) != "two\nthree\n" {
		t.Fatalf("data = %q, want the last two lines", data)
	}
}

// TestTailFromOnAMissingFile pins the first-run case: nothing to print, and the
// follow starts at the beginning of whatever appears.
func TestTailFromOnAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.log")
	var buffer bytes.Buffer
	written, position, err := TailFrom(path, 10, &buffer)
	if err != nil {
		t.Fatalf("TailFrom: %v", err)
	}
	if written != 0 || position != 0 || buffer.Len() != 0 {
		t.Fatalf("TailFrom = (%d, %d, %q), want nothing", written, position, buffer.String())
	}
}
