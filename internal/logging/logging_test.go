package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/logfile"
)

func newLogger(t *testing.T, level Level) (*Logger, *logfile.Logger, string) {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/dsh-web.log"
	file := logfile.New(path, 0, logfile.Format{Prefix: "=====", Product: "dshctl", Layout: "2006-01-02 15:04:05"})
	return New(file, level), file, path
}

// read returns the log's contents; a file that was never created reads as empty,
// which is what "nothing was recorded" means here.
func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return string(data)
}

func TestParseLevelNames(t *testing.T) {
	cases := []struct {
		text string
		want Level
	}{
		{"debug", LevelDebug},
		{"INFO", LevelInfo},
		{" info ", LevelInfo},
		{"", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.text)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", tc.text, err)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestParseLevelRefusesAnUnknownName(t *testing.T) {
	_, err := ParseLevel("verbose")
	if err == nil {
		t.Fatal("an unknown level was accepted")
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Errorf("the error does not name the value: %v", err)
	}
}

func TestInfoRecordsALineInsideTheCurrentSection(t *testing.T) {
	logger, _, path := newLogger(t, LevelInfo)
	if err := logger.Section("start"); err != nil {
		t.Fatalf("section: %v", err)
	}
	logger.Info("启动中")
	content := read(t, path)
	if !strings.Contains(content, "dshctl start") {
		t.Errorf("the section marker is missing: %q", content)
	}
	if !strings.Contains(content, "启动中") {
		t.Errorf("the line is missing from the log: %q", content)
	}
}

func TestDebugIsSilentUntilTheLevelAsksForIt(t *testing.T) {
	quiet, _, quietPath := newLogger(t, LevelInfo)
	quiet.Debug("探测端口 3080")
	if content := read(t, quietPath); strings.Contains(content, "探测端口") {
		t.Errorf("a debug line was recorded at info level: %q", content)
	}

	loud, _, loudPath := newLogger(t, LevelDebug)
	loud.Debug("探测端口 3080")
	if content := read(t, loudPath); !strings.Contains(content, "探测端口") {
		t.Errorf("a debug line was dropped at debug level: %q", content)
	}
}

func TestStepRecordsWhatItCost(t *testing.T) {
	logger, _, path := newLogger(t, LevelInfo)
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 12*int(time.Millisecond), time.UTC),
	}
	next := 0
	logger.Now = func() time.Time {
		value := times[next]
		if next < len(times)-1 {
			next++
		}
		return value
	}
	if err := logger.Step("切换版本", func() error { return nil }); err != nil {
		t.Fatalf("step: %v", err)
	}
	content := read(t, path)
	if !strings.Contains(content, "步骤 切换版本: 耗时 12ms") {
		t.Errorf("the step line is wrong: %q", content)
	}
}

func TestStepKeepsTheOperationError(t *testing.T) {
	logger, _, path := newLogger(t, LevelInfo)
	failure := fmt.Errorf("切换失败")
	err := logger.Step("切换版本", func() error { return failure })
	if err != failure {
		t.Fatalf("step returned %v, want the operation's error", err)
	}
	content := read(t, path)
	if !strings.Contains(content, "步骤 切换版本: 失败（切换失败）") {
		t.Errorf("the failed step line is wrong: %q", content)
	}
}

func TestLogFailureDoesNotMaskTheOperation(t *testing.T) {
	dir := t.TempDir()
	logger := New(logfile.New(dir, 0, logfile.Format{Prefix: "=====", Product: "dshctl", Layout: "2006-01-02 15:04:05"}), LevelInfo)
	failure := fmt.Errorf("切换失败")
	if err := logger.Step("切换版本", func() error { return failure }); err != failure {
		t.Fatalf("step returned %v, want the operation's error", err)
	}
	logger.Info("这一行写不进去")
}
