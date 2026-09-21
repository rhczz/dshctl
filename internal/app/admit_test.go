package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rhczz/dshctl/internal/state"
)

// TestOccupantIsExactlyWhatNoVerbMayActOn is the table the mutating verbs read:
// every state either belongs to dshctl or it does not, and a survivor belongs to
// it. A verb may only signal what this answers false for.
func TestOccupantIsExactlyWhatNoVerbMayActOn(t *testing.T) {
	cases := []struct {
		name     string
		observed observed
		want     bool
	}{
		{"running", observed{status: Status{State: StateRunning}}, false},
		{"starting", observed{status: Status{State: StateStarting}}, false},
		{"stopped", observed{status: Status{State: StateStopped}}, false},
		{"unobservable", observed{status: Status{State: StateUnobservable}}, false},
		{"foreign", observed{status: Status{State: StateForeign}}, true},
		{"orphan", observed{status: Status{State: StateOrphan}}, true},
		{"survivor", observed{status: Status{State: StateOrphan, Survivor: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.observed.occupant(); got != tc.want {
				t.Errorf("occupant() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAdmitSurvivorAdoptsAndReobserves pins the two halves of the verdict: the
// adoption happens, and the caller is handed the state the record now describes
// rather than the one that made adoption necessary.
func TestAdmitSurvivorAdoptsAndReobserves(t *testing.T) {
	f := newFixture(t)
	seedInterruptedStart(t, f, 8000, 8001, true)
	before, err := f.observe(context.Background())
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if before.status.State != StateOrphan || !before.status.Survivor {
		t.Fatalf("seeded state = %+v, want an orphan survivor", before.status)
	}

	verdict, after, err := f.admitSurvivor(context.Background(), before)
	if err != nil {
		t.Fatalf("admitSurvivor: %v", err)
	}
	if verdict != adoptDone {
		t.Fatalf("verdict = %v, want adoptDone", verdict)
	}
	if after.status.State != StateRunning || !after.status.RecordLive {
		t.Fatalf("re-observation = %+v, want the adopted server running", after.status)
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("the adopted survivor must have a record")
	}
	if record.PID != 8001 || record.SpawnedPID != 8000 {
		t.Fatalf("record = %+v, want the listener adopted with the wrapper kept", record)
	}
}

func TestAdmitSurvivorSaysNothingWhenThereIsNone(t *testing.T) {
	f := newFixture(t)
	before, err := f.observe(context.Background())
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	verdict, after, err := f.admitSurvivor(context.Background(), before)
	if err != nil {
		t.Fatalf("admitSurvivor: %v", err)
	}
	if verdict != adoptNothing {
		t.Fatalf("verdict = %v, want adoptNothing", verdict)
	}
	if after.status.State != before.status.State {
		t.Fatalf("observation changed from %q to %q", before.status.State, after.status.State)
	}
}

// TestAdmitSurvivorReportsOneItCannotRecord pins the failure verdict: a survivor
// whose record cannot be rebuilt is nobody's to signal, and the caller has to say
// so instead of acting on the old, wrong record.
func TestAdmitSurvivorReportsOneItCannotRecord(t *testing.T) {
	f := newFixture(t)
	seedInterruptedStart(t, f, 8000, 8001, true)
	before, err := f.observe(context.Background())
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "record-as-directory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	f.Record = state.Store{Path: dir}

	verdict, _, err := f.admitSurvivor(context.Background(), before)
	if err != nil {
		t.Fatalf("admitSurvivor: %v", err)
	}
	if verdict != adoptFailed {
		t.Fatalf("verdict = %v, want adoptFailed", verdict)
	}
}
