package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/repo"
	"github.com/rhczz/dshctl/internal/run"
)

// latestTarget is the selector that means origin/master's tip.
const latestTarget = "latest"

// deployRequest is one version move: what to move to, and how to name it.
type deployRequest struct {
	// verb names the operation in messages ("更新" / "回退").
	verb string
	// section is the log section title the move is recorded under.
	section string
	// target is the selector the operator gave: latest, a tag, a sha. Empty
	// means the target comes from the recorded history instead.
	target string
	// steps is how many recorded positions to walk back; used when target is
	// empty.
	steps int
	// fetch asks the remote before resolving. A rollback never does: returning
	// to a known position has to work without a network.
	fetch bool
}

// deployTarget is a resolved version to move to.
type deployTarget struct {
	// commit is the full revision to move to.
	commit string
	// selector is what the operator asked for, recorded verbatim.
	selector string
	// name names the target in messages.
	name string
	// latest marks the remote tip: the switch returns to the master branch and
	// fast-forwards instead of detaching.
	latest bool
}

// RunUpdate moves the checkout to the requested version, reinstalls
// dependencies, rebuilds, and restores the previous running state.
//
// Failure semantics, each covered by a test:
//
//   - the target cannot be resolved, or the worktree has tracked changes: the
//     service was never stopped, and the old build still serves.
//   - the switch fails: the old build still serves, and the service is restored
//     before the original error is reported.
//   - pnpm install or build fails: the service stays down with the reason in
//     the log, and the recorded history already names the new position, so
//     rollback can return.
func (s *Service) RunUpdate(ctx context.Context, target string) error {
	if strings.TrimSpace(target) == "" {
		target = latestTarget
	}
	return s.withLock(ctx, func() error {
		return s.deployLocked(ctx, deployRequest{
			verb: "更新", section: "update", target: target, fetch: true,
		})
	})
}

// RunRollback returns the checkout to an earlier deployed position.
//
// The target is either the n-th position back in the recorded stack (the
// default is one step) or a named version. It never fetches: returning to a
// known position is the firefighting path, and it has to work without a
// network.
func (s *Service) RunRollback(ctx context.Context, target string, steps int) error {
	return s.withLock(ctx, func() error {
		return s.deployLocked(ctx, deployRequest{
			verb: "回退", section: "rollback", target: target, steps: steps,
		})
	})
}

