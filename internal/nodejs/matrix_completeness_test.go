package nodejs

import (
	"sort"
	"strings"
	"testing"
)

// The decision table is the contract this package implements, and every row of
// it is a case in this suite. The lists below are frozen: adding a row without
// adding its case — or deleting a case without deciding that its row is gone —
// fails here rather than passing quietly.
//
// R rows: resolution (nodejs_test.go).
// G rows: the version gate (gate_test.go).

// frozenResolveRows is the R section of the decision table.
var frozenResolveRows = []string{
	"R1",  // nothing asked for: PATH serves the runtime, its release is read from the binary
	"R2",  // nothing asked for and PATH has no node
	"R3",  // nothing asked for and the node on PATH cannot be asked what it is
	"R4",  // the resolver reports what it found; the gate judges it
	"R5",  // a requested release that only PATH has (Homebrew, n, the official installer)
	"R6",  // a requested release a version manager holds: wins over PATH, costs no process
	"R7",  // the same through fnm
	"R8",  // a requested release installed nowhere: the report names the request and PATH
	"R9",  // a request is satisfied by that release, not a neighbouring one
	"R10", // a forwarding shim on PATH: the real interpreter is used
	"R11", // the exec-path probe fails: the PATH hit is still usable
	"R12", // a request that names nothing at all
	"R13", // no home: the version managers are not searched through a relative path
}

// frozenGateRows is the G section of the decision table.
var frozenGateRows = []string{
	"G1", // below the floor: refused, with the remedy
	"G2", // the floor itself: accepted (the boundary from below)
	"G3", // the verified release: accepted
	"G4", // a newer patch of the verified major version: accepted
	"G5", // an unverified major version: reported, not refused
	"G6", // an older major version: refused
	"G7", // pre-release and build suffixes do not move a release between majors
}

// TestResolutionMatrixIsComplete fails when a decision-table row has no case.
func TestResolutionMatrixIsComplete(t *testing.T) {
	assertRows(t, "R", frozenResolveRows, resolveRows)
}

// TestGateMatrixIsComplete fails when a gate row has no case.
func TestGateMatrixIsComplete(t *testing.T) {
	assertRows(t, "G", frozenGateRows, gateRows)
}

// assertRows compares a frozen row list with the cases that implement it, in
// both directions: a missing case and an unregistered case both fail.
func assertRows(t *testing.T, prefix string, frozen []string, cases map[string]func(*testing.T)) {
	t.Helper()
	if len(frozen) != len(cases) {
		t.Fatalf("%s rows = %d (frozen list) %d (cases), want the same", prefix, len(frozen), len(cases))
	}
	seen := map[string]bool{}
	for _, id := range frozen {
		if seen[id] {
			t.Fatalf("frozen list repeats %s", id)
		}
		seen[id] = true
		fn, ok := cases[id]
		if !ok {
			t.Errorf("decision table row %s has no case", id)
			continue
		}
		if fn == nil {
			t.Errorf("decision table row %s has an empty case", id)
		}
		if !strings.HasPrefix(id, prefix) {
			t.Errorf("the frozen list's %q prefix is not %q", id, prefix)
		}
	}
	extra := make([]string, 0, len(cases))
	for id := range cases {
		if !seen[id] {
			extra = append(extra, id)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("case %v is not registered in the frozen decision table", extra)
	}
}
