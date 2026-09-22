package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
)

// logScanWindowBytes is the logfile package's unexported maxScanBytes: how much
// of the tail of a log the build-section scanner walks. It is repeated here
// because the truncation branch cannot be reached without a file larger than it,
// and asserting the size the test created is what keeps the test honest about
// why it expects Truncated.
const logScanWindowBytes = 32 << 20

// TestLogsUsesTheDefaultLineCount pins the default a caller gets when it asks
// for no particular count. The value is user-visible — the command prints the
// tail of a file that may be megabytes long — and a `lines < 1` that fell
// through to "print nothing" would make `dshctl logs` useless in the common
// case, while one that fell through to "print everything" would flood the
// terminal.
func TestLogsUsesTheDefaultLineCount(t *testing.T) {
	f := newFixture(t)
	const seeded = 250
	for index := 1; index <= seeded; index++ {
		if err := f.LogFile.Line(fmt.Sprintf("line-%03d", index)); err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}

	if err := f.Logs(context.Background(), LogsOptions{Lines: 0}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	lines := strings.Split(strings.TrimRight(f.out.String(), "\n"), "\n")
	if len(lines) != DefaultLogLines {
		t.Fatalf("printed %d lines, want the %d-line default", len(lines), DefaultLogLines)
	}
	if lines[0] != fmt.Sprintf("line-%03d", seeded-DefaultLogLines+1) {
		t.Fatalf("first printed line = %q, want the %dth", lines[0], seeded-DefaultLogLines+1)
	}
	if lines[len(lines)-1] != fmt.Sprintf("line-%03d", seeded) {
		t.Fatalf("last printed line = %q, want the last line written", lines[len(lines)-1])
	}
}

// TestLogsReportsAMissingLog pins that a log which does not exist is a Failure
// naming the path, not a silent success: an operator who sees nothing printed
// has to learn whether the log is empty or absent.
func TestLogsReportsAMissingLog(t *testing.T) {
	f := newFixture(t)
	if _, err := os.Lstat(f.Settings.LogPath); !os.IsNotExist(err) {
		t.Fatalf("the fixture already created the log: %v", err)
	}

	err := f.Logs(context.Background(), LogsOptions{Lines: 10})
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "日志文件不存在")
	wantContains(t, err, f.Settings.LogPath)
}

// TestLogsReportsACancelledContext pins that cancellation is not dressed up as
// a service failure: the shell that sent SIGINT branches on the context error,
// and a Failure (exit code 1) would read as "dshctl is broken" instead of "you
// interrupted it".
func TestLogsReportsACancelledContext(t *testing.T) {
	f := newFixture(t)
	if err := f.LogFile.Line("server output"); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.Logs(ctx, LogsOptions{Lines: 10})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Logs error = %v, want context.Canceled", err)
	}
	if f.out.String() != "" {
		t.Fatalf("stdout = %q, want nothing printed for a cancelled call", f.out.String())
	}
}

