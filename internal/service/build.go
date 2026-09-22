package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/run"
)

// RunBuild installs nothing: it removes stale residue and runs the repository's
// own build, streaming output to the console and the log.
func (s *Service) RunBuild(ctx context.Context) error {
	return s.withLock(ctx, func() error { return s.buildLocked(ctx) })
}

// buildLocked performs the build while the caller holds the lock.
func (s *Service) buildLocked(ctx context.Context) error {
	if !s.Repo.Exists() {
		return exitcode.New(exitcode.Preflight, i18nLine(MsgBuildRepoMissing),
			s.Settings.RepoDir, paths.EnvRepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return exitcode.New(exitcode.Preflight,
			i18nLine(MsgBuildNotCheckout),
			s.Settings.RepoDir, configServerManifest, configWorkspaceManifest)
	}
	pnpm, err := s.pnpmPath()
	if err != nil {
		return err
	}
	if !s.Repo.NodeModulesPresent() {
		return exitcode.New(exitcode.Preflight, i18nLine(MsgBuildNodeModules), s.Settings.RepoDir)
	}
	installation, err := s.resolveNode(ctx)
	if err != nil {
		return err
	}
	s.reportRepoOverride()
	// The checkout is shared: building replaces the artifacts every running
	// server of ours is serving. update stops this port's server because it
	// restarts it; build cannot do that on the operator's behalf, so it refuses
	// as long as any of them is running — the same rule, one step stricter.
	if err := s.refuseWhileServing(ctx, i18nLine(MsgBuildSection)); err != nil {
		return err
	}
	if err := s.prune(ctx); err != nil {
		return err
	}
	// Rotate before the section marker is written, so a marker and its body can
	// never end up in different files.
	if rotated, err := s.LogFile.RotateIfNeeded(); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	} else if rotated {
		s.narrate(i18nLine(MsgBuildLogRotated, s.LogFile.BackupPath()))
	}
	if err := s.LogFile.Section(sectionBuild); err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	s.note("--- pnpm run build ---")

	s.narrate(i18nLine(MsgBuildRunning, s.Settings.RepoDir))
	if err := s.stream(ctx, run.Command{
		Name: pnpm,
		Args: []string{"run", "build"},
		Dir:  s.Settings.RepoDir,
		Env:  run.WithPathPrefix(installation.BinDir),
	}); err != nil {
		s.note(i18nLine(MsgBuildFailedNote))
		return exitcode.Wrap(exitcode.Failure,
			fmt.Errorf("%s", i18nLine(MsgBuildFailed, err, s.Settings.LogPath)))
	}
	s.note(i18nLine(MsgBuildSucceededNote))
	s.narrate(i18nLine(MsgBuildSucceeded))
	// A build proves the checkout is usable, which is exactly what a document
	// that decides nothing is missing: recording it is what makes the next plain
	// command operate on the tree that was just built instead of on a default
	// path nobody chose.
	s.writeBack(s.Settings.RepoDir, "")
	return nil
}

// refuseWhileServing refuses an operation that rewrites the checkout while any
// server of ours is running from it.
//
// The rule is the one `update` states for other ports, applied to every port
// including this one: an operation that replaces the build output underneath a
// running server is refused rather than performed. `update` may stop this
// port's server itself because it restarts it afterwards; a caller that cannot
// restart (build) refuses instead.
//
// Parameters:
//   - action: the command's name, for the message.
func (s *Service) refuseWhileServing(ctx context.Context, action string) error {
	observed, err := s.observe(ctx)
	if err != nil {
		return err
	}
	serving, err := s.serversUsingCheckout(ctx, observed)
	if err != nil {
		return err
	}
	if len(serving) == 0 {
		return nil
	}
	return exitcode.New(exitcode.Preflight, "%s",
		i18nLine(MsgRepoUsedByServers, s.Settings.RepoDir, serving.ports(), serving.pids(), action, action))
}

// servingPorts describes servers this state directory manages on other ports.
//
// The type and its accessors live in managed.go, beside the predicate that
// decides which servers count.

// otherPortsServing reports servers recorded for a different port in the same
// state directory that are running now and that use this checkout.
//
// Each port has its own record, so the only way to see the others is to read
// them: they are files named after their port. The predicate is the shared one
// (managed.go), so an interrupted start's survivor counts here exactly as it
// counts for this port.
//
// A record that names another checkout is skipped: a server built from a
// different tree cannot be disturbed by replacing this one's artifacts, and
// refusing on its account would block a build for a reason that is not true. A
// record that names no checkout — one written before the field existed — is
// counted, because "cannot tell" has to leave the guard as strict as it was.
func (s *Service) otherPortsServing(ctx context.Context) (servingPorts, error) {
	matches, err := filepath.Glob(config.StateFileGlob(s.Settings.StateDir))
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Failure, err)
	}
	var serving servingPorts
	for _, path := range matches {
		stored := recordStore(path)
		record, ok, err := stored.Load()
		if err != nil {
			// A record nobody can read is not a port nobody serves: the guard
			// exists to protect a running server's artifacts, and "cannot look"
			// has to leave it as strict as it was. The build is refused instead
			// of replacing artifacts under a server this build cannot see.
			return nil, exitcode.Wrap(exitcode.Preflight,
				fmt.Errorf("%s", i18nLine(MsgRecordUnreadable, path, s.Settings.RepoDir, err)))
		}
		if !ok || record.Port == s.Settings.Port {
			continue
		}
		if record.RepoDir != "" && record.RepoDir != s.Settings.RepoDir {
			continue
		}
		serves, err := s.recordServesOrFails(ctx, record)
		if err != nil {
			return nil, exitcode.Wrap(exitcode.Preflight,
				fmt.Errorf("%s", i18nLine(MsgPortServiceUnconfirmable, record.Port, s.Settings.RepoDir, err)))
		}
		if serves {
			serving = append(serving, servingPort{port: record.Port, pid: record.PID})
		}
	}
	return serving, nil
}

// prune removes residue left by packages upstream deleted.
func (s *Service) prune(ctx context.Context) error {
	result, err := s.Repo.Prune(ctx, func(message string) {
		s.narrate(message)
		s.note(message)
	})
	if err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	if len(result.Removed) > 0 {
		message := i18nLine(MsgPruneRemoved, len(result.Removed))
		s.narrate(message)
		s.note(i18nLine(MsgPruneDone) + message)
	}
	return nil
}

// stream runs a command with both streams mirrored to the console and the log.
func (s *Service) stream(ctx context.Context, command run.Command) error {
	handle, err := s.LogFile.OpenAppend()
	if err != nil {
		return exitcode.Wrap(exitcode.Failure, err)
	}
	defer handle.Close()
	command.Stdout = io.MultiWriter(s.emitter().Stream(), handle)
	command.Stderr = io.MultiWriter(s.emitter().Diagnostics(), handle)
	return s.Exec.Run(ctx, command)
}

// configServerManifest and configWorkspaceManifest name the checkout markers.
const (
	configServerManifest    = "package.json"
	configWorkspaceManifest = "pnpm-workspace.yaml"
)
