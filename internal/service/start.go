package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/detach"
	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
	"github.com/rhczz/dshctl/internal/logfile"
	"github.com/rhczz/dshctl/internal/nodejs"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

// WebURLPattern matches the address line `dsh web` prints on startup, token
// included. The address must be complete on its own line, which keeps a
// diagnostic that merely mentions a URL from being mistaken for the address.
var webURLPattern = regexp.MustCompile(`(?m)^dsh web:[ \t]+(https?://\S+)[ \t]*$`)

// StartResult describes what a start did.
type StartResult struct {
	// Status is the service state after the call.
	Status domain.Status `json:"status"`
	// AlreadyRunning reports that the server was up before the call.
	AlreadyRunning bool `json:"alreadyRunning"`
	// SpawnedPID is the process this call started, or 0.
	SpawnedPID int `json:"spawnedPid,omitempty"`
}

// Start launches the Web server in the background and waits for its port.
//
// Returns:
//   - nil when the server is already running or answered on the port.
//   - a Preflight error when a precondition is unmet, the port belongs to
//     something else, or the port cannot be inspected.
//   - a Failure error when the server did not come up.
//   - the context error when the call was cancelled.
func (s *Service) Start(ctx context.Context) (StartResult, error) {
	return withLockValue(ctx, s, func() (StartResult, error) { return s.startLocked(ctx) })
}

// startLocked performs the start while the caller holds the lock.
func (s *Service) startLocked(ctx context.Context) (StartResult, error) {
	observed, err := s.observe(ctx)
	if err != nil {
		return StartResult{}, err
	}
	verdict, observed, err := s.admitSurvivor(ctx, observed)
	if err != nil {
		return StartResult{}, err
	}
	if verdict == adoptFailed {
		return StartResult{}, exitcode.New(exitcode.Preflight,
			"检测到上次启动遗留的服务 (pid=%d)，但无法恢复运行记录;请手动结束它后重试",
			observed.status.ListenerPID)
	}
	if verdict == adoptDone {
		s.narrate(fmt.Sprintf("检测到上次启动被中断后仍存活的服务，已恢复管理: %s (pid=%d)", s.Settings.URL(), observed.status.ListenerPID))
		return StartResult{Status: observed.status, AlreadyRunning: true}, nil
	}
	switch observed.status.State {
	case domain.StateRunning:
		s.narrate(fmt.Sprintf("DSH Web 已在运行: %s (pid=%d)", s.Settings.URL(), observed.status.ListenerPID))
		s.reconcileRunningCheckout(observed)
		return StartResult{Status: observed.status, AlreadyRunning: true}, nil
	case domain.StateStarting:
		s.narrate(fmt.Sprintf("DSH Web 正在启动中: %s (pid=%d)", s.Settings.URL(), observed.status.ListenerPID))
		return StartResult{Status: observed.status}, nil
	case domain.StateForeign:
		return StartResult{}, exitcode.New(exitcode.Preflight,
			"端口 %d 被其他程序占用 (pid=%d: %s);请先停止它,或用 --port 换一个端口",
			s.boundPort(), observed.status.ListenerPID, observed.status.ListenerCommand)
	case domain.StateOrphan:
		// A survivor was already adopted above, so what is left is a process
		// whose ownership nothing can establish: telling the operator to end it
		// by hand is the only safe answer.
		return StartResult{}, exitcode.New(exitcode.Preflight,
			"端口 %d 上的进程 (pid=%d) 无法确认是不是 dshctl 启动的服务: %s\n"+
				"提示: 确认它可以安全停止后手动结束它,再重新启动;dshctl 不会主动结束无法确认归属的进程",
			s.boundPort(), observed.status.ListenerPID, observed.status.ListenerCommand)
	}

	if observed.hasRecord {
		if observed.status.RecordLive {
			// The record names a live server dshctl started, and that server is
			// not on this port. Starting a second one would leave two servers
			// with one record, so this is reported instead of done.
			return StartResult{}, exitcode.New(exitcode.Preflight,
				"运行记录中的服务 (pid=%d) 仍然存活，但它没有监听端口 %d\n"+
					"提示: 先运行 dshctl stop(会按记录结束它),或确认该进程可以安全结束后手动处理",
				observed.status.RecordedPID, s.boundPort())
		}
		// The record names a pid that is gone or has been recycled: clear it so
		// it cannot describe the server this call is about to start.
		if err := s.Record.Remove(); err != nil {
			s.warning(fmt.Sprintf("%v", err))
		}
	}
	return s.launch(ctx)
}

