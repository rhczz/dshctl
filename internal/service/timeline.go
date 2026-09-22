package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/paths"
	"github.com/rhczz/dshctl/internal/repo"
)

// timelineWindow bounds the commit rows a timeline prints. One release interval
// in the managed checkout holds over a hundred first-parent commits, so the
// view has to be a window plus every tag in the gap rather than a full log.
const timelineWindow = 10

// historyFileName is the deployment history inside the state directory. It is
// fixed: the file is part of the documented state-directory layout, and a
// configurable name would only make an incident harder to read.
const historyFileName = "updates.json"

// TimelineCurrent is where the checkout is now.
type TimelineCurrent struct {
	// Commit is the full revision.
	Commit string `json:"commit"`
	// Short is the abbreviated revision.
	Short string `json:"shortCommit"`
	// Branch is the checked-out branch, empty for a detached head.
	Branch string `json:"branch,omitempty"`
	// Tag is the tag exactly at this commit, when there is one.
	Tag string `json:"tag,omitempty"`
	// Detached reports that HEAD is not on a branch.
	Detached bool `json:"detached,omitempty"`
}

// TimelineRemote is where origin/master is.
type TimelineRemote struct {
	// Name is the ref the contract calls latest.
	Name string `json:"name"`
	// Commit is the full revision.
	Commit string `json:"commit"`
	// Short is the abbreviated revision.
	Short string `json:"shortCommit"`
	// Tag is the tag exactly at the remote tip, when there is one.
	Tag string `json:"tag,omitempty"`
}

// TimelineCommit is one row of the version list.
type TimelineCommit struct {
	// Commit is the full revision.
	Commit string `json:"commit"`
	// Short is the abbreviated revision.
	Short string `json:"shortCommit"`
	// Subject is the first line of the commit message.
	Subject string `json:"subject"`
	// Tags are the tags pointing at this commit.
	Tags []string `json:"tags,omitempty"`
	// Current marks the commit the checkout is at.
	Current bool `json:"current,omitempty"`
	// Remote marks the origin/master tip.
	Remote bool `json:"remote,omitempty"`
	// Skipped is how many commits the row before this one omitted.
	Skipped int `json:"skipped,omitempty"`
}

// TimelineReport is what `timeline` observed.
type TimelineReport struct {
	// RepoDir is the checkout the report describes.
	RepoDir string `json:"repoDir"`
	// Fetched reports whether origin was consulted; when false, every remote
	// answer comes from the last known state and must not be read as current.
	Fetched bool `json:"fetched"`
	// FetchError explains a failed fetch.
	FetchError string `json:"fetchError,omitempty"`
	// Dirty reports tracked files with uncommitted changes.
	Dirty bool `json:"dirty,omitempty"`
	// DirtyError reports that the worktree state could not be determined.
	DirtyError string `json:"dirtyError,omitempty"`
	// Current is where the checkout is now.
	Current TimelineCurrent `json:"current"`
	// Remote is where origin/master is.
	Remote TimelineRemote `json:"remote"`
	// Behind counts commits origin/master has and HEAD does not.
	Behind int `json:"behind"`
	// Ahead counts commits HEAD has and origin/master does not.
	Ahead int `json:"ahead"`
	// UpToDate reports that HEAD is exactly origin/master.
	UpToDate bool `json:"upToDate"`
	// Commits is the version list: the newest window, every tagged commit in
	// the gap, the remote tip and the current position.
	Commits []TimelineCommit `json:"commits,omitempty"`
	// History is the deployment history for this checkout, newest first,
	// capped at the window size.
	History []history.Record `json:"history,omitempty"`
	// HistoryTotal is how many positions the file holds for this checkout.
	HistoryTotal int `json:"historyTotal,omitempty"`
	// HistoryError reports a history file that exists but cannot be read.
	HistoryError string `json:"historyError,omitempty"`
}

