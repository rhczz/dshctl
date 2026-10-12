//go:build unix && !linux

package host

// startTimeFromProc reports that this Unix has no procfs to read a process start
// time from, which leaves the ps-based path in charge.
func startTimeFromProc(int) (int64, bool) { return 0, false }

// parentFromProc reports that this Unix has no procfs to read a parent pid
// from, which leaves the ps-based path in charge.
func parentFromProc(int) (int, bool) { return 0, false }
