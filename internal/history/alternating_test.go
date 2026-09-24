package history

import (
	"path/filepath"
	"testing"
)

// TestAlternatingCheckoutsKeepTheirOwnBounds pins the multi-checkout lifetime:
// two checkouts deploying in alternation each keep their own bounded stack, a
// move in one never widens the other's, and the round trip through the real
// file preserves both groups.
//
// This is the accumulation case: one checkout updated daily for years, another
// one touched occasionally, and neither may grow the file without bound.
func TestAlternatingCheckoutsKeepTheirOwnBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	store := Store{Path: path, MaxRecords: 3}

	deploy := func(repo string, commit string, at int64) File {
		file, _, err := store.Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		records := file.Records(repo)
		records = Visit(records, Record{Commit: commit, At: at}, store.maxRecords())
		next := file.With(repo, records)
		if err := store.Save(next); err != nil {
			t.Fatalf("save: %v", err)
		}
		return next
	}

	deploy("/a", "a3", 3)
	deploy("/b", "b1", 4)
	deploy("/a", "a2", 5)
	deploy("/b", "b2", 5)
	deploy("/a", "a1", 6)
	file := deploy("/a", "a0", 7)

	aRecords := file.Records("/a")
	bRecords := file.Records("/b")
	if len(aRecords) != 3 {
		t.Fatalf("checkout /a holds %d positions, want the bound 3", len(aRecords))
	}
	if len(bRecords) != 2 {
		t.Fatalf("checkout /b holds %d positions, want the 2 it was given", len(bRecords))
	}
	if aRecords[0].Commit != "a0" || aRecords[len(aRecords)-1].Commit != "a2" {
		t.Fatalf("checkout /a = %+v, want the newest three of its own positions", aRecords)
	}
	if bRecords[0].Commit != "b2" || bRecords[1].Commit != "b1" {
		t.Fatalf("checkout /b = %+v, want its own positions newest first", bRecords)
	}

	// The same document must read back through the real file: the bound holds
	// across generations, not only inside one call.
	reloaded, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("reload: ok=%v err=%v", ok, err)
	}
	if got := len(reloaded.Records("/a")); got != 3 {
		t.Fatalf("reloaded /a holds %d positions, want the bound", got)
	}
}
