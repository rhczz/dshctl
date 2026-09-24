package logging

import "testing"

// TestLevelStringRendersTheSetting pins how a level reads back in the verbose
// output and in the document: the zero value is "unset" (the caller did not
// choose), the four levels spell themselves, and nothing else exists.
func TestLevelStringRendersTheSetting(t *testing.T) {
	cases := []struct {
		level Level
		want  string
	}{
		{Level(0), "unset"},
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
	}
	for _, testCase := range cases {
		if got := testCase.level.String(); got != testCase.want {
			t.Fatalf("Level(%d).String() = %q, want %q", testCase.level, got, testCase.want)
		}
	}
}
