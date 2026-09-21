package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/run"
)

// fakeSHA builds a distinct full revision for the fixture's scripts. The first
// seven characters differ, so an abbreviated sha names one commit.
func fakeSHA(n int) string {
	return fmt.Sprintf("%07x%s", n, strings.Repeat("0", 33))
}

// seedHistory writes the deployment history the timeline reads.
func (f *fixture) seedHistory(t *testing.T, file history.File) {
	t.Helper()
	store := history.Store{Path: filepath.Join(f.Settings.StateDir, historyFileName)}
	if err := store.Save(file); err != nil {
		t.Fatalf("save history: %v", err)
	}
}

// TestTimelineReportsUpToDate pins the quiet case: no gap, no commit list, and
// an exact rendering.
func TestTimelineReportsUpToDate(t *testing.T) {
	f := newFixture(t)
	f.host.gitHead = f.host.gitRemote

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if !report.Fetched {
		t.Fatal("a healthy fetch was reported as failed")
	}
	if !report.UpToDate || report.Behind != 0 || report.Ahead != 0 {
		t.Fatalf("report = %+v, want up to date", report)
	}
	if len(report.Commits) != 0 {
		t.Fatalf("commits = %+v, want none when up to date", report.Commits)
	}
	if report.Dirty || report.DirtyError != "" {
		t.Fatalf("worktree = (dirty=%v, err=%q), want clean", report.Dirty, report.DirtyError)
	}

	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	want := fmt.Sprintf("仓库: %s\n当前: def5678 (main)\n远程: def5678 (origin/master)\n差距: 已是最新（origin/master）\n", f.repo)
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestTimelineShowsTheWindowTagsAndElision pins the version list: the newest
// window, every tag in the gap, the remote tip, the current position, and an
// elision row that accounts for what was left out.
func TestTimelineShowsTheWindowTagsAndElision(t *testing.T) {
	f := newFixture(t)
	head := fakeSHA(1000)
	remote := fakeSHA(2000)
	f.host.gitHead = head
	f.host.gitRemote = remote
	f.host.gitBehind = 12
	f.host.gitLog = nil
	for index := 0; index < 12; index++ {
		f.host.gitLog = append(f.host.gitLog, fakeCommit(fakeSHA(2000-index), fmt.Sprintf("subject %d", index)))
	}
	f.host.gitTags = map[string]string{
		"dsh-v0.1.0": fakeSHA(1995),
		"dsh-v0.1.1": fakeSHA(1989),
	}

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.Behind != 12 || report.UpToDate {
		t.Fatalf("report = %+v, want 12 behind", report)
	}
	if len(report.Commits) != 12 {
		t.Fatalf("commits = %d, want 10 window rows, the tagged row and the current row", len(report.Commits))
	}
	first := report.Commits[0]
	if !first.Remote || first.Commit != remote || first.Skipped != 0 {
		t.Fatalf("first row = %+v, want the remote tip", first)
	}
	if got := report.Commits[5].Tags; len(got) != 1 || got[0] != "dsh-v0.1.0" {
		t.Fatalf("row 5 tags = %v, want the in-window tag", got)
	}
	tagged := report.Commits[10]
	if tagged.Commit != fakeSHA(1989) || len(tagged.Tags) != 1 || tagged.Skipped != 1 {
		t.Fatalf("tagged row = %+v, want the older tag with one skipped commit", tagged)
	}
	current := report.Commits[11]
	if !current.Current || current.Commit != head || current.Skipped != 0 {
		t.Fatalf("last row = %+v, want the current position", current)
	}

	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"差距: 落后 12 个提交（中间有 2 个 tag）",
		"○ " + remote[:7] + "  subject 0   ← 远程最新",
		"  … 省略 1 个提交 …\n  " + fakeSHA(1989)[:7] + "  dsh-v0.1.1  subject 11\n● " + head[:7] + "  fixture head   ← 当前",
		"  " + fakeSHA(1995)[:7] + "  dsh-v0.1.0  subject 5",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output = %q, want it to contain %q", text, want)
		}
	}
}

// TestTimelineReportsDivergence pins the local-commits case: the gap line names
// both directions and says update cannot fast-forward.
func TestTimelineReportsDivergence(t *testing.T) {
	f := newFixture(t)
	f.host.gitHead = fakeSHA(1000)
	f.host.gitRemote = fakeSHA(2000)
	f.host.gitBehind = 5
	f.host.gitAhead = 3
	f.host.gitLog = []fakeGitCommit{fakeCommit(fakeSHA(2000), "upstream work")}

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.UpToDate || report.Behind != 5 || report.Ahead != 3 {
		t.Fatalf("report = %+v, want a diverged state", report)
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "分叉：落后 5 个、本地领先 3 个（update 无法快进）") {
		t.Fatalf("output = %q, want the divergence line", out.String())
	}
}

