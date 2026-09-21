// Package logging is dshctl's diagnostic channel: one level per line, one sink
// per audience.
//
// The log file is the operator's record: sections mirror the operations, and a
// step line carries what the step cost. Standard error is what the operator has
// to see right now, which is why a warning goes there and does not wait for a
// reader.
//
// The file format itself belongs to internal/logfile. This package only decides
// what a line is called and which sink it reaches, so the bytes an operator has
// been reading do not change.
//
// It does not use log/slog. The file format is the contract, a handler would
// have to reproduce it byte for byte, and no consumer here wants records or
// attributes that the format cannot carry: the standard library's logger would
// be indirection with nothing on the other side.
package logging

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/logfile"
)

// Level is how much detail a line carries. Info is the default: everything
// dshctl records about an operation, step lines included.
type Level int

// The zero value is not a level: it means the caller did not choose one, which
// lets a struct field carry "unset" without a second flag.
const (
	LevelDebug Level = 1 + iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	}
	return fmt.Sprintf("Level(%d)", int(l))
}

// ParseLevel reads the name a command line or an environment variable carries.
// The empty string means "not set" and answers with the default, because the
// caller that resolves settings is the one place allowed to apply defaults.
func ParseLevel(text string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "debug":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	}
	return LevelInfo, fmt.Errorf("未知的日志级别 %q: 取值是 debug、info、warn、error", text)
}

// Logger writes diagnostics to the log file and to standard error.
type Logger struct {
	// Now is the clock the step lines read. Tests replace it; production uses
	// time.Now, set by New.
	Now func() time.Time

	file  *logfile.Logger
	diag  io.Writer
	level Level
}

// New returns a logger that records to file and reports to diag.
func New(file *logfile.Logger, diag io.Writer, level Level) *Logger {
	return &Logger{Now: time.Now, file: file, diag: diag, level: level}
}

// Level reports the threshold this logger filters at.
func (l *Logger) Level() Level { return l.level }

// Section marks a new operation in the log file.
func (l *Logger) Section(title string) error { return l.file.Section(title) }

// Info records a line about the operation in the log file. A log that cannot be
// written must not turn into the operation's result, so the failure is dropped
// here rather than returned: the operator still gets the outcome on stdout and
// standard error.
func (l *Logger) Info(msg string) {
	if l.level > LevelInfo {
		return
	}
	_ = l.file.Line(msg)
}

// Debug records detail only the debug level asked for.
func (l *Logger) Debug(msg string) {
	if l.level > LevelDebug {
		return
	}
	_ = l.file.Line(msg)
}

// Warn reports a diagnostic that does not stop the operation. It goes to
// standard error and not to the log file, because a warning is for the operator
// reading this run, not for whoever reads the file later.
func (l *Logger) Warn(msg string) {
	fmt.Fprintf(l.diag, "警告: %s\n", msg)
}

// Error writes a line to standard error for a caller that frames its own text,
// so a failure raised inside an operation is not double-prefixed.
func (l *Logger) Error(msg string) {
	fmt.Fprintln(l.diag, msg)
}

// Step runs one named step of an operation, records what it cost, and returns
// the step's error unchanged. It is the observability seam: the cost of every
// step an operator can name lands in the log next to the step's name.
func (l *Logger) Step(name string, run func() error) error {
	start := l.Now()
	err := run()
	elapsed := l.Now().Sub(start).Round(time.Millisecond)
	if err != nil {
		l.Info(fmt.Sprintf("步骤 %s: 失败（%v），耗时 %s", name, err, elapsed))
		return err
	}
	l.Info(fmt.Sprintf("步骤 %s: 耗时 %s", name, elapsed))
	return nil
}
