package service

import (
	"fmt"
	"testing"

	"github.com/rhczz/dshctl/internal/repo"
)

// BenchmarkTimelineRowsOnALargeGap simulates the lifetime worst case for a
// checkout that is never updated: the gap to origin/master grows with every
// deployment on the remote side, and the version list has to walk all of it
// once (every tag inside the gap is shown, even below the display window).
//
// It is a canary, not a tuning tool: if the row build ever goes quadratic — a
// map lookup per row is fine, a scan per row is not — years of accumulated gap
// would show up as a `timeline` that takes seconds to answer.
func BenchmarkTimelineRowsWithALargeGap(b *testing.B) {
	const gap = 10_000
	commits := make([]repo.Commit, 0, gap)
	for index := 0; index < gap; index++ {
		full := fmt.Sprintf("%040d", index)
		commits = append(commits, repo.Commit{Full: full, Short: full[:7], Subject: fmt.Sprintf("subject %d", index)})
	}
	tags := make(map[string][]string)
	remote := commits[0].Full
	current := commits[gap-1]

	b.ResetTimer()
	for b.Loop() {
		rows := timelineRows(commits, tags, remote, current)
		if len(rows) == 0 {
			b.Fatal("no rows built")
		}
	}
}
