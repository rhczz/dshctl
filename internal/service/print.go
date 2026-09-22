package service

import (
	"fmt"
	"io"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"sort"
)

// ServeExitCode maps an observed state onto the process exit code, so a shell
// condition can ask whether the service is up.
//
// Running and starting both report success: the service exists and is being
// managed. Everything else reports "not running". It lives here rather than on
// the model because exit codes are the operator's contract, which the model has
// no business knowing.
func ServeExitCode(status domain.Status) int {
	if status.Owning() {
		return exitcode.OK
	}
	return exitcode.NotRunning
}

// StatusReport is what `status` observed.
//
// One state directory may manage several servers, so the report carries the
// instance the command was about — the configured port, or the one named on the
// command line — beside every instance this state directory holds a record for.
// The two are the same list for a single-instance installation, and the shape
// stays an object because that is what every existing consumer of `--json`
// reads.
type StatusReport struct {
	// Status is the instance the command was about.
	Status domain.Status `json:"status"`
	// Ports lists every observed instance: the instance the command was about
	// first, then the rest in ascending port order. It is what makes a server
	// started with another port visible instead of orphaned.
	Ports []domain.Status `json:"ports,omitempty"`
	// Others is the instances worth naming beside the one above: the ones that
	// are serving, or that need attention. An instance that is simply not running
	// is left out, because a report is not a roll call of everything that is off.
	Others []domain.Status `json:"-"`
}

// NewStatusReport assembles the report from the observed instances.
//
// The instance the command was about is always first: the selection puts the
// configured port there, or the one that was named, so a report of a single
// instance is that instance.
func NewStatusReport(statuses []domain.Status) StatusReport {
	report := StatusReport{Ports: statuses}
	if len(statuses) == 0 {
		return report
	}
	report.Status = statuses[0]
	report.Others = make([]domain.Status, 0, len(statuses)-1)
	for _, status := range statuses[1:] {
		if status.Port == report.Status.Port || status.State == domain.StateStopped {
			continue
		}
		report.Others = append(report.Others, status)
	}
	return report
}

// PrintStatuses writes the human-readable status report for every observed
// instance, and the token-carrying address of the extra ones to extra.
//
// The first block is the instance the command was about and follows the shape
// `status` has always had. When this state directory manages more, each of them
// is named rather than left out: an operator who started a server with `--port`
// has to be able to see that it is still running, and where to reach it. Those
// lines go to extra — standard error for the command — because they are notes
// about other instances, while standard output stays the report of the instance
// that was asked about.
func PrintStatuses(w, extra io.Writer, report StatusReport) error {
	if err := PrintStatus(w, report.Status); err != nil {
		return err
	}
	for _, status := range report.Others {
		if _, err := fmt.Fprintf(extra, "\n端口 %d:\n", status.Port); err != nil {
			return err
		}
		if err := PrintStatus(extra, status); err != nil {
			return err
		}
	}
	return nil
}