// adoptSurvivor rewrites the record to name the listener that is actually
// serving the port when the record names a process of the recorded start's
// group instead.
//
// The shape it recovers from is a start that was interrupted after the wrapper
// record was written but before the port answered: the record names the
// wrapper, and the server — a child in the wrapper's group — serves the port.
// Group membership is the same evidence waitForListening uses to accept a
// listener as this start's own: a live member of the recorded group cannot be
// a recycled stranger, because the group id cannot be reused while a member
// still holds it.
//
// Returns the adopted record and whether the adoption happened. A failure to
// write the record reports the survivor as unadoptable rather than pretending
// it was managed.
func (s *Service) adoptSurvivor(ctx context.Context, observed observed) (domain.Record, bool) {
	record := observed.record
	if !observed.hasRecord || record.SpawnedPID <= 0 || observed.status.ListenerPID <= 0 {
		return domain.Record{}, false
	}
	if !s.descendsFromSpawned(record.SpawnedPID, observed.status.ListenerPID) {
		return domain.Record{}, false
	}
	facts := s.Host.Inspect(ctx, observed.status.ListenerPID)
	if !facts.Alive {
		return domain.Record{}, false
	}
	adopted := domain.Record{
		PID:         observed.status.ListenerPID,
		SpawnedPID:  record.SpawnedPID,
		StartedAt:   facts.StartedAt,
		Port:        record.Port,
		Phase:       domain.PhaseRunning,
		URL:         s.urlFromLog(ctx),
		NodeVersion: record.NodeVersion,
		NodePath:    record.NodePath,
		RepoDir:     record.RepoDir,
	}
	if adopted.URL == "" {
		adopted.URL = record.URL
	}
	if err := s.Record.Save(adopted); err != nil {
		s.warning(fmt.Sprintf("无法收养上次启动遗留的服务 (pid=%d): %v", observed.status.ListenerPID, err))
		return domain.Record{}, false
	}
	return adopted, true
}

