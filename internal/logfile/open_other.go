//go:build !windows

package logfile

import "os"

// openForFollow opens the log for the follower. On Unix an open handle does not
// keep the file from being renamed or unlinked, so the ordinary open is enough.
func openForFollow(path string) (*os.File, error) { return os.Open(path) }
