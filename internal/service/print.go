package service

import (
	"fmt"
	"io"
)

// PrintStatus writes the human-readable status report.
func PrintStatus(w io.Writer, status Status) error {
	summary := statusSummary(status)
	switch status.State {
	case StateRunning:
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
	case StateStarting:
		if _, err := fmt.Fprintf(w, "状态: 启动中或关闭中(端口未就绪)\nPID:  %d\n日志: %s\n",
			status.RecordedPID, status.LogPath); err != nil {
			return err
		}
		return nil
	case StateForeign:
		if _, err := fmt.Fprintf(w, "状态: 端口被占用（非 dshctl 启动的进程）\n地址: %s\n进程: %s\n日志: %s\n",
			status.URL, status.ListenerCommand, status.LogPath); err != nil {
			return err
		}
		return nil
	case StateOrphan:
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
