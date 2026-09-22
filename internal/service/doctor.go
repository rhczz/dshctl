package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
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
	checks := make([]Check, 0, 15)
	add := func(name, status, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	if paths.IsDir(s.Settings.StateDir) {
		add(i18nLine(MsgRowStateDir), CheckOK, s.Settings.StateDir)
	} else {
		add(i18nLine(MsgRowStateDir), CheckWarn, s.Settings.StateDir+i18nLine(MsgDirNotCreated))
	}
	if paths.Exists(s.Settings.ConfigPath) {
		add(i18nLine(MsgRowConfig), CheckOK, s.Settings.ConfigPath)
	} else {
		add(i18nLine(MsgRowConfig), CheckWarn, s.Settings.ConfigPath+i18nLine(MsgConfigNotCreated))
	}

	switch {
	case !s.Repo.Exists():
		add(i18nLine(MsgRowRepo), CheckFail,
			i18nLine(MsgRepoMissingDetail, s.Settings.RepoDir, paths.EnvRepoDir, s.Settings.ConfigPath))
	case !s.Repo.IsGit():
		add(i18nLine(MsgRowRepo), CheckFail, i18nLine(MsgRepoNotGitDetail, s.Settings.RepoDir))
	case !s.Repo.IsServerCheckout():
		add(i18nLine(MsgRowRepo), CheckFail,
			i18nLine(MsgRepoNotCheckoutDetail, s.Settings.RepoDir, config.ServerManifestRel, config.WorkspaceManifestRel))
	default:
		sha, branch, err := s.Repo.Head(ctx)
		if err != nil {
			add(i18nLine(MsgRowRepoVersion), CheckWarn, err.Error())
			break
		}
		detail := branch + "@" + sha
		if dirty, err := s.Repo.Dirty(ctx); err == nil && dirty {
			detail += i18nLine(MsgRepoDirty)
		}
		add(i18nLine(MsgRowRepoVersion), CheckOK, detail)
	}

	if s.Repo.NodeModulesPresent() {
		add(i18nLine(MsgRowDeps), CheckOK, filepath.Join(s.Settings.RepoDir, "node_modules")+i18nLine(MsgDepsInstalled))
	} else {
		add(i18nLine(MsgRowDeps), CheckFail, filepath.Join(s.Settings.RepoDir, "node_modules")+i18nLine(MsgDepsMissing))
	}
	if s.Repo.BuildReady() {
		add(i18nLine(MsgRowArtifacts), CheckOK, s.Repo.BuildRecordPath())
	} else {
		add(i18nLine(MsgRowArtifacts), CheckFail, i18nLine(MsgArtifactsMissing, s.Repo.BuildRecordPath()))
	}

	s.doctorNode(ctx, add)
	s.doctorPnpm(ctx, add)
	s.doctorService(ctx, add)
	s.doctorLock(add)

	if size, err := s.LogFile.Size(); err != nil {
		add(i18nLine(MsgRowLog), CheckWarn, err.Error())
	} else {
		add(i18nLine(MsgRowLog), CheckOK, fmt.Sprintf("%s (%s)", s.Settings.LogPath, HumanBytes(size)))
	}
	add(i18nLine(MsgRowDetach), CheckOK, detach.Describe())
	if s.BuildInfo.GoVersion != "" {
		add(i18nLine(MsgRowBuildInfo), CheckOK, s.BuildInfo.GoVersion+" · "+s.BuildInfo.Module)
	}
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
		detail += i18nLine(MsgNodeViaShim)
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
		add(i18nLine(MsgRowPnpm), CheckFail, i18nLine(MsgPnpmMissing))
		return
	}
	version, err := run.Collector(s.Exec).Output(ctx, run.Command{Name: path, Args: []string{"--version"}})
	if err != nil {
		add(i18nLine(MsgRowPnpm), CheckWarn, i18nLine(MsgPnpmNotRunnable, path))
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
		add(i18nLine(MsgRowPort), CheckFail, err.Error())
		add(i18nLine(MsgRowRecord), CheckWarn, i18nLine(MsgStatusUnobservableDoctor))
		return
	}
	status := observed.status

	switch status.State {
	case domain.StateRunning:
		add(i18nLine(MsgRowPort), CheckOK, i18nLine(MsgPortOwnedRunning, s.Settings.Port, status.ListenerPID))
	case domain.StateStarting:
		add(i18nLine(MsgRowPort), CheckWarn, i18nLine(MsgPortOwnedStarting, s.Settings.Port, status.ListenerPID))
	case domain.StateForeign:
		add(i18nLine(MsgRowPort), CheckWarn,
			i18nLine(MsgPortForeignDoctor, s.Settings.Port, status.ListenerPID, status.ListenerCommand))
	case domain.StateOrphan:
		if status.Survivor {
			add(i18nLine(MsgRowPort), CheckWarn,
				i18nLine(MsgPortSurvivorDoctor, s.Settings.Port, status.ListenerPID))
		} else {
			add(i18nLine(MsgRowPort), CheckWarn,
				i18nLine(MsgPortUnclaimedDoctor, s.Settings.Port, status.ListenerPID, status.ListenerCommand))
		}
	default:
		add(i18nLine(MsgRowPort), CheckOK, i18nLine(MsgPortFree, s.Settings.Port))
	}

	switch {
	case status.RecordStale && status.StaleRecord == nil:
		add(i18nLine(MsgRowRecord), CheckWarn, i18nLine(MsgRecordCorrupt, s.Record.Path))
	case status.RecordStale:
		add(i18nLine(MsgRowRecord), CheckWarn, i18nLine(MsgRecordStaleDoctor, status.StaleRecord.PID))
	case status.RecordLive:
		add(i18nLine(MsgRowRecord), CheckOK, observed.record.Describe())
	case status.RecordedPID != 0:
		add(i18nLine(MsgRowRecord), CheckWarn, observed.record.Describe())
	default:
		add(i18nLine(MsgRowRecord), CheckOK, i18nLine(MsgRecordMissing))
	}

	// The running instance and the configuration can name different checkouts:
	// --repo applies to one invocation, and a checkout can move while a server
	// keeps running. Saying so is what keeps a set of failures about the
	// configured directory from looking like a broken service.
	if running := status.RecordedRepoDir; running != "" && running != s.Settings.RepoDir {
		add(i18nLine(MsgRowServiceRepo), CheckWarn,
			i18nLine(MsgServiceOtherRepo, status.ListenerPID, running, s.Settings.RepoDir, running))
	}
}

// doctorLock reports the operation lock.
func (s *Service) doctorLock(add func(string, string, string)) {
	holder, held, err := lock.Held(s.Settings.LockFile())
	switch {
	case err != nil:
		add(i18nLine(MsgRowLock), CheckWarn, err.Error())
	case !held:
		add(i18nLine(MsgRowLock), CheckOK, i18nLine(MsgLockFree))
	case holder != 0:
		add(i18nLine(MsgRowLock), CheckWarn, i18nLine(MsgLockHeldBy, holder))
	default:
		add(i18nLine(MsgRowLock), CheckWarn, i18nLine(MsgLockUnreadable))
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

// HumanBytes renders a byte count for people.
//
// It formats a value that goes into a Check's detail, which is data the service
// returns rather than text a front-end draws: the row's shape is part of the
// `--json` contract, so the rendering lives where the row is built.
func HumanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " B"
	}
	value := float64(size)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		value /= unit
		if value < unit {
			return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix
		}
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " PB"
}