// TestLogsBuildOnlyPrintsTheLastRecord pins the `--build` path that prints
// something: the body of the most recent build section belongs to the operator's
// current problem, and an older record printed instead would be worse than
// nothing.
func TestLogsBuildOnlyPrintsTheLastRecord(t *testing.T) {
	f := newFixture(t)
	if err := f.LogFile.Line("before any build"); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	if err := f.LogFile.Section("build"); err != nil {
		t.Fatalf("section: %v", err)
	}
	if err := f.LogFile.Line("first build body"); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	if err := f.LogFile.Section("build"); err != nil {
		t.Fatalf("section: %v", err)
	}
	if err := f.LogFile.Line("second build body line one"); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	if err := f.LogFile.Line("second build body line two"); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	if err := f.Logs(context.Background(), LogsOptions{BuildOnly: true}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	out := f.out.String()
	if !strings.Contains(out, "second build body line one") || !strings.Contains(out, "second build body line two") {
		t.Fatalf("stdout = %q, want the last build body", out)
	}
	if strings.Contains(out, "first build body") || strings.Contains(out, "before any build") {
		t.Fatalf("stdout = %q, want only the last build record", out)
	}
}

// TestLogsBuildOnlyTrimsToTheRequestedLines pins that the line count applies to
// a build section too. A build that printed thousands of lines is exactly when
// an operator reaches for a smaller count, and a `-n 5` that returned the whole
// section would defeat the flag.
func TestLogsBuildOnlyTrimsToTheRequestedLines(t *testing.T) {
	f := newFixture(t)
	if err := f.LogFile.Section("build"); err != nil {
		t.Fatalf("section: %v", err)
	}
	for index := 1; index <= 20; index++ {
		if err := f.LogFile.Line(fmt.Sprintf("build-output-%02d", index)); err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}

	if err := f.Logs(context.Background(), LogsOptions{BuildOnly: true, Lines: 5}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	lines := strings.Split(strings.TrimRight(f.out.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("printed %d lines, want 5: %q", len(lines), f.out.String())
	}
	if lines[0] != "build-output-16" || lines[4] != "build-output-20" {
		t.Fatalf("printed %v, want the last five build lines", lines)
	}
}

// TestLogsBuildOnlyReportsATruncatedLog pins the honest answer for a log larger
// than the section scanner can walk. "I could not look" must not be reported as
// "there is no build record": the operator would go looking for a missing build
// instead of reading the tail of an oversized log.
//
// The file is created as a sparse one — a real section marker followed by a
// hole that extends past the scan window — so the test costs a few writes
// rather than 32 MiB of disk and time, while the scanner still sees a file
// larger than the window.
func TestLogsBuildOnlyReportsATruncatedLog(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.state, 0o755); err != nil {
		t.Fatalf("create the state directory: %v", err)
	}
	file, err := os.Create(f.Settings.LogPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	marker := fmt.Sprintf("%s 2024-01-01 00:00:00 build %s\n", strings.Repeat("=", 5), strings.Repeat("=", 5))
	if _, err := file.WriteString(marker + "ancient build output\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	// The rest of the file is a hole: only the size matters to the scanner.
	const logSize = int64(logScanWindowBytes + 1024)
	if _, err := file.Seek(logSize-1, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		t.Fatalf("extend log: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}
	info, err := os.Stat(f.Settings.LogPath)
	if err != nil || info.Size() <= int64(logScanWindowBytes) {
		t.Fatalf("log size = %v (err=%v), want more than the %d-byte scan window", info, err, logScanWindowBytes)
	}
	err = f.Logs(context.Background(), LogsOptions{BuildOnly: true})
	wantCode(t, err, exitcode.Failure)
	wantContains(t, err, "日志文件过大")
	if f.out.String() != "" {
		t.Fatalf("stdout = %q, want nothing printed for an unreadable record", f.out.String())
	}
}

// TestWebURLRefusesAnotherPortsAddress pins the token-leak guard on the
// record-then-log path. The state directory is shared by every port, so the
// record may hold an address that belongs to a different server — and handing
// that address (and its token) to an operator of this port is a silent
// cross-service leak. A record for another port with nothing in the log for
// this one is a Failure, never that address.
func TestWebURLRefusesAnotherPortsAddress(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "http://127.0.0.1:"+strconv.Itoa(f.Settings.Port+1)+"/?token=somebody-else")
	if err := f.LogFile.Line("dsh web: http://127.0.0.1:" + strconv.Itoa(f.Settings.Port+1) + "/?token=somebody-else"); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	_, err := f.WebURL(context.Background())
	wantCode(t, err, exitcode.Failure)
	if err != nil && strings.Contains(err.Error(), "somebody-else") {
		t.Fatalf("error = %v, want no foreign token leaked", err)
	}
	wantContains(t, err, strconv.Itoa(f.Settings.Port))
}

// TestWebURLExplainsWhyThereIsNoAddress pins the "nothing is running" answer.
// It names the state that was observed, so an operator staring at a port that
// looks busy learns whether dshctl sees a stopped service or somebody else's
// process.
func TestWebURLExplainsWhyThereIsNoAddress(t *testing.T) {
	f := newFixture(t)
	// A stale record: it exists, so the summary has to say what became of it.
	if err := f.Record.Save(stateRecord(f, 999999)); err != nil {
		t.Fatalf("save record: %v", err)
	}

	_, err := f.WebURL(context.Background())
	wantCode(t, err, exitcode.NotRunning)
	wantContains(t, err, "DSH Web 未在运行")
	wantContains(t, err, "没有可访问的地址")
}

// TestLogsBuildOnlyReadsTheRealSectionPath is a guard against the fixture
// silently changing what `--build` reads: the section must come from the log
// file the service was configured with, not from a path a test happens to
// create.
func TestLogsBuildOnlyReadsTheRealSectionPath(t *testing.T) {
	f := newFixture(t)
	if err := f.LogFile.Section("update"); err != nil {
		t.Fatalf("section: %v", err)
	}
	if err := f.LogFile.Line("pulled to abc1234"); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	// A decoy that even matches the state file's name pattern: if the command
	// ever read the wrong file in the state directory, this is what it would
	// find.
	decoy := f.Settings.StateFile()
	writeFile(t, decoy, "===== 2024-01-01 00:00:00 dshctl build =====\nDECOY\n")

	if err := f.Logs(context.Background(), LogsOptions{BuildOnly: true}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !strings.Contains(f.out.String(), "pulled to abc1234") {
		t.Fatalf("stdout = %q, want the update record from the configured log", f.out.String())
	}
	if strings.Contains(f.out.String(), "DECOY") {
		t.Fatalf("stdout = %q, want nothing from another file", f.out.String())
	}
}