// TestTimelineKeepsLocalStateWhenFetchFails pins the honesty rule: a failed
// fetch still shows what is known locally, says the remote was not consulted,
// and never claims to be up to date.
func TestTimelineKeepsLocalStateWhenFetchFails(t *testing.T) {
	f := newFixture(t)
	f.host.gitHead = f.host.gitRemote
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "fetch") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.Fetched {
		t.Fatal("a failed fetch was reported as fetched")
	}
	if report.FetchError == "" {
		t.Fatal("the fetch failure has no reason")
	}
	if report.Remote.Commit != f.host.gitRemote {
		t.Fatalf("remote = %q, want the locally known tip", report.Remote.Commit)
	}
	if !strings.Contains(f.errOut.String(), "无法获取远程更新") {
		t.Fatalf("stderr = %q, want the fetch warning", f.errOut.String())
	}

	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "远程: 无法获取（") {
		t.Fatalf("output = %q, want the unknown-remote header", text)
	}
	if strings.Contains(text, "已是最新") {
		t.Fatalf("output = %q, must not claim to be up to date after a failed fetch", text)
	}
	if !strings.Contains(text, "远程未确认") {
		t.Fatalf("output = %q, want the gap line to say the remote is unconfirmed", text)
	}
}

// TestTimelineNamesTheDirtyWorktree pins the row that explains why an update
// would refuse before the operator tries it.
func TestTimelineNamesTheDirtyWorktree(t *testing.T) {
	f := newFixture(t)
	f.host.gitStatus = " M apps/cli/src/main.ts\n"

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if !report.Dirty {
		t.Fatal("a modified tracked file did not mark the worktree dirty")
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "工作区: 有未提交修改（update/rollback 会拒绝，请先处理）") {
		t.Fatalf("output = %q, want the dirty row", out.String())
	}
}

// TestTimelineReportsWhenTheWorktreeCannotBeChecked pins that a failed status
// query is not reported as a clean worktree.
func TestTimelineReportsWhenTheWorktreeCannotBeChecked(t *testing.T) {
	f := newFixture(t)
	f.host.fail = func(cmd run.Command) error {
		if hasArgument(cmd, "status", "--porcelain") {
			return &run.ExitError{Command: cmd.String(), Code: 128}
		}
		return nil
	}

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.Dirty {
		t.Fatal("an unanswerable status query was reported as dirty")
	}
	if report.DirtyError == "" {
		t.Fatal("an unanswerable status query left no reason")
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "工作区: 无法确认（") {
		t.Fatalf("output = %q, want the unknown-worktree row", out.String())
	}
}

// TestTimelinePrintsTheHistoryNewestFirst pins the deployment section: only
// this checkout's positions, newest first, capped at the window, with the
// current position marked and an empty selector shown as a dash.
func TestTimelinePrintsTheHistoryNewestFirst(t *testing.T) {
	f := newFixture(t)
	head := f.host.gitHead
	records := []history.Record{
		{Commit: head, Selector: "latest", At: 1_700_000_000},
		{Commit: fakeSHA(900), Selector: "", At: 1_699_000_000},
	}
	f.seedHistory(t, history.File{Repos: []history.Group{
		{Repo: f.repo, Records: records},
		{Repo: filepath.Join(f.root, "other"), Records: []history.Record{{Commit: fakeSHA(901), At: 1}}},
	}})

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(report.History) != 2 || report.HistoryTotal != 2 {
		t.Fatalf("history = %+v (total %d), want two records", report.History, report.HistoryTotal)
	}
	if report.History[0].Commit != head {
		t.Fatalf("history = %+v, want the newest first", report.History)
	}

	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	text := out.String()
	stamp := time.Unix(1_700_000_000, 0).Format("2006-01-02 15:04")
	if !strings.Contains(text, "更新历史:\n● "+head[:7]+"  latest") || !strings.Contains(text, stamp) {
		t.Fatalf("output = %q, want the current deployment marked", text)
	}
	if !strings.Contains(text, "  "+fakeSHA(900)[:7]+"  - ") {
		t.Fatalf("output = %q, want an empty selector shown as a dash", text)
	}
	if strings.Contains(text, fakeSHA(901)[:7]) {
		t.Fatalf("output = %q, want another checkout's history left out", text)
	}
}

