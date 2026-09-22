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
	want := "checkout: /checkouts/harness\n" +
		"current: " + fakeSHA(1000)[:7] + " (detached)\n" +
		"remote: " + fakeSHA(2000)[:7] + " (origin/master)\n" +
		"gap: 2 commits behind (1 tags in the gap)\n" +
		"worktree: uncommitted changes (update/rollback refuses; handle them first)\n" +
		"\n" +
		"○ " + fakeSHA(2000)[:7] + "  the newest   ← remote tip\n" +
		"  … 1 commits elided …\n" +
		"  " + fakeSHA(1999)[:7] + "  dsh-v0.1.1  a middle commit\n" +
		"● " + fakeSHA(1000)[:7] + "  where we are   ← current\n" +
		"\n" +
		"deployment history:\n" +
		"● " + fakeSHA(1000)[:7] + "  -n 1              " + stamp + "\n" +
		"  " + fakeSHA(999)[:7] + "  -                 " + older + "\n" +
		"  … 1 more (see --json)\n"
	if out.String() != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out.String(), want)
	}
}

// fakeSHA builds a distinct full revision, the shape a report carries. The first
// seven characters differ, so an abbreviated sha names one commit.
func fakeSHA(n int) string {
	return fmt.Sprintf("%07x%s", n, strings.Repeat("0", 33))
}
