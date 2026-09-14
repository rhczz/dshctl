//go:build linux

package host

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// procSysUptime is the kernel's own uptime, in seconds.
const procSysUptime = "/proc/uptime"

// procStatHz is the clock tick rate /proc reports the start time in.
const procStatHz = defaultTickRate

// startTimeFromProc reads a process start time from /proc without running any
// tool.
//
// /proc reports the start time as clock ticks since boot, which is only half of
// the answer: the ticks say how long after boot the process started, and that
// age has to be subtracted from the current time to become the absolute
// timestamp every ownership decision compares. startTimeFromBootTicks owns that
// conversion — and the portable test beside it — because getting the direction
// wrong makes the fingerprint drift, which is what happens when the age is
// returned as if it were a start time.
//
// Returns:
//   - the start time in Unix seconds, and whether it could be read.
func startTimeFromProc(pid int) (int64, bool) {
	ticks, ok := startTicks(pid)
	if !ok {
		return 0, false
	}
	uptime, ok := uptimeSeconds()
	if !ok {
		return 0, false
	}
	return startTimeFromBootTicks(ticks, uptime, time.Now().Unix(), procStatHz), true
}

// startTicks reads the 22nd field of /proc/<pid>/stat, which is the process
// start time in clock ticks.
func startTicks(pid int) (int64, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	return parseStartTicks(string(data))
}

// parseStartTicks extracts the start time from /proc/<pid>/stat content.
func parseStartTicks(text string) (int64, bool) {
	// The second field is the executable name in parentheses and may itself
	// contain spaces, so the split starts after the closing parenthesis.
	close := strings.LastIndexByte(text, ')')
	if close < 0 || close+2 >= len(text) {
		return 0, false
	}
	fields := strings.Fields(text[close+2:])
	// After the name, field 3 is state, so starttime (field 22) is index 19.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return 0, false
	}
	ticks, err := strconv.ParseInt(fields[startTimeIndex], 10, 64)
	if err != nil || ticks < 0 {
		return 0, false
	}
	return ticks, true
}

// uptimeSeconds reads the kernel uptime.
func uptimeSeconds() (float64, bool) {
	data, err := os.ReadFile(procSysUptime)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