// TestTimelineCapsTheHistorySection pins the display bound and the note that
// says more positions exist.
func TestTimelineCapsTheHistorySection(t *testing.T) {
	f := newFixture(t)
	records := make([]history.Record, 0, timelineWindow+2)
	for index := 0; index < timelineWindow+2; index++ {
		records = append(records, history.Record{Commit: fakeSHA(100 + index), Selector: "latest", At: int64(index)})
	}
	f.seedHistory(t, history.File{Repos: []history.Group{{Repo: f.repo, Records: records}}})

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(report.History) != timelineWindow || report.HistoryTotal != timelineWindow+2 {
		t.Fatalf("history = %d/%d, want the window over the total", len(report.History), report.HistoryTotal)
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "… 还有 2 条（--json 查看）") {
		t.Fatalf("output = %q, want the remaining count", out.String())
	}
}

// TestTimelineWindowBoundary pins the off-by-one at the window edge: exactly
// one window of commits has nothing to elide, and one more commit does.
func TestTimelineWindowBoundary(t *testing.T) {
	for _, count := range []int{timelineWindow, timelineWindow + 1} {
		t.Run(fmt.Sprintf("%d commits", count), func(t *testing.T) {
			f := newFixture(t)
			f.host.gitHead = fakeSHA(1000)
			f.host.gitRemote = fakeSHA(2000)
			f.host.gitBehind = count
			f.host.gitLog = nil
			for index := 0; index < count; index++ {
				f.host.gitLog = append(f.host.gitLog, fakeCommit(fakeSHA(2000-index), fmt.Sprintf("subject %d", index)))
			}

			report, err := f.Timeline(context.Background())
			if err != nil {
				t.Fatalf("Timeline: %v", err)
			}
			wantRows := count
			if count > timelineWindow {
				wantRows = timelineWindow
			}
			wantRows++ // the current position closes the list
			if len(report.Commits) != wantRows {
				t.Fatalf("commits = %d, want %d", len(report.Commits), wantRows)
			}
			wantSkipped := count - timelineWindow
			if wantSkipped < 0 {
				wantSkipped = 0
			}
			if got := report.Commits[len(report.Commits)-1].Skipped; got != wantSkipped {
				t.Fatalf("current row skipped = %d, want %d", got, wantSkipped)
			}
		})
	}
}

// TestTimelineNamesATagOnTheCurrentCommit pins that a rolled-back checkout is
// named by its tag in both the header and the list row.
func TestTimelineNamesATagOnTheCurrentCommit(t *testing.T) {
	f := newFixture(t)
	head := fakeSHA(1000)
	f.host.gitHead = head
	f.host.gitRemote = fakeSHA(2000)
	f.host.gitBehind = 3
	f.host.gitLog = []fakeGitCommit{fakeCommit(fakeSHA(2000), "upstream work")}
	f.host.gitTags = map[string]string{"dsh-v0.1.0": head}

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.Current.Tag != "dsh-v0.1.0" || report.Current.Detached {
		t.Fatalf("current = %+v, want a tag on a branch", report.Current)
	}
	current := report.Commits[len(report.Commits)-1]
	if !current.Current || len(current.Tags) != 1 || current.Tags[0] != "dsh-v0.1.0" {
		t.Fatalf("current row = %+v, want its tag", current)
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "当前: "+head[:7]+" (tag dsh-v0.1.0)") {
		t.Fatalf("output = %q, want the tag in the header", out.String())
	}
}

// TestTimelineReportsACorruptHistory pins that a file nobody can read is
// reported and the timeline still works: the version gap does not depend on
// dshctl's own bookkeeping.
func TestTimelineReportsACorruptHistory(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.Settings.StateDir, historyFileName)
	writeFile(t, path, "{not json")

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.HistoryError == "" {
		t.Fatal("a corrupt history left no reason")
	}
	if len(report.History) != 0 {
		t.Fatalf("history = %+v, want none", report.History)
	}
	if !strings.Contains(f.errOut.String(), "无法读取更新历史") {
		t.Fatalf("stderr = %q, want the history warning", f.errOut.String())
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if strings.Contains(out.String(), "更新历史:") {
		t.Fatalf("output = %q, want no history section", out.String())
	}
}

// TestTimelineTakesTheCurrentPositionFromGit pins the split the history file
// must not blur: where the checkout is now comes from git, and the record only
// says where dshctl has deployed it. A hand-made checkout makes the two differ,
// and the report has to side with git.
func TestTimelineTakesTheCurrentPositionFromGit(t *testing.T) {
	f := newFixture(t)
	current := fakeSHA(1000)
	recorded := fakeSHA(999)
	f.host.gitHead = current
	f.host.gitRemote = fakeSHA(2000)
	f.host.gitBehind = 1
	f.host.gitLog = []fakeGitCommit{fakeCommit(fakeSHA(2000), "upstream work")}
	f.seedHistory(t, history.File{Repos: []history.Group{{Repo: f.repo, Records: []history.Record{
		{Commit: recorded, Selector: "latest", At: 1},
	}}}})

	report, err := f.Timeline(context.Background())
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if report.Current.Commit != current {
		t.Fatalf("current = %q, want the commit git reports %q", report.Current.Commit, current)
	}
	if len(report.History) != 1 || report.History[0].Commit != recorded {
		t.Fatalf("history = %+v, want the recorded position untouched", report.History)
	}
	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	if strings.Contains(out.String(), "● "+recorded[:7]) {
		t.Fatalf("output = %q, want the recorded position not marked as current", out.String())
	}
	if !strings.Contains(out.String(), "● "+current[:7]) {
		t.Fatalf("output = %q, want the git position marked as current", out.String())
	}
}