// Timeline reports the gap between the checkout and origin/master.
//
// It is the one reporting command that writes outside its own output: the fetch
// updates .git's remote-tracking references. The working tree and the state
// directory are not touched. A failed fetch is not hidden — the report says so
// and the command exits as a failed preflight — because a timeline that
// silently showed last week's remote would be worse than no timeline at all.
func (s *Service) Timeline(ctx context.Context) (TimelineReport, error) {
	if !s.Repo.Exists() {
		return TimelineReport{}, exitcode.New(exitcode.Preflight,
			"仓库目录不存在: %s\n提示: 用 --repo 或环境变量 %s 指定仓库路径",
			s.Settings.RepoDir, paths.EnvRepoDir)
	}
	if !s.Repo.IsGit() {
		return TimelineReport{}, exitcode.New(exitcode.Preflight, "%s 不是 git 仓库", s.Settings.RepoDir)
	}
	if !s.Repo.IsServerCheckout() {
		return TimelineReport{}, exitcode.New(exitcode.Preflight,
			"%s 看起来不是 DeepSeek Harness 仓库(缺少 %s 或 %s)",
			s.Settings.RepoDir, configServerManifest, configWorkspaceManifest)
	}
	hasOrigin, err := s.Repo.HasOrigin(ctx)
	if err != nil {
		return TimelineReport{}, exitcode.Wrap(exitcode.Preflight, err)
	}
	if !hasOrigin {
		return TimelineReport{}, exitcode.New(exitcode.Preflight,
			"仓库 %s 没有 origin 远程，无法比较版本\n提示: 确认这是一个 clone，而不是本地目录",
			s.Settings.RepoDir)
	}

	report := TimelineReport{RepoDir: s.Settings.RepoDir}
	if err := s.Repo.Fetch(ctx, nil, nil); err != nil {
		report.FetchError = err.Error()
		s.warning(fmt.Sprintf("无法获取远程更新，以下差距基于本地已知状态: %v", err))
	} else {
		report.Fetched = true
	}

	current, err := s.Repo.HeadCommit(ctx)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	branch, tag, err := s.Repo.HeadName(ctx)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	report.Current = TimelineCurrent{
		Commit: current, Short: domain.ShortCommit(current),
		Branch: branch, Tag: tag, Detached: branch == "",
	}

	remote, err := s.Repo.RemoteTip(ctx)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	tags, err := s.Repo.Tags(ctx)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	report.Remote = TimelineRemote{
		Name: repo.RemoteTipName, Commit: remote, Short: domain.ShortCommit(remote),
		Tag: firstTag(tags[remote]),
	}

	behind, err := s.Repo.CountRange(ctx, current, remote)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	ahead, err := s.Repo.CountRange(ctx, remote, current)
	if err != nil {
		return report, exitcode.Wrap(exitcode.Preflight, err)
	}
	report.Behind, report.Ahead = behind, ahead
	report.UpToDate = behind == 0 && ahead == 0

	if behind > 0 {
		commits, err := s.Repo.FirstParentLog(ctx, current, remote)
		if err != nil {
			return report, exitcode.Wrap(exitcode.Preflight, err)
		}
		info, err := s.Repo.CommitInfo(ctx, current)
		if err != nil {
			return report, exitcode.Wrap(exitcode.Preflight, err)
		}
		report.Commits = timelineRows(commits, tags, remote, info)
	}

	if dirty, err := s.Repo.TrackedChanges(ctx); err != nil {
		// "Cannot look" is not "clean": the row says the state is unknown.
		report.DirtyError = err.Error()
	} else {
		report.Dirty = dirty
	}

	s.readTimelineHistory(&report)
	return report, nil
}

// readTimelineHistory fills the deployment history section of the report.
func (s *Service) readTimelineHistory(report *TimelineReport) {
	store := history.Store{Path: filepath.Join(s.Settings.StateDir, historyFileName)}
	file, ok, err := store.Load()
	if err != nil {
		report.HistoryError = err.Error()
		s.warning(fmt.Sprintf("无法读取更新历史 %s: %v", store.Path, err))
		return
	}
	if !ok {
		return
	}
	records := file.Records(report.RepoDir)
	report.HistoryTotal = len(records)
	if len(records) > timelineWindow {
		records = records[:timelineWindow]
	}
	report.History = records
}

// timelineRows builds the version list: the newest window, every tagged commit
// in the gap, the remote tip, and the current position closing the list.
func timelineRows(commits []repo.Commit, tags map[string][]string, remote string, current repo.Commit) []TimelineCommit {
	rows := make([]TimelineCommit, 0, timelineWindow+len(tags)+1)
	skipped := 0
	for index, commit := range commits {
		if index >= timelineWindow && len(tags[commit.Full]) == 0 {
			skipped++
			continue
		}
		rows = append(rows, TimelineCommit{
			Commit: commit.Full, Short: commit.Short, Subject: commit.Subject,
			Tags: tags[commit.Full], Remote: commit.Full == remote, Skipped: skipped,
		})
		skipped = 0
	}
	rows = append(rows, TimelineCommit{
		Commit: current.Full, Short: current.Short, Subject: current.Subject,
		Tags: tags[current.Full], Current: true, Skipped: skipped,
	})
	return rows
}

func firstTag(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
