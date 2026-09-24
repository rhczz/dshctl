package domain

import "testing"

// TestLabelNamesThePositionOrItsName pins the two shapes a target's label can
// take: a bare short commit when the selector says nothing beyond it, and the
// name in parentheses when it does — the line an operator reads in
// "update: abc → def (dsh-v0.1.0)".
func TestLabelNamesThePositionOrItsName(t *testing.T) {
	cases := []struct {
		name   string
		target Target
		want   string
	}{
		{
			name:   "no name, only the position",
			target: Target{Commit: "1a2b3c4d5e6f"},
			want:   "1a2b3c4",
		},
		{
			name:   "a named version reads as position (name)",
			target: Target{Commit: "1a2b3c4d5e6f", Name: "dsh-v0.1.0"},
			want:   "1a2b3c4 (dsh-v0.1.0)",
		},
		{
			name:   "a short hash stays as it is",
			target: Target{Commit: "abc1234"},
			want:   "abc1234",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.target.Label(); got != testCase.want {
				t.Fatalf("Label() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestShortCommitTruncatesToSevenPinsTheAbbreviation pins git's default
// abbreviation length: an abbreviated sha is never invented from a shorter one,
// and a full sha loses everything past the seventh character.
func TestShortCommitTruncatesToSevenPinsTheAbbreviation(t *testing.T) {
	if got := ShortCommit("1a2b3c4d5e6f"); got != "1a2b3c4" {
		t.Fatalf("ShortCommit(full) = %q, want the seven-character abbreviation", got)
	}
	if got := ShortCommit("abc123"); got != "abc123" {
		t.Fatalf("ShortCommit(short) = %q, want it unchanged", got)
	}
	if got := ShortCommit(""); got != "" {
		t.Fatalf("ShortCommit(empty) = %q, want empty", got)
	}
}