// launch runs the preconditions, spawns the server and waits for its port.
func (s *Service) launch(ctx context.Context) (StartResult, error) {
	installation, pnpm, err := s.preflight(ctx)
	if err != nil {
		return StartResult{}, err
	}

	if rotated, err := s.LogFile.RotateIfNeeded(); err != nil {
		return StartResult{}, exitcode.Wrap(exitcode.Failure, err)
	} else if rotated {
		s.narrate(fmt.Sprintf("日志已轮转: %s", s.LogFile.BackupPath()))
	}
	if err := s.LogFile.Section(sectionStart); err != nil {
		return StartResult{}, exitcode.Wrap(exitcode.Failure, err)
	}

	s.reportNodeOverride(installation)
	s.reportRepoOverride()

	handle, err := s.LogFile.OpenAppend()
	if err != nil {
		return StartResult{}, exitcode.Wrap(exitcode.Failure, err)
	}
	s.narrate(fmt.Sprintf("正在后台启动 DSH Web ... (日志: %s)", s.Settings.LogPath))
	pid, exited, spawnErr := s.spawn(pnpm, installation, handle)
	closeErr := handle.Close()
	if spawnErr != nil {
		s.note("start 失败: " + spawnErr.Error())
		return StartResult{}, exitcode.Wrap(exitcode.Failure, spawnErr)
	}
	if closeErr != nil {
		s.warning(fmt.Sprintf("关闭日志句柄时出错: %v", closeErr))
	}

	// A record is written as soon as the wrapper exists, before the port answers.
	//
	// It is the difference between a server dshctl can still clean up and one it
	// cannot: a crash during the readiness wait would otherwise leave a live
	// server on the port with no record at all, which every later command reports
	// as unmanageable. It names the wrapper — the leader of the group the server
	// lives in — and is replaced with the listener once the port answers.
	spawnedAt := s.processStartTime(ctx, pid, exited)
	if err := s.Record.Save(domain.Record{
		PID:         pid,
		SpawnedPID:  pid,
		StartedAt:   spawnedAt,
		Port:        s.boundPort(),
		Phase:       domain.PhaseRunning,
		NodeVersion: installation.Version,
		NodePath:    installation.NodePath,
		RepoDir:     s.Settings.RepoDir,
	}); err != nil {
		s.warning(fmt.Sprintf("无法记录启动的进程 (pid=%d): %v", pid, err))
	}

	listenerPID, err := s.waitForListening(ctx, pid, exited, s.Settings.StartTimeout)
	if err != nil {
		return StartResult{}, s.cleanupFailedStart(ctx, pid, err)
	}

	// The record now names the process that actually holds the port. Its start
	// time is the fingerprint every later ownership decision is checked against,
	// read when the server is past every exec it will do.
	//
	// A host that forbids reading process information still gets a working
	// dshctl: ownership then rests on the port check alone — the record's pid must
	// also be the process holding the port — which is enough to make a wrong kill
	// impossible, though it no longer detects pid reuse. That degradation is
	// reported rather than left to be discovered.
	startedAt := s.processStartTime(ctx, listenerPID, nil)
	if startedAt == 0 {
		s.warning(fmt.Sprintf("无法读取 DSH Web (pid=%d) 的进程启动时间，本次运行将只依据端口归属判断;"+
			"若系统复用了该 pid，dshctl 可能拒绝结束它(检查是否有安全策略限制读取进程信息)", listenerPID))
	}
	record := domain.Record{
		PID:         listenerPID,
		SpawnedPID:  pid,
		StartedAt:   startedAt,
		Port:        s.boundPort(),
		Phase:       domain.PhaseRunning,
		URL:         s.urlFromLog(ctx),
		NodeVersion: installation.Version,
		NodePath:    installation.NodePath,
		RepoDir:     s.Settings.RepoDir,
	}
	if err := s.Record.Save(record); err != nil {
		s.warning(fmt.Sprintf("无法更新运行记录: %v", err))
	}
	s.recordRuntime(installation)
	final, observeErr := s.observe(ctx)
	if observeErr != nil {
		return StartResult{}, observeErr
	}
	status := final.status
	s.narrate(fmt.Sprintf("启动成功: %s (pid=%d)", status.URL, status.ListenerPID))
	if record.URL != "" {
		s.narrate(fmt.Sprintf("访问地址: %s", record.URL))
	}
	return StartResult{Status: status, SpawnedPID: pid}, nil
}

// recordRuntime writes the facts this start established into the settings
// document: the checkout the server was started from and the Node release it
// started with. Each key is written only when the document decides nothing, so a
// document that names a checkout or a release belongs to the operator and is
// never rewritten from underneath them.
//
// The rule is what lets the first successful start decide the rest. Without it
// the checkout would be honoured for one invocation only: every later command
// resolves it from the configuration, so a service started with `--repo` would
// keep serving while `doctor`, `update` and a plain `start` talked about a
// directory nobody chose — the shape of a configuration that contradicts the
// service it manages.
//
// The server is already serving by the time this runs, so a document that cannot
// be written is a warning: stopping a working server because its configuration
// could not be updated would be worse than the missing line.
func (s *Service) recordRuntime(installation nodejs.Installation) {
	s.writeBack(s.Settings.RepoDir, installation.Version)
}