// PrintURLs writes one token-carrying address per line, in ascending port order.
//
// Every address that was found is printed, because an operator asking for "the
// address" of an installation that runs two servers needs both; a script that
// wants one address names its port. What was *not* found is not silently
// missing either: an instance that is running but has no address yet is named on
// standard error, and the caller turns an empty report into "not running".
func PrintURLs(w io.Writer, extra io.Writer, report URLReport) error {
	ports := make([]int, 0, len(report.Addresses))
	for port := range report.Addresses {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	for _, port := range ports {
		if _, err := fmt.Fprintln(w, report.Addresses[port]); err != nil {
			return err
		}
	}
	for _, status := range report.Statuses {
		if _, done := report.Addresses[status.Port]; done {
			continue
		}
		// The instance the command was about is not explained twice: when there
		// is simply nothing running there, the caller says so once — with an
		// error, or with the exit code the command is documented to return.
		if status.Port == report.Status.Port && !status.Owning() && !status.Survivor {
			continue
		}
		if _, err := fmt.Fprintf(extra, "端口 %d 上没有可访问的地址(%s)\n", status.Port, StatusSummary(status)); err != nil {
			return err
		}
	}
	return nil
}

// PrintStatus writes the human-readable status report.
func PrintStatus(w io.Writer, status domain.Status) error {
	summary := StatusSummary(status)
	switch status.State {
	case domain.StateRunning:
		if _, err := fmt.Fprintf(w, "状态: 运行中\n地址: %s\nPID:  %d\n", status.URL, status.ListenerPID); err != nil {
			return err
		}
		if status.URLFromRecord != "" {
			if _, err := fmt.Fprintf(w, "访问: %s\n", status.URLFromRecord); err != nil {
				return err
			}
		}
		// The subject of this branch is the service that is running, so the
		// checkout line names the tree that process serves. The configured one is
		// named too when the two differ: --repo applies to one invocation, and a
		// status that printed only the configuration would describe a directory
		// the running instance never used.
		checkout := status.RepoDir
		note := ""
		switch {
		case status.RecordedRepoDir != "":
			checkout = status.RecordedRepoDir
			if status.RecordedRepoDir != status.RepoDir {
				note = " (配置中为 " + status.RepoDir + ")"
			}
		case checkout != "":
			note = " (配置值；运行记录未记录仓库目录)"
		}
		if _, err := fmt.Fprintf(w, "仓库: %s%s\n日志: %s\n", checkout, note, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateStarting:
		if _, err := fmt.Fprintf(w, "状态: 启动中或关闭中(端口未就绪)\nPID:  %d\n日志: %s\n",
			status.RecordedPID, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateForeign:
		if _, err := fmt.Fprintf(w, "状态: 端口被占用（非 dshctl 启动的进程）\n地址: %s\n进程: %s\n日志: %s\n",
			status.URL, status.ListenerCommand, status.LogPath); err != nil {
			return err
		}
		return nil
	case domain.StateOrphan:
		if _, err := fmt.Fprintf(w, "状态: 端口被一个 dshctl 无法确认归属的进程占用\n地址: %s\n进程: %s\n",
			status.URL, status.ListenerCommand); err != nil {
			return err
		}
		if status.Survivor {
			_, err := fmt.Fprintln(w, "提示: 这是上次启动被中断后仍存活的服务;运行 dshctl start 或 dshctl stop 可恢复管理")
			return err
		}
		_, err := fmt.Fprintln(w, "提示: dshctl 不会结束它;确认可以安全停止后请手动处理")
		return err
	case domain.StateUnobservable:
		// Nothing about this instance is known, and saying "not running" would
		// be a claim the failed probe cannot support.
		if _, err := fmt.Fprintf(w, "状态: 无法探测端口 %d 的状态: %s\n", status.Port, status.ProbeError); err != nil {
			return err
		}
		if status.RecordedPID != 0 {
			if _, err := fmt.Fprintf(w, "记录: pid=%d\n", status.RecordedPID); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(w, "提示: 端口探测工具(lsof/ss/netstat)不可用时无法判断该实例，请修复后重试")
		return err
	default:
		if _, err := fmt.Fprintf(w, "状态: %s\n", summary); err != nil {
			return err
		}
		if status.RecordLive {
			if _, err := fmt.Fprintf(w,
				"记录: pid=%d 仍然存活，但没有监听端口 %d;可用 dshctl stop 结束它\n",
				status.RecordedPID, status.Port); err != nil {
				return err
			}
		}
		if status.RecordStale && status.StaleRecord != nil {
			if _, err := fmt.Fprintf(w, "陈旧记录: pid=%d 已不存在或已被复用\n", status.StaleRecord.PID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "日志: %s\n", status.LogPath); err != nil {
			return err
		}
		return nil
	}
}

// PrintChecks writes the human-readable diagnosis.
func PrintChecks(w io.Writer, checks []Check) error {
	for _, check := range checks {
		label := "OK  "
		switch check.Status {
		case CheckWarn:
			label = "警告"
		case CheckFail:
			label = "失败"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", label, check.Name, check.Detail); err != nil {
			return err
		}
	}
	return nil
}
