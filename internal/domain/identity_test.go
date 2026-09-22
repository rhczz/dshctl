package domain

import (
	"testing"
	"time"
)

// TestMatches pins the fingerprint comparison that makes pid reuse detectable.
func TestMatches(t *testing.T) {
	const recorded = int64(1_700_000_000)
	tolerance := 5 * time.Second
	cases := []struct {
		name     string
		recorded int64
		observed int64
		want     bool
	}{
		{"exact", recorded, recorded, true},
		{"within tolerance", recorded, recorded + 3, true},
		{"within tolerance backwards", recorded, recorded - 3, true},
		{"just outside", recorded, recorded + 10, false},
		{"recycled much later", recorded, recorded + 9_000, false},
		{"unknown observed time still matches", recorded, 0, true},
		{"unknown recorded time still matches", 0, recorded, true},
		{"both unknown", 0, 0, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Matches(testCase.recorded, testCase.observed, tolerance)
			if got != testCase.want {
				t.Fatalf("Matches = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestMatchesWithASubSecondTolerance pins that a tolerance below one second is
// still a tolerance, and that a negative one is read as "exact only" rather than
// as "nothing matches".
func TestMatchesWithASubSecondTolerance(t *testing.T) {
	const recorded = int64(1_700_000_000)
	cases := []struct {
		name      string
		delta     int64
		tolerance time.Duration
		want      bool
	}{
		{"sub-second tolerance absorbs a zero delta", 0, 500 * time.Millisecond, true},
		{"sub-second tolerance does not absorb a second", 1, 500 * time.Millisecond, false},
		{"negative tolerance still matches exactly", 0, -time.Second, true},
		{"negative tolerance does not match a second later", 1, -time.Second, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Matches(recorded, recorded+testCase.delta, testCase.tolerance)
			if got != testCase.want {
				t.Fatalf("Matches(delta=%ds, tolerance=%s) = %v, want %v",
					testCase.delta, testCase.tolerance, got, testCase.want)
			}
		})
	}
}
