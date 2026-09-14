package detach

import "time"

// reapGrace is how long Exited waits for a just-ended child to be collected
// before reporting that it is still running.
const reapGrace = 25 * time.Millisecond

// newTimer returns a channel that fires after d. It is a variable so tests can
// shorten the grace period.
var newTimer = func(d time.Duration) <-chan time.Time { return time.After(d) }