// writeBack records what a command established and reports each key it wrote.
//
// An empty value means "nothing to record for this key": the Node release is
// only ever written by a start, so a build records the checkout alone.
func (s *Service) writeBack(repoDir, nodeVersion string) {
	if repoDir == "" && nodeVersion == "" {
		return
	}
	wrote, err := s.Settings.RecordRuntime(repoDir, nodeVersion)
	if err != nil {
		s.warning(fmt.Sprintf("无法把本次运行的信息写入配置 %s: %v(以后仍会按既有设置重新解析)", s.Settings.ConfigPath, err))
		return
	}
	if wrote.RepoDir {
		message := fmt.Sprintf("已将仓库目录 %s 写入配置: %s", repoDir, s.Settings.ConfigPath)
		s.narrate(message)
		s.note(message)
	}
	if wrote.NodeVersion {
		message := fmt.Sprintf("已将 Node %s 写入配置: %s", nodeVersion, s.Settings.ConfigPath)
		s.narrate(message)
		s.note(message)
	}
}

// reconcileRunningCheckout closes the gap between the settings document and a
// service that is already running.
//
// A start that finds the port already served does not touch the running process,
// so the only useful thing it can do is make the configuration describe it. The
// checkout recorded for the running instance is the fact every later command
// needs — doctor, update and a plain start all resolve the checkout from the
// document — so it is written when the document decides nothing. Without that,
// a service started with `--repo` keeps serving while every later command
// operates on a directory that may not even exist.
//
// When the document does decide a checkout, nothing is rewritten: an operator's
// own value is not dshctl's to change. A running instance that disagrees with it
// is reported instead, because that disagreement is how the next command ends up
// acting on a different tree than the one being served.
func (s *Service) reconcileRunningCheckout(observed observed) {
	if s.Settings.ConfiguredRepoDir != "" {
		s.warnRunningCheckoutMismatch(observed)
		return
	}
	target := observed.record.RepoDir
	if target == "" && s.namesCheckoutItself() {
		// A record written before the checkout was recorded says nothing about
		// where the running service came from. An explicitly named checkout is
		// still worth recording — it is the one the next command should use —
		// but only once the directory proves to be a DeepSeek Harness checkout,
		// so a mistyped --repo cannot be written down as the operator's choice.
		if s.Repo.IsServerCheckout() {
			target = s.Settings.RepoDir
		}
	}
	if target == "" {
		return
	}
	s.writeBack(target, "")
}

// warnRunningCheckoutMismatch reports a running service that was started from a
// different checkout than the settings document names.
func (s *Service) warnRunningCheckoutMismatch(observed observed) {
	running := observed.record.RepoDir
	if running == "" || running == s.Settings.RepoDir {
		return
	}
	s.warning(fmt.Sprintf("运行中的服务 (pid=%d) 来自 %s，配置中的 repoDir 是 %s；两者操作的不是同一份 checkout", observed.status.ListenerPID, running, s.Settings.RepoDir))
}

// namesCheckoutItself reports whether this invocation names a checkout of its
// own — with --repo or DSH_REPO_DIR — rather than inheriting one from the
// configuration or the built-in default.
func (s *Service) namesCheckoutItself() bool {
	switch s.Settings.Sources.RepoDir {
	case "flag", "env":
		return true
	default:
		return false
	}
}

// reportRepoOverride tells the operator when this run uses a checkout other than
// the one the settings document names.
//
// --repo is deliberately a change to one run: it must not rewrite the document,
// so the only thing that makes it safe to use is saying out loud which run it
// applied to and how to make it permanent. Without that line the flag looks like
// it did nothing at all once the command ends — and the next command, which
// resolves the checkout from the document, silently operates on another tree.
func (s *Service) reportRepoOverride() {
	configured := s.Settings.ConfiguredRepoDir
	if configured == "" || configured == s.Settings.RepoDir {
		return
	}
	if s.Settings.Sources.RepoDir != "flag" {
		return
	}
	s.narrate(fmt.Sprintf("本次使用仓库 %s(配置中为 %s；如需固定请修改 %s)", s.Settings.RepoDir, configured, s.Settings.ConfigPath))
}

// warnOverriddenRepoDir reports a settings document whose checkout this run does
// not use.
//
// An environment variable is invisible once it is exported, so a worker shell
// that carries one must not silently move the service onto another checkout: the
// checkout in effect, the one the document names, and the fact that the two
// differ are all worth one line.
func (s *Service) warnOverriddenRepoDir() {
	if s.Settings.Sources.RepoDir != "env" {
		return
	}
	configured := s.Settings.ConfiguredRepoDir
	if configured == "" || configured == s.Settings.RepoDir {
		return
	}
	s.warning(fmt.Sprintf("环境变量 %s=%s 覆盖了配置里的 repoDir=%s，本次运行使用 %s", paths.EnvRepoDir, s.Settings.RepoDir, configured, s.Settings.RepoDir))
}

