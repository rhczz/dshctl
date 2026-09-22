package cli

// This test pins the timeline's whole human rendering, because the view is the
// feature: a change to any row shape is a change to the contract an operator
// reads. The service layer computes the report; this is how it reads.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rhczz/dshctl/internal/history"
	"github.com/rhczz/dshctl/internal/service"
)

// TestTimelineRendersTheExactFormat pins the whole human report, because
// the view is the feature: a change to any row shape is a change to the
// contract an operator reads.
func TestTimelineRendersTheExactFormat(t *testing.T) {
	report := service.TimelineReport{
		RepoDir:  "/checkouts/harness",
		Fetched:  true,
		Dirty:    true,
		Current:  service.TimelineCurrent{Commit: fakeSHA(1000), Short: fakeSHA(1000)[:7], Detached: true},
		Remote:   service.TimelineRemote{Name: "origin/master", Commit: fakeSHA(2000), Short: fakeSHA(2000)[:7]},
		Behind:   2,
		UpToDate: false,
		Commits: []service.TimelineCommit{
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

	older := time.Unix(1_699_000_000, 0).Format("2006-01-02 15:04")
	stamp := time.Unix(1_700_000_000, 0).Format("2006-01-02 15:04")
	var out strings.Builder
	if err := printTimeline(&out, report); err != nil {
		t.Fatalf("printTimeline: %v", err)
	}
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

// fakeSHA builds a distinct full revision, the shape a report carries. The first
// seven characters differ, so an abbreviated sha names one commit.
func fakeSHA(n int) string {
	return fmt.Sprintf("%07x%s", n, strings.Repeat("0", 33))
}
