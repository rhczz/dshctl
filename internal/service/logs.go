package service

import (
	"context"
	"fmt"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/logfile"
)

// DefaultLogLines is how much of the log `dshctl logs` prints.
const DefaultLogLines = 200

// buildSectionTitles are the section names that record a build, update or
// rollback run: `logs --build` is how an operator reads what the last
// deployment did, whichever kind it was.
var buildSectionTitles = []string{"build", "update", "rollback"}

// LogsOptions selects what `dshctl logs` prints.
type LogsOptions struct {
	// Lines is how many trailing lines to print; values below 1 use
	// DefaultLogLines, whichever section is printed.
	Lines int
	// Follow keeps streaming new lines until the context is cancelled.
	Follow bool
	// BuildOnly prints the last build or update record instead of the service log.
	BuildOnly bool
}

// Logs prints or follows the log file.
//
// The tail and the follow run over one pass: the followed offset is the end of
// what was printed, so a line written while the tail is being flushed is still
// delivered. Two separate passes would silently drop it.
func (s *Service) Logs(ctx context.Context, options LogsOptions) error {
	lines := options.Lines
	if lines < 1 {
		lines = DefaultLogLines
	}
	if options.BuildOnly {
		return s.printBuildSection(lines)
	}
	if !s.LogFile.Exists() {
		return exitcode.New(exitcode.Failure, "日志文件不存在: %s", s.Settings.LogPath)
	}
	if options.Follow {
		return s.tailThenFollow(ctx, lines)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := logfile.Tail(s.Settings.LogPath, lines, s.emitter().Stream()); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return exitcode.Wrap(exitcode.Failure, err)
	}
	return ctx.Err()
}

// tailThenFollow prints the trailing lines and then streams everything after
// them.
//
// The follow starts where the tail stopped, not at the end of the file: the two
// steps are separated by the time it takes to print the tail, and a line written
// in that window belongs to neither step if the follow simply attaches at the
// current end.
func (s *Service) tailThenFollow(ctx context.Context, lines int) error {
	_, position, err := logfile.TailFrom(s.Settings.LogPath, lines, s.emitter().Stream())
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return exitcode.Wrap(exitcode.Failure, err)
	}
	if err := s.LogFile.StreamFrom(ctx, s.emitter().Stream(), position); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return exitcode.Wrap(exitcode.Failure, err)
	}
	return ctx.Err()
}

// printBuildSection prints the body of the last build or update record.
func (s *Service) printBuildSection(lines int) error {
	body, outcome, err := logfile.LastSection(s.Settings.LogPath, buildSectionTitles)
	if err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	switch outcome {
	case logfile.NotFound:
		s.failure(fmt.Sprintf("日志中没有 build/update/rollback 记录: %s", s.Settings.LogPath))
		return nil
	case logfile.Truncated:
		return exitcode.New(exitcode.Failure,
			"日志文件过大，未能定位最近一次 build/update/rollback 记录: %s\n提示: 用 dshctl logs -n <行数> 直接查看尾部",
			s.Settings.LogPath)
	}
	if len(body) > lines {
		body = body[len(body)-lines:]
	}
	for _, line := range body {
		s.narrate(line)
	}
	return nil
}

// WebURL returns the address of the server this dshctl manages, token included.
//
// It answers only when a server of ours is actually running — the same state
// every other command acts on — because an address is only useful when
// something listens behind it. The address itself comes from the runtime record
// first (it survives log rotation and names its port) and from the log as a
// fallback for a server that started before the record existed.
func (s *Service) WebURL(ctx context.Context) (string, error) {
	observed, err := s.observe(ctx)
	if err != nil {
		return "", err
	}
	return s.observedAddress(observed.status)
}

// observedAddress reports the token-carrying address of one observed instance.
//
// It answers for a server of ours that is up or starting, and for the survivor
// of an interrupted start; every other state has no address. A multi-instance
// command and a single-instance one ask the same question here, which is what
// keeps `url --port 3081` and the 3081 line of a bare `url` from disagreeing
// about the same server.
//
// The port is the value's own, so one instance's address is never reported for
// another: an address carries a token, and handing an operator the token of a
// different server is exactly the mistake this pairing exists to prevent.
func (s *Service) observedAddress(status domain.Status) (string, error) {
	switch {
	case status.Owning():
		// The server is up (or starting): its address is the one it announced.
		if url := status.URLFromRecord; url != "" && addressPort(url) == s.boundPort() {
			return url, nil
		}
		address, truncated := announcedURL(s.Settings.LogPath, s.boundPort())
		if address != "" {
			return address, nil
		}
		if truncated {
			return "", exitcode.New(exitcode.Failure,
				"日志已超过 %d MiB，未能在其中定位端口 %d 的访问地址\n提示: 可运行 dshctl logs -n 50 查看尾部输出",
				logScanMiB, s.boundPort())
		}
		if status.State == domain.StateStarting {
			return "", exitcode.New(exitcode.Failure,
				"服务正在启动，尚未公布端口 %d 的访问地址;稍后重试或查看 dshctl logs", s.boundPort())
		}
		return "", exitcode.New(exitcode.Failure,
			"运行记录中没有端口 %d 的访问地址，日志中也找不到: %s", s.boundPort(), s.Settings.LogPath)

	case status.Survivor:
		// A server of ours is serving, left behind by an interrupted start.
		// Its address is in the log; managing it again is one command away.
		address, _ := announcedURL(s.Settings.LogPath, s.boundPort())
		if address != "" {
			s.failure("提示: 这是上次启动被中断后仍存活的服务;运行 dshctl start 或 dshctl stop 可恢复管理")
			return address, nil
		}
		return "", exitcode.New(exitcode.Failure,
			"端口 %d 上的服务是上次启动遗留的，日志中找不到它的访问地址;运行 dshctl start 恢复管理后再试", s.boundPort())

	default:
		return "", exitcode.New(exitcode.NotRunning,
			"DSH Web 未在运行(%s)，没有可访问的地址", StatusSummary(status))
	}
}

// logScanMiB is how much of the log the address lookup can see, in MiB.
const logScanMiB = 8