// reportNodeOverride tells the operator when this run uses a release other than
// the one the settings document names.
//
// --node is deliberately a change to one run: it must not rewrite the document,
// so the only thing that makes it safe to use is saying out loud which run it
// applied to and how to make it permanent. Without that line the flag looks like
// it did nothing at all once the command ends.
func (s *Service) reportNodeOverride(installation nodejs.Installation) {
	configured := s.Settings.ConfiguredNodeVersion
	if configured == "" || nodejs.Matches(installation.Version, configured) {
		return
	}
	s.narrate(fmt.Sprintf("本次使用 Node %s(配置中为 %s；如需固定请修改 %s)", installation.Version, configured, s.Settings.ConfigPath))
}

// spawn starts the detached server with the log as its output.
func (s *Service) spawn(pnpm string, installation nodejs.Installation, log *os.File) (int, func(context.Context) bool, error) {
	spawn := s.Spawn
	if spawn == nil {
		spawn = spawnDetached
	}
	args := []string{"--dir", s.Settings.RepoDir, "dsh", "web",
		"--port", strconv.Itoa(s.boundPort()), "--no-open"}
	return spawn(pnpm, args, s.Settings.RepoDir, run.WithPathPrefix(installation.BinDir), log)
}

// spawnDetached starts the server in its own session with the log as its output.
//
// Standard input is not inherited: a background server must not hold the
// terminal dshctl was started from.
func spawnDetached(path string, args []string, dir string, env []string, log *os.File) (int, func(context.Context) bool, error) {
	command := detach.Command(path, args...)
	command.Dir = dir
	command.Env = env
	command.Stdin = nil
	command.Stdout = log
	command.Stderr = log
	process, err := detach.Start(command)
	if err != nil {
		return 0, nil, err
	}
	return process.PID, process.Exited, nil
}

// cleanupFailedStart ends everything this attempt created and reports why.
//
// It works on the process group, not on the pid it spawned: the harness is
// launched through a package script, so the process holding the port is a child
// of the wrapper, and ending only the wrapper leaves the server serving with
// nothing able to manage it. The port is checked afterwards — the cleanup is
// only reported as done once the port is actually free.
func (s *Service) cleanupFailedStart(ctx context.Context, pid int, cause error) error {
	s.failure("启动失败或超时，正在清理本次启动的进程 ...")
	s.note("start 失败")

	if err := s.endGroup(ctx, pid); err != nil {
		s.warning(fmt.Sprintf("%v", err))
	}

	// A record that named this attempt must go, and it must go whichever pid it
	// ended up naming.
	if record, ok, err := s.Record.Load(); err == nil && ok &&
		(record.PID == pid || record.SpawnedPID == pid) {
		if err := s.Record.Remove(); err != nil {
			s.warning(fmt.Sprintf("%v", err))
		}
	}

	if err := s.waitForStopped(ctx, s.Settings.StopTimeout); err != nil {
		// The port is still held, so nothing was really cleaned up. Saying so is
		// the difference between a recoverable state and a mystery.
		s.failure(fmt.Sprintf("端口 %d 仍被占用，本次启动的进程没有全部退出: %v", s.boundPort(), err))
	}

	s.failure("已清理。日志尾部:")
	_, _ = logfile.Tail(s.Settings.LogPath, startTailLines, s.emitter().Diagnostics())
	return exitcode.Wrap(exitcode.Failure, fmt.Errorf("%w(日志: %s)", cause, s.Settings.LogPath))
}

