package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

// shutdownMessage explains a service left stopped after a failed deployment.
// shutdownMessage explains a service left stopped after a failed deployment.
//
// It is a function, not a variable: a package-level i18nLine would run at init
// time, before the shell installs the language, and every message would render
// as its own id.
func shutdownMessage() string { return i18nLine(MsgShutdownMessage) }

// deployRequest is one version move: what to move to, and how to name it.
type deployRequest struct {
	// verb names the operation in messages.
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
		target = domain.Latest
	}
	return s.withLock(ctx, func() error {
		return s.deployLocked(ctx, deployRequest{
			verb: i18nLine(MsgUpdateVerb), section: sectionUpdate, target: target, fetch: true,
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
			verb: i18nLine(MsgRollbackVerb), section: sectionRollback, target: target, steps: steps,
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
	verdict, observed, err := s.admitSurvivor(ctx, observed)
	if err != nil {
		return err
	}
	if verdict == adoptFailed {
		return exitcode.New(exitcode.Preflight, i18nLine(MsgUpdateAdoptFailed),
			observed.status.ListenerPID)
	}
	if verdict == adoptDone {
		s.narrate(i18nLine(MsgUpdateAdopted))
	}
	if observed.occupant() {
		return exitcode.New(exitcode.Preflight, i18nLine(MsgUpdateOccupant),
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
			i18nLine(MsgRepoUsedByOtherPorts),
			s.Settings.RepoDir, elsewhere.ports(), elsewhere.pids(), request.verb, request.verb)
	}

	if !s.Repo.Exists() {
		return exitcode.New(exitcode.Preflight,
			i18nLine(MsgUpdateRepoMissing),
			s.Settings.RepoDir, paths.EnvRepoDir)
	}
	if !s.Repo.IsGit() {
		return exitcode.New(exitcode.Preflight, i18nLine(MsgUpdateNotGit), s.Settings.RepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return exitcode.New(exitcode.Preflight,
			i18nLine(MsgUpdateNotCheckout),
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

	var target domain.Target
	if err := s.Log.Step(i18nLine(MsgUpdateResolveTarget), func() error {
		resolved, err := s.resolveDeployTarget(ctx, request)
		target = resolved
		return err
	}); err != nil {
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
		return exitcode.New(exitcode.Preflight, i18nLine(MsgRepoTrackedChanges),
			s.Settings.RepoDir, s.Settings.RepoDir)
	}
	s.Log.Debug(fmt.Sprintf("%s: %s -> %s (selector=%q fetch=%v)",
		request.verb, domain.ShortCommit(current), domain.ShortCommit(target.Commit), request.target, request.fetch))
	if target.Commit == current {
		s.narrate(i18nLine(MsgUpdateNoOp, target.Label(), request.verb))
		// A no-op is still a successful run against this checkout, and the
		// document records the checkout a successful run used.
		s.writeBack(s.Settings.RepoDir, "")
		return nil
	}
	s.warnWhenOutsideOrigin(ctx, target)

	if wasRunning {
		// The move stops this port's server because it restarts it. That is
		// only allowed once the identity is verified: it must not rewrite the
		// checkout underneath a process it cannot safely end.
		if _, ok := s.stopTarget(ctx, observed); !ok {
			return exitcode.New(exitcode.Preflight,
				i18nLine(MsgUpdateUnverifiable)+"\n"+i18nLine(MsgUpdateUnverifiableTip),
				observed.status.RecordedPID)
		}
		s.narrate(i18nLine(MsgUpdateStopping))
		if _, err := s.stopLocked(ctx); err != nil {
			return err
		}
	}

	// Rotate before the section marker is written, so a marker and its body can
	// never end up in different files.
	if rotated, err := s.LogFile.RotateIfNeeded(); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	} else if rotated {
		s.narrate(i18nLine(MsgBuildLogRotated, s.LogFile.BackupPath()))
	}
	if err := s.LogFile.Section(request.section); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}

	s.narrate(fmt.Sprintf("%s: %s → %s", request.verb, domain.ShortCommit(current), target.Label()))
	if err := s.switchToTarget(ctx, target); err != nil {
		// A half-completed latest (the checkout to master succeeded, the
		// merge failed) has already moved the tree; the record must not claim
		// otherwise, or a later rollback would step to the wrong position.
		if moved, headErr := s.Repo.HeadCommit(ctx); headErr == nil && moved != current {
			if recordErr := s.recordDeploy(ctx, current, domain.Target{Commit: moved, Name: "master"}); recordErr != nil {
				s.warning(fmt.Sprintf("%v", recordErr))
			}
		}
		s.failure(i18nLine(MsgUpdateSwitchFailed, request.verb, err))
		if wasRunning {
			s.narrate(i18nLine(MsgUpdateRestore))
			if _, startErr := s.startLocked(ctx); startErr != nil {
				s.failure(i18nLine(MsgUpdateRestoreFailed, startErr))
			}
		}
		return exitcode.Wrap(exitcode.Failure, fmt.Errorf("%s", i18nLine(MsgUpdateFailed, request.verb, err)))
	}

	// The move is a fact on disk the moment the switch returns: record it
	// before the build, so a failed install or build still leaves a history
	// rollback can return from.
	historyErr := s.recordDeploy(ctx, current, target)

	if err := s.prune(ctx); err != nil {
		return err
	}

	s.narrate("--- pnpm install ---")
	installErr := s.Log.Step("pnpm install", func() error {
		return s.stream(ctx, run.Command{
			Name: pnpm,
			Args: []string{"install"},
			Dir:  s.Settings.RepoDir,
			Env:  env,
		})
	})
	if installErr != nil {
		s.note(i18nLine(MsgInstallFailedNote))
		return exitcode.Wrap(exitcode.Failure,
			fmt.Errorf("%s: %w\n%s", i18nLine(MsgInstallFailedNote), installErr, shutdownMessage()))
	}
	s.note(i18nLine(MsgInstallSucceeded))

	s.narrate("--- pnpm run build ---")
	buildErr := s.Log.Step("pnpm build", func() error {
		return s.stream(ctx, run.Command{
			Name: pnpm,
			Args: []string{"run", "build"},
			Dir:  s.Settings.RepoDir,
			Env:  env,
		})
	})
	if buildErr != nil {
		s.note(i18nLine(MsgBuildFailedNote))
		return exitcode.Wrap(exitcode.Failure,
			fmt.Errorf("%s: %w\n%s", i18nLine(MsgBuildFailedNote), buildErr, shutdownMessage()))
	}
	s.note(i18nLine(MsgBuildSucceededNote))

	s.narrate(i18nLine(MsgMoveDone, request.verb))
	if wasRunning {
		s.narrate(i18nLine(MsgUpdateRestarting))
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
func (s *Service) resolveDeployTarget(ctx context.Context, request deployRequest) (domain.Target, error) {
	if request.steps > 0 {
		return s.rollbackTarget(ctx, request.steps)
	}
	if request.target == domain.Latest {
		hasOrigin, err := s.Repo.HasOrigin(ctx)
		if err != nil {
			return domain.Target{}, exitcode.Wrap(exitcode.Preflight, err)
		}
		if !hasOrigin {
			return domain.Target{}, exitcode.New(exitcode.Preflight,
				i18nLine(MsgUpdateNoOriginLatest),
				s.Settings.RepoDir)
		}
		if err := s.Repo.Fetch(ctx, nil, nil); err != nil {
			// latest cannot be resolved from local state: the whole point is
			// the remote's tip.
			return domain.Target{}, exitcode.Wrap(exitcode.Failure, err)
		}
		tip, err := s.Repo.RemoteTip(ctx)
		if err != nil {
			return domain.Target{}, exitcode.Wrap(exitcode.Preflight, err)
		}
		return domain.Target{
			Commit: tip, Selector: domain.Latest, Name: s.Repo.RemoteTipName(), Latest: true,
		}, nil
	}
	if request.fetch {
		// A named version is useful offline when it is already known locally:
		// a failed fetch is a warning, not a refusal.
		if err := s.Repo.Fetch(ctx, nil, nil); err != nil {
			s.warning(fmt.Sprintf("%s: %v", i18nLine(MsgUpdateFetchFailed, request.target), err))
		}
	}
	commit, err := s.Repo.ResolveRevision(ctx, request.target)
	if err != nil {
		return domain.Target{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	name := request.target
	if strings.HasPrefix(commit, request.target) {
		// The selector is an abbreviation of the commit, so repeating it in
		// parentheses says nothing.
		name = ""
	}
	return domain.Target{Commit: commit, Selector: request.target, Name: name}, nil
}

// rollbackTarget resolves the position a step count names.
//
// The stack is the recorded positions for this checkout, and the current
// commit is its top: step 1 is where the previous move started, which is what
// a bare `dshctl rollback` means. A corrupt history refuses the rollback —
// unlike an update, there is nothing the operator asked for that could be
// carried out without it.
func (s *Service) rollbackTarget(ctx context.Context, steps int) (domain.Target, error) {
	current, err := s.Repo.HeadCommit(ctx)
	if err != nil {
		return domain.Target{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	store := history.Store{Path: filepath.Join(s.Settings.StateDir, historyFileName)}
	file, ok, err := store.Load()
	if err != nil {
		return domain.Target{}, exitcode.New(exitcode.Preflight,
			i18nLine(MsgHistoryUnreadable), err, store.Path)
	}
	if !ok {
		return domain.Target{}, exitcode.New(exitcode.Preflight, "%s",
			i18nLine(MsgNoHistory)+"\n"+i18nLine(MsgNoHistoryTip))
	}
	records := file.Records(s.Settings.RepoDir)
	now := history.Record{Commit: current, At: time.Now().Unix()}
	position, ok := history.Step(records, now, steps)
	if !ok {
		return domain.Target{}, exitcode.New(exitcode.Preflight,
			i18nLine(MsgNoHistorySteps), len(history.Visit(records, now, history.MaxRecords()))-1)
	}
	name := i18nLine(MsgRecordedPosition)
	if tags, err := s.Repo.Tags(ctx); err == nil {
		if tag := firstTag(tags[position.Commit]); tag != "" {
			name = tag
		}
	}
	return domain.Target{
		Commit:   position.Commit,
		Selector: fmt.Sprintf("-n %d", steps),
		Name:     name,
	}, nil
}

// switchToTarget moves the checkout to the resolved target, streaming git's
// words to the console and the log.
func (s *Service) switchToTarget(ctx context.Context, target domain.Target) error {
	handle, err := s.LogFile.OpenAppend()
	if err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	defer handle.Close()
	out := io.MultiWriter(s.emitter().Stream(), handle)
	errOut := io.MultiWriter(s.emitter().Diagnostics(), handle)
	if target.Latest {
		return s.Repo.FastForwardMaster(ctx, out, errOut)
	}
	return s.Repo.CheckoutDetach(ctx, target.Commit, out, errOut)
}

// warnWhenOutsideOrigin names a target origin/master cannot reach. It is a
// warning, not a refusal: an unmerged release branch is a legitimate thing to
// deploy.
func (s *Service) warnWhenOutsideOrigin(ctx context.Context, target domain.Target) {
	if target.Latest {
		return
	}
	tip, err := s.Repo.RemoteTip(ctx)
	if err != nil {
		// Cannot look: no claim either way.
		return
	}
	ok, err := s.Repo.IsAncestor(ctx, target.Commit, tip)
	if err != nil || ok {
		return
	}
	s.warning(i18nLine(MsgTargetOutsideOrigin, domain.ShortCommit(target.Commit), s.Repo.RemoteTipName()))
}

// recordDeploy writes the move into the deployment history: where the tree was
// and where it went. A corrupt history is rebuilt from the move itself rather
// than blocking a deployment, and the caller is told.
func (s *Service) recordDeploy(ctx context.Context, before string, target domain.Target) error {
	store := history.Store{Path: filepath.Join(s.Settings.StateDir, historyFileName)}
	file, _, err := store.Load()
	if err != nil {
		s.warning(i18nLine(MsgHistoryRebuild, err))
		file = history.File{}
	}
	records := file.Records(s.Settings.RepoDir)
	now := time.Now().Unix()
	if before != "" && (len(records) == 0 || records[0].Commit != before) {
		// The starting point is recorded only when the stack does not already
		// name it: it is what a bare rollback returns to.
		records = history.Visit(records, history.Record{Commit: before, At: now}, history.MaxRecords())
	}
	records = history.Visit(records, history.Record{
		Commit: target.Commit, Selector: target.Selector, At: now,
	}, history.MaxRecords())
	if err := store.Save(file.With(s.Settings.RepoDir, records)); err != nil {
		return fmt.Errorf("%s: %w", i18nLine(MsgHistoryWriteFailed), err)
	}
	return nil
}
