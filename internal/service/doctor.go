package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/detach"
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/lock"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

// Check statuses.
const (
	// CheckOK means the item is healthy.
	CheckOK = "ok"
	// CheckWarn means the item deserves attention but does not block.
	CheckWarn = "warn"
	// CheckFail means an operation will fail until it is fixed.
	CheckFail = "fail"
)

// Check is one diagnosed item.
type Check struct {
	// Name is the item's short label.
	Name string `json:"name"`
	// Status is one of the Check* constants.
	Status string `json:"status"`
	// Detail explains the observed value or the remedy.
	Detail string `json:"detail"`
}

// Doctor inspects the environment without changing it.
func (s *Service) Doctor(ctx context.Context) []Check {
	checks := make([]Check, 0, 14)
	add := func(name, status, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	if paths.IsDir(s.Settings.StateDir) {
		add("状态目录", CheckOK, s.Settings.StateDir)
	} else {
		add("状态目录", CheckWarn, s.Settings.StateDir+" 尚未创建，首次运行会自动创建")
	}
	if paths.Exists(s.Settings.ConfigPath) {
		add("配置文件", CheckOK, s.Settings.ConfigPath)
	} else {
		add("配置文件", CheckWarn, s.Settings.ConfigPath+" 尚未创建，将写入默认值")
	}

	switch {
	case !s.Repo.Exists():
		add("仓库目录", CheckFail, fmt.Sprintf(
			"%s 不存在；用 --repo 或环境变量 %s 指定一次，成功运行后会写入 %s",
			s.Settings.RepoDir, paths.EnvRepoDir, s.Settings.ConfigPath))
	case !s.Repo.IsGit():
		add("仓库目录", CheckFail, s.Settings.RepoDir+" 不是 git 仓库")
	case !s.Repo.IsServerCheckout():
		add("仓库目录", CheckFail,
			fmt.Sprintf("%s 缺少 %s 或 %s，不像是 DeepSeek Harness checkout",
				s.Settings.RepoDir, config.ServerManifestRel, config.WorkspaceManifestRel))
	default:
		sha, branch, err := s.Repo.Head(ctx)
		if err != nil {
			add("仓库版本", CheckWarn, err.Error())
			break
		}
		detail := branch + "@" + sha
		if dirty, err := s.Repo.Dirty(ctx); err == nil && dirty {
			detail += " (有未提交改动)"
		}
		add("仓库版本", CheckOK, detail)
	}

	if s.Repo.NodeModulesPresent() {
		add("依赖", CheckOK, filepath.Join(s.Settings.RepoDir, "node_modules")+" 已安装")
	} else {
		add("依赖", CheckFail, filepath.Join(s.Settings.RepoDir, "node_modules")+" 不存在，请先执行 pnpm install")
	}
	if s.Repo.BuildReady() {
		add("构建产物", CheckOK, s.Repo.BuildRecordPath())
	} else {
		add("构建产物", CheckFail, "缺少 "+s.Repo.BuildRecordPath()+"，请运行 dshctl build")
	}

	s.doctorNode(ctx, add)
	s.doctorPnpm(ctx, add)
	s.doctorService(ctx, add)
	s.doctorLock(add)

	if size, err := s.LogFile.Size(); err != nil {
		add("日志", CheckWarn, err.Error())
	} else {
		add("日志", CheckOK, fmt.Sprintf("%s (%s)", s.Settings.LogPath, humanBytes(size)))
	}
	add("进程分离方式", CheckOK, detach.Describe())
	return checks
}

// doctorNode reports the Node runtime resolution.
//
// The row is the resolution a start would use, judged by the same gate: a doctor
// that disagreed with the start about a machine would be worse than no doctor at
// all.
func (s *Service) doctorNode(ctx context.Context, add func(string, string, string)) {
	installation, err := s.resolveNode(ctx)
	if err != nil {
		add("Node", CheckFail, err.Error())
		return
	}
	detail := fmt.Sprintf("%s (%s, %s)", installation.NodePath, installation.Version, installation.Source)
	if installation.ViaShim {
		detail += "，经转发条目解析"
	}
	// A release the gate refuses never reaches this point: resolveNode reports it
	// as the failure above. Everything else is usable, so the row separates
	// "inside the verified range" from "used, and said out loud".
	switch verdict := nodejs.Assess(installation, config.MinNodeVersion, config.TestedNodeVersion); verdict.Status {
	case nodejs.Supported:
		add("Node", CheckOK, detail)
	default:
		add("Node", CheckWarn, detail+"；"+verdict.Reason)
	}
}

// doctorPnpm reports whether pnpm is runnable.
func (s *Service) doctorPnpm(ctx context.Context, add func(string, string, string)) {
	path, err := s.pnpmPath()
	if err != nil {
		add("pnpm", CheckFail, "找不到 pnpm，请先安装并确保它在 PATH 中")
		return
	}
	version, err := run.Collector(s.Exec).Output(ctx, run.Command{Name: path, Args: []string{"--version"}})
	if err != nil {
		add("pnpm", CheckWarn, path+" 存在但无法执行")
		return
	}
	add("pnpm", CheckOK, strings.TrimSpace(path+" "+version))
}

// doctorService reports the port and the runtime record from one observation,
// using the same judgment `status` uses.
//
// Asking the two questions separately is how doctor and status end up
// disagreeing about one machine: one says "the service dshctl started", the
// other "another process holds the port". One observation, one verdict, two
// rows.
func (s *Service) doctorService(ctx context.Context, add func(string, string, string)) {
	observed, err := s.observe(ctx)
	if err != nil {
		add("端口", CheckFail, err.Error())
		add("运行记录", CheckWarn, "无法观察服务状态，见上一条")
		return
	}
	status := observed.status

	switch status.State {
	case domain.StateRunning:
		add("端口", CheckOK, fmt.Sprintf("%d 由 dshctl 启动的服务占用 (pid=%d)", s.Settings.Port, status.ListenerPID))
	case domain.StateStarting:
		add("端口", CheckWarn, fmt.Sprintf("%d 由 dshctl 的服务占用 (pid=%d)，端口尚未就绪", s.Settings.Port, status.ListenerPID))
	case domain.StateForeign:
		add("端口", CheckWarn, fmt.Sprintf("%d 被其他进程占用 (pid=%d: %s)",
			s.Settings.Port, status.ListenerPID, status.ListenerCommand))
	case domain.StateOrphan:
		if status.Survivor {
			add("端口", CheckWarn, fmt.Sprintf(
				"%d 上是上次启动被中断后仍存活的服务 (pid=%d);运行 dshctl start 或 dshctl stop 可恢复管理",
				s.Settings.Port, status.ListenerPID))
		} else {
			add("端口", CheckWarn, fmt.Sprintf("%d 被一个 dshctl 无法确认归属的进程占用 (pid=%d: %s)",
				s.Settings.Port, status.ListenerPID, status.ListenerCommand))
		}
	default:
		add("端口", CheckOK, fmt.Sprintf("%d 空闲", s.Settings.Port))
	}

	switch {
	case status.RecordStale && status.StaleRecord == nil:
		add("运行记录", CheckWarn, s.Record.Path+" 无法解析，下次 start/stop 会重建它")
	case status.RecordStale:
		add("运行记录", CheckWarn, fmt.Sprintf(
			"记录 pid=%d 已不存在或已被复用(陈旧记录，下次 start/stop 会清理)", status.StaleRecord.PID))
	case status.RecordLive:
		add("运行记录", CheckOK, observed.record.Describe())
	case status.RecordedPID != 0:
		add("运行记录", CheckWarn, observed.record.Describe())
	default:
		add("运行记录", CheckOK, "不存在(尚未启动过服务)")
	}

	// The running instance and the configuration can name different checkouts:
	// --repo applies to one invocation, and a checkout can move while a server
	// keeps running. Saying so is what keeps a set of failures about the
	// configured directory from looking like a broken service.
	if running := status.RecordedRepoDir; running != "" && running != s.Settings.RepoDir {
		add("服务仓库", CheckWarn, fmt.Sprintf(
			"运行中的服务 (pid=%d) 来自 %s，配置中是 %s；用对应端口执行 dshctl stop 后再用 --repo %s start 可切换",
			status.ListenerPID, running, s.Settings.RepoDir, running))
	}
}

// doctorLock reports the operation lock.
func (s *Service) doctorLock(add func(string, string, string)) {
	holder, held, err := lock.Held(s.Settings.LockFile())
	switch {
	case err != nil:
		add("操作锁", CheckWarn, err.Error())
	case !held:
		add("操作锁", CheckOK, "空闲")
	case holder != 0:
		add("操作锁", CheckWarn, fmt.Sprintf("被 pid=%d 持有，另一个 dshctl 操作正在进行", holder))
	default:
		add("操作锁", CheckWarn, "已被持有，但锁文件里没有可读的 pid 记录")
	}
}

// ChecksFailed reports whether any check blocks an operation.
func ChecksFailed(checks []Check) bool {
	for _, check := range checks {
		if check.Status == CheckFail {
			return true
		}
	}
	return false
}

// humanBytes renders a byte count for people.
func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, name := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, name)
		}
	}
	return fmt.Sprintf("%.1f TB", value/unit)
}
