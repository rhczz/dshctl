package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/service"
)

// TestTimelineGapNeverSaysUpToDateWithoutAConfirmedFetch pins the honesty rule
// from the README: a fetch that failed must never let the gap line read "up to
// date", whatever the local commit counts are, because "the same" against the
// last known remote is not "confirmed against the remote".
func TestTimelineGapNeverSaysUpToDateWithoutAConfirmedFetch(t *testing.T) {
	cases := []struct {
		name    string
		report  service.TimelineReport
		banned  string
		require string
	}{
		{
			name:    "no gap, fetch failed",
			report:  service.TimelineReport{Remote: service.TimelineRemote{Name: "origin/master"}, Fetched: false},
			banned:  "up to date",
			require: "not confirmed",
		},
		{
			name: "behind, fetch failed",
			report: service.TimelineReport{
				Behind: 2,
				Remote: service.TimelineRemote{Name: "origin/master"},
				Commits: []service.TimelineCommit{
					{Commit: "r", Short: "r", Subject: "remote", Remote: true},
					{Commit: "m", Short: "m", Subject: "middle", Skipped: 1},
					{Commit: "c", Short: "c", Subject: "current", Current: true},
				},
			},
			banned:  "up to date",
			require: "not confirmed",
		},
		{
			name: "ahead, fetch failed",
			report: service.TimelineReport{
				Ahead:  1,
				Remote: service.TimelineRemote{Name: "origin/master"},
			},
			banned:  "up to date",
			require: "not confirmed",
		},
		{
			name: "diverged, fetch failed",
			report: service.TimelineReport{
				Behind: 1,
				Ahead:  1,
				Remote: service.TimelineRemote{Name: "origin/master"},
			},
			banned:  "up to date",
			require: "not confirmed",
		},
		{
			name: "no gap, fetch succeeded keeps the confirmed wording",
			report: service.TimelineReport{
				Fetched: true,
				Remote:  service.TimelineRemote{Name: "origin/master"},
			},
			require: "up to date (origin/master)",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := timelineGap(testCase.report)
			if testCase.banned != "" && strings.Contains(got, testCase.banned) {
				t.Fatalf("gap = %q, must not claim %q when the fetch did not confirm it", got, testCase.banned)
			}
			if testCase.require != "" && !strings.Contains(got, testCase.require) {
				t.Fatalf("gap = %q, want it to say %q", got, testCase.require)
			}
		})
	}
}

// TestPrintTimelineMarksAnUnconfirmedRemoteNamesTheFetchReason pins that the
// remote row tells the operator why the remote is unavailable rather than
// printing an unexplained "(unknown)".
func TestPrintTimelineMarksAnUnconfirmedRemoteNamesTheFetchReason(t *testing.T) {
	var out bytes.Buffer
	report := service.TimelineReport{
		RepoDir:    "/repo",
		Current:    service.TimelineCurrent{Commit: "c", Short: "c", Branch: "main"},
		FetchError: "git fetch exited 128",
		Behind:     1,
		Remote:     service.TimelineRemote{Name: "origin/master", Commit: "r", Short: "r"},
	}
	if err := printTimeline(&out, report); err != nil {
		t.Fatalf("printTimeline: %v", err)
	}
	if !strings.Contains(out.String(), "unavailable (git fetch exited 128)") {
		t.Fatalf("output = %q, want the fetch reason named", out.String())
	}
	if !strings.Contains(out.String(), "not confirmed") {
		t.Fatalf("output = %q, want the gap marked not confirmed", out.String())
	}
}
