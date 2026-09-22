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
		add("state directory", CheckOK, s.Settings.StateDir)
	} else {
		add("state directory", CheckWarn, s.Settings.StateDir+" not created yet; the first run creates it")
	}
	if paths.Exists(s.Settings.ConfigPath) {
		add("settings document", CheckOK, s.Settings.ConfigPath)
	} else {
		add("settings document", CheckWarn, s.Settings.ConfigPath+" not created yet; the defaults will be written")
	}

	switch {
	case !s.Repo.Exists():
		add("checkout", CheckFail,
			fmt.Sprintf("%s does not exist; name it once with --repo or %s and a successful run writes it into %s", s.Settings.RepoDir, paths.EnvRepoDir, s.Settings.ConfigPath))
	case !s.Repo.IsGit():
		add("checkout", CheckFail, fmt.Sprintf("%s is not a git repository", s.Settings.RepoDir))
	case !s.Repo.IsServerCheckout():
		add("checkout", CheckFail,
			fmt.Sprintf("%s has no %s or %s; it does not look like a DeepSeek Harness checkout", s.Settings.RepoDir, config.ServerManifestRel, config.WorkspaceManifestRel))
	default:
		sha, branch, err := s.Repo.Head(ctx)
		if err != nil {
			add("checkout revision", CheckWarn, err.Error())
			break
		}
		detail := branch + "@" + sha
		if dirty, err := s.Repo.Dirty(ctx); err == nil && dirty {
			detail += " (tracked changes present)"
		}
		add("checkout revision", CheckOK, detail)
	}

	if s.Repo.NodeModulesPresent() {
		add("dependencies", CheckOK, filepath.Join(s.Settings.RepoDir, "node_modules")+" installed")
	} else {
		add("dependencies", CheckFail, filepath.Join(s.Settings.RepoDir, "node_modules")+" does not exist; run pnpm install first")
	}
	if s.Repo.BuildReady() {
		add("build artifacts", CheckOK, s.Repo.BuildRecordPath())
	} else {
		add("build artifacts", CheckFail, fmt.Sprintf("missing %s; run dshctl build", s.Repo.BuildRecordPath()))
	}

	s.doctorNode(ctx, add)
	s.doctorPnpm(ctx, add)
	s.doctorService(ctx, add)
	s.doctorLock(add)

	if size, err := s.LogFile.Size(); err != nil {
		add("log", CheckWarn, err.Error())
	} else {
		add("log", CheckOK, fmt.Sprintf("%s (%s)", s.Settings.LogPath, HumanBytes(size)))
	}
	add("detach method", CheckOK, detach.Describe())
	if s.BuildInfo.GoVersion != "" {
		add("build info", CheckOK, s.BuildInfo.GoVersion+" · "+s.BuildInfo.Module)
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
		detail += ", resolved through a shim"
	}
	// A release the gate refuses never reaches this point: resolveNode reports it
	// as the failure above. Everything else is usable, so the row separates
	// "inside the verified range" from "used, and said out loud".
	switch verdict := nodejs.Assess(installation, config.MinNodeVersion, config.TestedNodeVersion); verdict.Status {
	case nodejs.Supported:
		add("Node", CheckOK, detail)
	default:
		add("Node", CheckWarn, detail+"; "+verdict.Reason)
	}
}

// doctorPnpm reports whether pnpm is runnable.
func (s *Service) doctorPnpm(ctx context.Context, add func(string, string, string)) {
	path, err := s.pnpmPath()
	if err != nil {
		add("pnpm", CheckFail, "pnpm was not found; install it and make sure it is on PATH")
		return
	}
	version, err := run.Collector(s.Exec).Output(ctx, run.Command{Name: path, Args: []string{"--version"}})
	if err != nil {
		add("pnpm", CheckWarn, fmt.Sprintf("%s exists but cannot be executed", path))
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
		add("port", CheckFail, err.Error())
		add("runtime record", CheckWarn, "the service state cannot be observed; see the row above")
		return
	}
	status := observed.status

	switch status.State {
	case domain.StateRunning:
		add("port", CheckOK, fmt.Sprintf("%d is held by a service dshctl started (pid=%d)", s.Settings.Port, status.ListenerPID))
	case domain.StateStarting:
		add("port", CheckWarn, fmt.Sprintf("%d is held by a service dshctl started (pid=%d), but the port is not ready yet", s.Settings.Port, status.ListenerPID))
	case domain.StateForeign:
		add("port", CheckWarn,
			fmt.Sprintf("%d is held by another process (pid=%d: %s)", s.Settings.Port, status.ListenerPID, status.ListenerCommand))
	case domain.StateOrphan:
		if status.Survivor {
			add("port", CheckWarn,
				fmt.Sprintf("%d is served by a survivor of an interrupted start (pid=%d); dshctl start or dshctl stop manages it again", s.Settings.Port, status.ListenerPID))
		} else {
			add("port", CheckWarn,
				fmt.Sprintf("%d is held by a process dshctl cannot claim (pid=%d: %s)", s.Settings.Port, status.ListenerPID, status.ListenerCommand))
		}
	default:
		add("port", CheckOK, fmt.Sprintf("%d is free", s.Settings.Port))
	}

	switch {
	case status.RecordStale && status.StaleRecord == nil:
		add("runtime record", CheckWarn, fmt.Sprintf("%s cannot be parsed; the next start or stop rebuilds it", s.Record.Path))
	case status.RecordStale:
		add("runtime record", CheckWarn, fmt.Sprintf("the record names pid=%d, which is gone or has been reused (a stale record; the next start or stop clears it)", status.StaleRecord.PID))
	case status.RecordLive:
		add("runtime record", CheckOK, observed.record.Describe())
	case status.RecordedPID != 0:
		add("runtime record", CheckWarn, observed.record.Describe())
	default:
		add("runtime record", CheckOK, "none (the service has never been started)")
	}

	// The running instance and the configuration can name different checkouts:
	// --repo applies to one invocation, and a checkout can move while a server
	// keeps running. Saying so is what keeps a set of failures about the
	// configured directory from looking like a broken service.
	if running := status.RecordedRepoDir; running != "" && running != s.Settings.RepoDir {
		add("service checkout", CheckWarn,
			fmt.Sprintf("the running service (pid=%d) comes from %s while the configuration says %s; stop it with its port and start again with --repo %s to switch", status.ListenerPID, running, s.Settings.RepoDir, running))
	}
}

// doctorLock reports the operation lock.
func (s *Service) doctorLock(add func(string, string, string)) {
	holder, held, err := lock.Held(s.Settings.LockFile())
	switch {
	case err != nil:
		add("operation lock", CheckWarn, err.Error())
	case !held:
		add("operation lock", CheckOK, "free")
	case holder != 0:
		add("operation lock", CheckWarn, fmt.Sprintf("held by pid=%d; another dshctl operation is running", holder))
	default:
		add("operation lock", CheckWarn, "held, but the lock file carries no readable pid")
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
