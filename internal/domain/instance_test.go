package domain

import "testing"

// TestStateOccupant is the table every mutating verb reads: a state either
// belongs to dshctl or it does not, and a survivor belongs to it. A verb may
// only signal what this answers false for.
func TestStateOccupant(t *testing.T) {
	cases := []struct {
		state    State
		survivor bool
		want     bool
	}{
		{StateRunning, false, false},
		{StateStarting, false, false},
		{StateStopped, false, false},
		{StateUnobservable, false, false},
		{StateForeign, false, true},
		{StateOrphan, false, true},
		{StateOrphan, true, false},
	}
	for _, testCase := range cases {
		got := testCase.state.Occupant(testCase.survivor)
		if got != testCase.want {
			t.Errorf("%q survivor=%v: Occupant = %v, want %v", testCase.state, testCase.survivor, got, testCase.want)
		}
	}
}

func TestOwningIsRunningOrStarting(t *testing.T) {
	cases := []struct {
		state State
		want  bool
	}{
		{StateRunning, true},
		{StateStarting, true},
		{StateStopped, false},
		{StateForeign, false},
		{StateOrphan, false},
		{StateUnobservable, false},
	}
	for _, testCase := range cases {
		status := Status{State: testCase.state}
		if got := status.Owning(); got != testCase.want {
			t.Errorf("%q: Owning = %v, want %v", testCase.state, got, testCase.want)
		}
	}
}