// endGroup asks every process this start created to exit, then forces what is
// left.
//
// A group id is only meaningful while a member of the group is alive, so the
// whole operation is skipped when the group is already gone: signalling a
// recycled group id would reach an unrelated tree. This is also why a stop
// checks the group before ending it, days after the start: the wrapper's pid,
// which is the group id, may have been handed to somebody else in the meantime.
func (s *Service) endGroup(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	if !s.Host.GroupExists(pid) {
		return nil
	}
	if err := s.Host.SignalGroup(pid, hostGraceful); err != nil {
		s.warning(fmt.Sprintf("%v", err))
	}
	if s.waitForGroupExit(ctx, pid, s.grace) {
		return nil
	}
	if err := s.Host.KillGroup(pid); err != nil {
		return err
	}
	if !s.waitForGroupExit(ctx, pid, s.grace) {
		return fmt.Errorf("本次启动的进程树 (组长 %d) 在强制结束后仍然存在", pid)
	}
	return nil
}

// waitForGroupExit polls until no process is left in a tree.
//
// The question is "does the tree still exist", which the root's own exit does
// not answer: the wrapper may die first and leave the server alive in it. A
// group lookup through the leader returns nothing the moment the leader is
// reaped and would skip the force step with the server still holding the port,
// so GroupExists asks the kernel directly instead.
func (s *Service) waitForGroupExit(ctx context.Context, pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !s.Host.GroupExists(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// preflight verifies every precondition before anything is spawned.
func (s *Service) preflight(ctx context.Context) (nodejs.Installation, string, error) {
	if !s.Repo.Exists() {
		return nodejs.Installation{}, "", exitcode.New(exitcode.Preflight,
			"仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
			s.Settings.RepoDir, paths.EnvRepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return nodejs.Installation{}, "", exitcode.New(exitcode.Preflight,
			"%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)\n提示: 用 --repo 指向正确的 checkout",
			s.Settings.RepoDir, config.ServerManifestRel, config.WorkspaceManifestRel)
	}
	installation, err := s.resolveNode(ctx)
	if err != nil {
		return nodejs.Installation{}, "", err
	}
	pnpm, err := s.pnpmPath()
	if err != nil {
		return nodejs.Installation{}, "", err
	}
	if !s.Repo.BuildReady() {
		return nodejs.Installation{}, "", exitcode.New(exitcode.Preflight,
			"仓库尚未构建(缺少 %s 或 node_modules)\n提示: 先运行 dshctl build, 再执行 dshctl start",
			s.Repo.BuildRecordPath())
	}
	return installation, pnpm, nil
}

// resolveNode finds the runtime this call must use and reports what the operator
// needs to know about it.
//
// The order of the two judgments matters. A release below the minimum is refused
// whatever named it — the configuration, --node, the environment or PATH —
// because dshctl cannot serve the Web client with it. A release in a major
// version dshctl has not been verified against is used, and said out loud: the
// failure it can cause is visible only in the browser.
func (s *Service) resolveNode(ctx context.Context) (nodejs.Installation, error) {
	// Version managers live in the operating-system home directory — ~/.nvm,
	// ~/.local/share/fnm — not under $DSH_HOME. Searching the harness home found
	// nothing on any machine that keeps its runtimes in the usual place.
	home, err := paths.Home()
	if err != nil {
		return nodejs.Installation{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	installation, failure := s.Node.Resolve(ctx, nodejs.Preferences{
		Version: s.Settings.NodeVersion,
		Home:    home,
	})
	if failure != nil {
		return nodejs.Installation{}, exitcode.New(exitcode.Preflight, "%s",
			nodejs.Describe(failure, config.MinNodeVersion, config.TestedNodeVersion))
	}

	switch verdict := nodejs.Assess(installation, config.MinNodeVersion, config.TestedNodeVersion); verdict.Status {
	case nodejs.TooOld:
		return nodejs.Installation{}, exitcode.New(exitcode.Preflight, "%s\n%s", verdict.Reason, verdict.Remedy)
	case nodejs.Untested:
		s.warning(fmt.Sprintf("%s", verdict.Reason))
	}
	s.warnOverriddenNodeVersion(installation)
	s.warnOverriddenRepoDir()
	return installation, nil
}

// warnOverriddenNodeVersion reports a settings document whose release this run
// does not use.
//
// An environment variable is invisible once it is exported, so a worker shell
// that carries one must not silently move the service onto another runtime: the
// release in effect, the one the document names, and the command that fixes the
// difference are all worth one line. The flag does not need this — it was typed
// for this invocation, and start reports it on its own.
func (s *Service) warnOverriddenNodeVersion(installation nodejs.Installation) {
	if s.Settings.Sources.NodeVersion != "env" {
		return
	}
	configured := s.Settings.ConfiguredNodeVersion
	if configured == "" || nodejs.Matches(installation.Version, configured) {
		return
	}
	s.warning(fmt.Sprintf("环境变量 %s=%s 覆盖了配置里的 nodeVersion=%s，本次运行使用 %s", paths.EnvNodeVersion, s.Settings.NodeVersion, configured, installation.Version))
}

// pnpmPath resolves the pnpm executable.
func (s *Service) pnpmPath() (string, error) {
	lookPath := s.LookPath
	if lookPath == nil {
		lookPath = run.LookPath
	}
	path, err := lookPath("pnpm")
	if err != nil {
		return "", exitcode.New(exitcode.Preflight, "找不到 pnpm，请先安装并确保它在 PATH 中")
	}
	return path, nil
}

// processStartTime reads the start time of a freshly spawned process.
//
// The budget is generous on purpose: this value is the fingerprint every later
// ownership decision rests on, so waiting a moment for it is cheaper than
// proceeding without it. The wait itself is not the contract — reaching the
// degraded mode after the budget is — so a test may shorten the budget through
// the service field while the production value stays as pinned by
// TestFingerprintTimeoutIsGenerous.
func (s *Service) processStartTime(ctx context.Context, pid int, exited func(context.Context) bool) int64 {
	deadline := time.Now().Add(s.fingerprintBudget())
	for {
		// A process that has already ended will never yield a start time, and
		// waiting the whole budget for one would turn a fast failure into a slow
		// one.
		if exited != nil && exited(ctx) {
			return 0
		}
		facts := s.Host.Inspect(ctx, pid)
		if facts.StartedAt > 0 {
			return facts.StartedAt
		}
		if time.Now().After(deadline) {
			return 0
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// urlFromLog reads the address this server announced.
//
// The log is shared by every port in the state directory, so the last line in it
// may belong to a different instance: the address is accepted only when it names
// the port this service runs on. The address is then stored in the runtime
// record, so `dshctl url` keeps working after the log rotates past the line that
// carried it.
//
// A log larger than the scan window is reported rather than silently yielding
// no address: not being able to look is not the same as there being nothing.
func (s *Service) urlFromLog(ctx context.Context) string {
	if err := ctx.Err(); err != nil {
		return ""
	}
	address, truncated := announcedURL(s.Settings.LogPath, s.boundPort())
	if address == "" && truncated {
		s.warning(fmt.Sprintf("日志过大，未能在其中找到本次启动公布的访问地址;可用 dshctl logs 查看或等待服务输出"))
	}
	return address
}

// announcedURL returns the last address the log announced for port, and whether
// an older address may exist outside the scan window.
//
// Matching the whole pattern is not enough: the shared log holds the addresses
// of every instance, so the port is compared too. An address for another port is
// not this service's address, and reporting it would hand the operator a token
// for a different server.
func announcedURL(logPath string, port int) (string, bool) {
	matches, truncated, err := logfile.AllMatches(logPath, webURLPattern)
	if err != nil {
		return "", false
	}
	for index := len(matches) - 1; index >= 0; index-- {
		if addressPort(matches[index]) == port {
			return matches[index], truncated
		}
	}
	return "", truncated
}

// addressPort reads the port out of an announced address.
func addressPort(address string) int {
	parsed, err := url.Parse(address)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return 0
	}
	return port
}

// fallback returns value when set, otherwise alternative.
func fallback(value, alternative string) string {
	if strings.TrimSpace(value) == "" {
		return alternative
	}
	return value
}

// startTailLines is how much of the log a failed start prints for diagnosis.
const startTailLines = 15

// hostGraceful and hostForce name the two termination strengths.
const (
	hostGraceful = host.Graceful
	hostForce    = host.Force
)
