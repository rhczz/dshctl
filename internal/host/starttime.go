package host

// defaultTickRate is the clock tick rate /proc is read with unless a platform
// supplies its own. proc(5) documents the starttime field as "clock ticks
// (divide by sysconf(_SC_CLK_TCK))", and _SC_CLK_TCK is 100 on Linux.
const defaultTickRate = 100

// startTimeFromBootTicks turns a process start time expressed as clock ticks
// since boot into the absolute timestamp the runtime record compares.
//
// The arithmetic is the whole fingerprint, and getting its direction wrong is
// quiet: ticks/tickRate is how long *after boot* the process started, so
// subtracting that age from the current time is what makes the value absolute.
// Returning the age itself — the shape this had — still produces a plausible
// looking number, but two readings of the same process then differ by the time
// between them. A server that had been up longer than the matching tolerance
// was consequently reported as a different process: `status` called it foreign,
// and `stop` refused to end the server dshctl had started itself.
//
// Parameters:
//   - ticks: the process start time in clock ticks since boot.
//   - uptime: seconds since boot, as the kernel reports them.
//   - now: the current time in Unix seconds.
//   - tickRate: clock ticks per second; a non-positive value uses the default.
//
// Returns:
//   - the start time in Unix seconds. An age that computes as negative (a clock
//     that moved backwards, or a start rounded into the future) counts as zero,
//     because the alternative is a timestamp before the epoch.
func startTimeFromBootTicks(ticks int64, uptime float64, now int64, tickRate int64) int64 {
	if tickRate <= 0 {
		tickRate = defaultTickRate
	}
	age := uptime - float64(ticks)/float64(tickRate)
	if age < 0 {
		age = 0
	}
	return now - int64(age)
}