// TestTimelineRefusesACheckoutWithoutOrigin pins the preflight: "latest" has no
// meaning without a remote, and a fetch failure message would not say that.
func TestTimelineRefusesACheckoutWithoutOrigin(t *testing.T) {
	f := newFixture(t)
	f.host.gitOrigin = ""

	_, err := f.Timeline(context.Background())
	if err == nil {
		t.Fatal("a checkout without origin produced a timeline")
	}
	if exitcode.Of(err) != exitcode.Preflight {
		t.Fatalf("exit code = %d, want preflight", exitcode.Of(err))
	}
	if !strings.Contains(err.Error(), "origin") {
		t.Fatalf("error = %v, want it to name origin", err)
	}
}

// TestTimelineRefusesAMissingCheckout pins the first preflight.
func TestTimelineRefusesAMissingCheckout(t *testing.T) {
	f := newFixture(t)
	missing := filepath.Join(f.root, "missing-repo")
	f.Settings.RepoDir = missing
	f.Repo.Dir = missing

	_, err := f.Timeline(context.Background())
	if err == nil {
		t.Fatal("a missing checkout produced a timeline")
	}
	if exitcode.Of(err) != exitcode.Preflight {
		t.Fatalf("exit code = %d, want preflight", exitcode.Of(err))
	}
}

// TestPrintTimelineRendersTheExactFormat pins the whole human report, because
// the view is the feature: a change to any row shape is a change to the
// contract an operator reads.
func TestPrintTimelineRendersTheExactFormat(t *testing.T) {
	report := TimelineReport{
		RepoDir:  "/checkouts/harness",
		Fetched:  true,
		Dirty:    true,
		Current:  TimelineCurrent{Commit: fakeSHA(1000), Short: fakeSHA(1000)[:7], Detached: true},
		Remote:   TimelineRemote{Name: "origin/master", Commit: fakeSHA(2000), Short: fakeSHA(2000)[:7]},
		Behind:   2,
		UpToDate: false,
		Commits: []TimelineCommit{
			{Commit: fakeSHA(2000), Short: fakeSHA(2000)[:7], Subject: "the newest", Remote: true},
			{Commit: fakeSHA(1999), Short: fakeSHA(1999)[:7], Subject: "a middle commit", Tags: []string{"dsh-v0.1.1"}, Skipped: 1},
			{Commit: fakeSHA(1000), Short: fakeSHA(1000)[:7], Subject: "where we are", Current: true},
		},
		History: []history.Record{
			{Commit: fakeSHA(1000), Selector: "-n 1", At: 1_700_000_000},
			{Commit: fakeSHA(999), At: 1_699_000_000},
		},
		HistoryTotal: 3,
	}

	var out strings.Builder
	if err := PrintTimeline(&out, report); err != nil {
		t.Fatalf("PrintTimeline: %v", err)
	}
	stamp := time.Unix(1_700_000_000, 0).Format("2006-01-02 15:04")
	older := time.Unix(1_699_000_000, 0).Format("2006-01-02 15:04")
	want := "仓库: /checkouts/harness\n" +
		"当前: " + fakeSHA(1000)[:7] + " (detached)\n" +
		"远程: " + fakeSHA(2000)[:7] + " (origin/master)\n" +
		"差距: 落后 2 个提交（中间有 1 个 tag）\n" +
		"工作区: 有未提交修改（update/rollback 会拒绝，请先处理）\n" +
		"\n" +
		"○ " + fakeSHA(2000)[:7] + "  the newest   ← 远程最新\n" +
		"  … 省略 1 个提交 …\n" +
		"  " + fakeSHA(1999)[:7] + "  dsh-v0.1.1  a middle commit\n" +
		"● " + fakeSHA(1000)[:7] + "  where we are   ← 当前\n" +
		"\n" +
		"更新历史:\n" +
		"● " + fakeSHA(1000)[:7] + "  -n 1              " + stamp + "\n" +
		"  " + fakeSHA(999)[:7] + "  -                 " + older + "\n" +
		"  … 还有 1 条（--json 查看）\n"
	if out.String() != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out.String(), want)
	}
}
