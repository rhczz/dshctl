package host

import "testing"

// TestStartTimeFromBootTicksIsAnAbsoluteTime pins the conversion that turns a
// boot-relative tick count into the timestamp the runtime record carries.
//
// The bug it is here for is quiet rather than loud: returning the process's age
// instead of its start time still yields a plausible-looking number, and a
// single reading is indistinguishable from a correct one. What gives it away is
// the second reading — the age has moved on, so two readings of the same process
// differ by the time between them, and every ownership check that compares a
// record against a live process stops matching once the difference passes the
// tolerance. On Linux that meant `status` reporting a server dshctl had started
// as a stranger, and `stop` refusing to end it.
//
// The arithmetic is portable even though the file that feeds it is not, so the
// expectation lives here and runs on every platform.
func TestStartTimeFromBootTicksIsAnAbsoluteTime(t *testing.T) {
	const (
		now  = int64(1_700_000_000)
		rate = int64(100)
	)
	cases := []struct {
		name   string
		ticks  int64
		uptime float64
		now    int64
		rate   int64
		want   int64
		why    string
	}{
		{
			name:   "started half a second ago",
			ticks:  6040, // 60.40s after boot
			uptime: 60.9,
			now:    now,
			rate:   rate,
			want:   now,
			why:    "an age below one second is still a start time of now, not zero",
		},
		{
			name:   "started an hour after boot, read an hour later",
			ticks:  3600 * rate,
			uptime: 7200,
			now:    now,
			rate:   rate,
			want:   now - 3600,
			why:    "the start time is when the process began, not how long it has run",
		},
		{
			name:   "uptime behind the ticks counts as just started",
			ticks:  6000,
			uptime: 10,
			now:    now,
			rate:   rate,
			want:   now,
			why:    "a clock that moved backwards must not produce a start before the epoch",
		},
		{
			name:   "a missing tick rate falls back to the documented one",
			ticks:  3600 * rate,
			uptime: 7200,
			now:    now,
			rate:   0,
			want:   now - 3600,
			why:    "the default is what proc(5) documents",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := startTimeFromBootTicks(testCase.ticks, testCase.uptime, testCase.now, testCase.rate)
			if got != testCase.want {
				t.Fatalf("startTimeFromBootTicks(%d ticks, %.1fs uptime, now=%d, rate=%d) = %d, want %d: %s",
					testCase.ticks, testCase.uptime, testCase.now, testCase.rate, got, testCase.want, testCase.why)
			}
		})
	}
}

// TestStartTimeFromBootTicksIsStableAcrossReadings pins the property the record
// depends on: the same process read twice describes the same start time.
//
// This is the case a single-reading test cannot see, and the shape that made
// Linux unusable past the matching tolerance: the conversion subtracted the tick
// count from the wrong side, so each reading reported how long the process had
// been running at that moment.
func TestStartTimeFromBootTicksIsStableAcrossReadings(t *testing.T) {
	const (
		now  = int64(1_700_000_000)
		rate = int64(100)
	)
	ticks := int64(3600 * rate) // the process started an hour after boot
	first := startTimeFromBootTicks(ticks, 7200, now, rate)
	second := startTimeFromBootTicks(ticks, 7200+600, now+600, rate)
	if first != second {
		t.Fatalf("two readings of one process disagree: %d then %d (drift %d s, tolerance is seconds)",
			first, second, second-first)
	}
	if want := now - 3600; first != want {
		t.Fatalf("start time = %d, want %d", first, want)
	}
}
