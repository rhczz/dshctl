//go:build linux

package host

import (
	"os"
	"strconv"
	"strings"
)

// procSysUptime is the kernel's own uptime, in seconds.
const procSysUptime = "/proc/uptime"

// procStatHz is the kernel's clock tick rate.
const procStatHz = 100

// startTimeFromProc reads a process start time from /proc without running any
// tool.
//
// /proc reports the start time as clock ticks since boot, so it is combined with
// the kernel's uptime. This is the most reliable fingerprint available on Linux:
// it needs no external binary and works in containers that ship no ps.
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
	seconds := uptime - float64(ticks)/procStatHz
	if seconds < 0 {
		seconds = 0
	}
	return int64(seconds), true
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
