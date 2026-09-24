package history

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveEvictsTheStalestCheckoutsToStayUnderItsBound pins the document's own
// lifetime rule. One checkout's stack is bounded per deployment; the document
// itself is bounded by dropping the groups that have not been deployed for the
// longest. Without it, every checkout path an operator ever used keeps its
// group forever, the file crosses maxFileBytes after enough years of moving
// checkouts around, and from then on every update ends in a save failure even
// though the deployment itself succeeded.
//
// The group that was deployed last must survive eviction: its position is the
// one a rollback needs next.
func TestSaveEvictsTheStalestCheckoutsToStayUnderItsBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	box := Store{Path: path, MaxRecords: maxRecordsDefault}

	// Twenty checkouts, each at the full stack bound: far past the file bound.
	// Their deployment times do not overlap, so "stalest" is unambiguous.
	file := File{}
	for group := 0; group < 20; group++ {
		records := make([]Record, 0, maxRecordsDefault)
		for position := 0; position < maxRecordsDefault; position++ {
			records = append(records, Record{
				Commit: fmt.Sprintf("aa11bb22cc33dd44ee55ff667788%06d", group*1000+position),
				At:     int64(group*1000 + position + 1),
			})
		}
		file.Repos = append(file.Repos, Group{
			Repo:    fmt.Sprintf("/checkouts/checkout-%02d", group),
			Records: records,
		})
	}

	if err := box.Save(file); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the document is not on the disk: %v", err)
	}
	if info.Size() > maxFileBytes {
		t.Fatalf("the document is %d bytes, want it evicted under %d", info.Size(), maxFileBytes)
	}

	loaded, ok, err := box.Load()
	if err != nil || !ok {
		t.Fatalf("Load = (%+v, %v, %v), want the document evicted to the bound", loaded, ok, err)
	}
	if got := loaded.Records("/checkouts/checkout-00"); got != nil {
		t.Fatalf("the stalest checkout kept %d positions, want its group evicted", len(got))
	}
	newest := loaded.Records("/checkouts/checkout-19")
	if len(newest) != maxRecordsDefault {
		t.Fatalf("the newest deployment holds %d positions, want the full stack", len(newest))
	}
	if newest[0].At != 19*1000+1 || newest[len(newest)-1].At != 19*1000+50 {
		t.Fatalf("the newest group reads back as At %d..%d, want its own 19_001..19_050",
			newest[0].At, newest[len(newest)-1].At)
	}
}