// deployLocked performs one version move while the caller holds the lock.
//
// The order is the contract: every check that can fail without touching the
// checkout runs before the service is stopped, so a typo in a version never
// takes a serving instance down.
func (s *Service) deployLocked(ctx context.Context, request deployRequest) error {
	observed, err := s.observe(ctx)
	if err != nil {
		return err
	}
	// A survivor of an interrupted start is adopted first: a move is not
	// blocked by a record that simply has not caught up with reality.
	if observed.status.Survivor {
		if _, ok := s.adoptSurvivor(ctx, observed); !ok {
			return exitcode.New(exitcode.Preflight,
				"检测到上次启动遗留的服务 (pid=%d)，但无法恢复运行记录;请先运行 dshctl stop 或手动处理",
				observed.status.ListenerPID)
		}
		fmt.Fprintln(s.Out, "检测到上次启动被中断后仍存活的服务，已恢复管理")
		if observed, err = s.observe(ctx); err != nil {
			return err
		}
	}
	if observed.status.State == StateForeign || observed.status.State == StateOrphan {
		return exitcode.New(exitcode.Preflight,
			"端口 %d 被 dshctl 无法确认归属的进程占用 (pid=%d): %s\n提示: 先确认并停止它,再执行%s",
			s.Settings.Port, observed.status.ListenerPID, observed.status.ListenerCommand, request.verb)
	}

	// The checkout is shared. This port's server is stopped by the move itself
	// and restarted afterwards; a server on another port is not, so its
	// artifacts would be replaced underneath it — that is refused.
	serving, err := s.serversUsingCheckout(ctx, observed)
	if err != nil {
		return err
	}
	var elsewhere servingPorts
	wasRunning := false
	for _, entry := range serving {
		if entry.port == s.Settings.Port {
			wasRunning = true
			continue
		}
		elsewhere = append(elsewhere, entry)
	}
	if len(elsewhere) > 0 {
		return exitcode.New(exitcode.Preflight,
			"仓库 %s 还被端口 %v 上的服务使用 (pid %v),%s会替换它正在使用的构建产物\n"+
				"提示: 先停止那些服务(可用对应的 --port 运行 dshctl stop),再执行%s",
			s.Settings.RepoDir, elsewhere.ports(), elsewhere.pids(), request.verb, request.verb)
	}

	if !s.Repo.Exists() {
		return exitcode.New(exitcode.Preflight,
			"仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
			s.Settings.RepoDir, paths.EnvRepoDir)
	}
	if !s.Repo.IsGit() {
		return exitcode.New(exitcode.Preflight, "%s 不是 git 仓库", s.Settings.RepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return exitcode.New(exitcode.Preflight,
			"%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)",
			s.Settings.RepoDir, configServerManifest, configWorkspaceManifest)
	}
	pnpm, err := s.pnpmPath()
	if err != nil {
		return err
	}
	installation, err := s.resolveNode(ctx)
	if err != nil {
		return err
	}
	s.reportRepoOverride()
	env := run.WithPathPrefix(installation.BinDir)

	target, err := s.resolveDeployTarget(ctx, request)
	if err != nil {
		return err
	}
	current, err := s.Repo.HeadCommit(ctx)
	if err != nil {
		return exitcode.Wrap(exitcode.Preflight, err)
	}
	if dirty, err := s.Repo.TrackedChanges(ctx); err != nil {
		// "Cannot look" must not be read as "clean": switching a tree whose
		// state could not be determined could discard the operator's work.
		return exitcode.Wrap(exitcode.Preflight, err)
	} else if dirty {
		return exitcode.New(exitcode.Preflight,
			"仓库 %s 有已跟踪文件的未提交修改，不能切换版本\n"+
				"提示: 先运行 git -C %s status 查看并处理（未跟踪文件不受影响）",
			s.Settings.RepoDir, s.Settings.RepoDir)
	}
	if target.commit == current {
		fmt.Fprintf(s.Out, "已在 %s（%s），无需%s\n", shortCommit(current), target.name, request.verb)
		return nil
	}
	s.warnWhenOutsideOrigin(ctx, target)

	if wasRunning {
		// The move stops this port's server because it restarts it. That is
		// only allowed once the identity is verified: it must not rewrite the
		// checkout underneath a process it cannot safely end.
		if _, ok := s.stopTarget(ctx, observed); !ok {
			return exitcode.New(exitcode.Preflight,
				"运行记录中的服务 (pid=%d) 是否属于本次启动无法验证(平台读不到进程启动时间)，不能安全地结束它\n"+
					"提示: 确认该进程可以停止后手动结束它，或用 --port 换一个端口",
				observed.status.RecordedPID)
		}
		fmt.Fprintln(s.Out, "DSH Web 正在运行，先停止服务 ...")
		if _, err := s.stopLocked(ctx); err != nil {
			return err
		}
	}

	// Rotate before the section marker is written, so a marker and its body can
	// never end up in different files.
	if rotated, err := s.Log.RotateIfNeeded(); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	} else if rotated {
		fmt.Fprintf(s.Out, "日志已轮转: %s\n", s.Log.BackupPath())
	}
	if err := s.Log.Section(request.section); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}

	fmt.Fprintf(s.Out, "%s: %s → %s（%s）\n",
		request.verb, shortCommit(current), shortCommit(target.commit), target.name)
	if err := s.switchToTarget(ctx, target); err != nil {
		// A half-completed latest (the checkout to master succeeded, the
		// merge failed) has already moved the tree; the record must not claim
		// otherwise, or a later rollback would step to the wrong position.
		if moved, headErr := s.Repo.HeadCommit(ctx); headErr == nil && moved != current {
			if recordErr := s.recordDeploy(ctx, current, deployTarget{commit: moved, name: "master"}); recordErr != nil {
				s.warn("%v", recordErr)
			}
		}
		s.errorf("错误: %s失败: %v", request.verb, err)
		if wasRunning {
			fmt.Fprintln(s.Out, "仓库旧构建仍然完好，恢复启动旧版本 ...")
			if _, startErr := s.startLocked(ctx); startErr != nil {
				s.errorf("恢复启动失败: %v", startErr)
			}
		}
		return exitcode.Wrap(exitcode.Failure, fmt.Errorf("%s失败: %w", request.verb, err))
	}

	// The move is a fact on disk the moment the switch returns: record it
	// before the build, so a failed install or build still leaves a history
	// rollback can return from.
	historyErr := s.recordDeploy(ctx, current, target)

	if err := s.prune(ctx); err != nil {
		return err
	}

	fmt.Fprintln(s.Out, "--- pnpm install ---")
	if err := s.stream(ctx, run.Command{
		Name: pnpm,
		Args: []string{"install"},
		Dir:  s.Settings.RepoDir,
		Env:  env,
	}); err != nil {
		s.note("pnpm install 失败")
		return exitcode.Wrap(exitcode.Failure, fmt.Errorf("pnpm install 失败: %w\n%s", err, shutdownMessage))
	}
	s.note("pnpm install 成功")

	fmt.Fprintln(s.Out, "--- pnpm run build ---")
	if err := s.stream(ctx, run.Command{
		Name: pnpm,
		Args: []string{"run", "build"},
		Dir:  s.Settings.RepoDir,
		Env:  env,
	}); err != nil {
		s.note("pnpm run build 失败")
		return exitcode.Wrap(exitcode.Failure, fmt.Errorf("pnpm run build 失败: %w\n%s", err, shutdownMessage))
	}
	s.note("pnpm run build 成功")

	fmt.Fprintf(s.Out, "%s完成\n", request.verb)
	if wasRunning {
		fmt.Fprintln(s.Out, "恢复启动 DSH Web ...")
		if _, err := s.startLocked(ctx); err != nil {
			return err
		}
	}
	// The move ran against this checkout; when the document decides none, that
	// is the checkout every later command has to resolve.
	s.writeBack(s.Settings.RepoDir, "")
	if historyErr != nil {
		return exitcode.Wrap(exitcode.Failure, historyErr)
	}
	return nil
}

