package domain

import "time"

// Matches reports whether the recorded start time and the one the operating
// system reports describe the same process.
//
// The rule exists because a pid is recycled: "the record names this pid" is not
// evidence that the process is the one dshctl started, but "the record names
// this pid and it started when dshctl saw it start" is.
//
// Either side being unknown (zero) matches. That is a deliberate reading, not a
// fallback: on a platform that cannot report a start time, refusing every match
// would make dshctl treat its own running server as a recycled pid and refuse to
// end it, which is worse than the risk the check addresses.
//
// A negative tolerance is read as no tolerance at all — the exact fingerprint
// still matches — because the only alternative reading makes a running server
// look like a stranger.
//
// It takes the two instants rather than a record so this layer stays free of the
// record file and its serialization: the model owns the rule, the store owns the
// bytes.
func Matches(recordedStart, observedStart int64, tolerance time.Duration) bool {
	if observedStart <= 0 || recordedStart <= 0 {
		return true
	}
	if tolerance < 0 {
		tolerance = 0
	}
	delta := recordedStart - observedStart
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(tolerance/time.Second)
}