// resolveDeployTarget turns a request into the commit to move to.
func (s *Service) resolveDeployTarget(ctx context.Context, request deployRequest) (deployTarget, error) {
	if request.steps > 0 {
		return s.rollbackTarget(ctx, request.steps)
	}
	if request.target == latestTarget {
		if err := s.Repo.Fetch(ctx, nil, nil); err != nil {
			// latest cannot be resolved from local state: the whole point is
			// the remote's tip.
			return deployTarget{}, exitcode.Wrap(exitcode.Failure, err)
		}
		tip, err := s.Repo.RemoteTip(ctx)
		if err != nil {
			return deployTarget{}, exitcode.Wrap(exitcode.Preflight, err)
		}
		return deployTarget{
			commit: tip, selector: latestTarget, name: repo.RemoteTipName, latest: true,
		}, nil
	}
	if request.fetch {
		// A named version is useful offline when it is already known locally:
		// a failed fetch is a warning, not a refusal.
		if err := s.Repo.Fetch(ctx, nil, nil); err != nil {
			s.warn("无法获取远程更新，按本地已知状态解析 %q: %v", request.target, err)
		}
	}
	commit, err := s.Repo.ResolveRevision(ctx, request.target)
	if err != nil {
		return deployTarget{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	return deployTarget{commit: commit, selector: request.target, name: request.target}, nil
}

// rollbackTarget resolves the position a step count names.
//
// The stack is the recorded positions for this checkout, and the current
// commit is its top: step 1 is where the previous move started, which is what
// a bare `dshctl rollback` means. A corrupt history refuses the rollback —
// unlike an update, there is nothing the operator asked for that could be
// carried out without it.
func (s *Service) rollbackTarget(ctx context.Context, steps int) (deployTarget, error) {
	current, err := s.Repo.HeadCommit(ctx)
	if err != nil {
		return deployTarget{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	store := history.Store{Path: filepath.Join(s.Settings.StateDir, historyFileName)}
	file, ok, err := store.Load()
	if err != nil {
		return deployTarget{}, exitcode.New(exitcode.Preflight,
			"更新历史无法读取: %v\n提示: 删除 %s 后可用 dshctl update <版本> 定点切换", err, store.Path)
	}
	if !ok {
		return deployTarget{}, exitcode.New(exitcode.Preflight,
			"没有可回退的历史: dshctl 还没有记录过这个 checkout 的部署位置\n"+
				"提示: 用 dshctl timeline 查看版本，用 dshctl update <版本> 定点切换")
	}
	records := file.Records(s.Settings.RepoDir)
	now := history.Record{Commit: current, At: time.Now().Unix()}
	position, ok := history.Step(records, now, steps)
	if !ok {
		return deployTarget{}, exitcode.New(exitcode.Preflight,
			"没有可回退的位置: 历史里最多还能退 %d 步", len(history.Visit(records, now))-1)
	}
	name := "记录中的位置"
	if tags, err := s.Repo.Tags(ctx); err == nil {
		if tag := firstTag(tags[position.Commit]); tag != "" {
			name = tag
		}
	}
	return deployTarget{
		commit:   position.Commit,
		selector: fmt.Sprintf("-n %d", steps),
		name:     name,
	}, nil
}

// switchToTarget moves the checkout to the resolved target, streaming git's
// words to the console and the log.
func (s *Service) switchToTarget(ctx context.Context, target deployTarget) error {
	handle, err := s.Log.OpenAppend()
	if err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	defer handle.Close()
	out := io.MultiWriter(s.Out, handle)
	errOut := io.MultiWriter(s.Err, handle)
	if target.latest {
		return s.Repo.FastForwardMaster(ctx, out, errOut)
	}
	return s.Repo.CheckoutDetach(ctx, target.commit, out, errOut)
}

// warnWhenOutsideOrigin names a target origin/master cannot reach. It is a
// warning, not a refusal: an unmerged release branch is a legitimate thing to
// deploy.
func (s *Service) warnWhenOutsideOrigin(ctx context.Context, target deployTarget) {
	if target.latest {
		return
	}
	tip, err := s.Repo.RemoteTip(ctx)
	if err != nil {
		// Cannot look: no claim either way.
		return
	}
	ok, err := s.Repo.IsAncestor(ctx, target.commit, tip)
	if err != nil || ok {
		return
	}
	s.warn("目标 %s 不在 %s 的历史上（可能来自未合并的分支或本地提交）",
		shortCommit(target.commit), repo.RemoteTipName)
}

// recordDeploy writes the move into the deployment history: where the tree was
// and where it went. A corrupt history is rebuilt from the move itself rather
// than blocking a deployment, and the caller is told.
func (s *Service) recordDeploy(ctx context.Context, before string, target deployTarget) error {
	store := history.Store{Path: filepath.Join(s.Settings.StateDir, historyFileName)}
	file, _, err := store.Load()
	if err != nil {
		s.warn("更新历史无法读取(%v)，将以当前版本重建", err)
		file = history.File{}
	}
	records := file.Records(s.Settings.RepoDir)
	now := time.Now().Unix()
	if before != "" && (len(records) == 0 || records[0].Commit != before) {
		// The starting point is recorded only when the stack does not already
		// name it: it is what a bare rollback returns to.
		records = history.Visit(records, history.Record{Commit: before, At: now})
	}
	records = history.Visit(records, history.Record{
		Commit: target.commit, Selector: target.selector, At: now,
	})
	if err := store.Save(file.With(s.Settings.RepoDir, records)); err != nil {
		return fmt.Errorf("更新历史未写入: %w", err)
	}
	return nil
}
