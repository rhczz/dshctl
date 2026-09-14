package host

import (
	"os"
	"strconv"
	"strings"
)

// The helper-process tests re-execute this test binary as a child that takes a
// special branch. Two properties keep that mechanism from being hijacked by the
// ambient environment:
//
//   - the marker value names the parent pid, and the child only takes the
//     helper branch when the value names its own parent (os.Getppid). An
//     exported variable can carry any value, but it cannot name a parent that
//     is not there.
//   - the marker key is stripped from the child's environment before the real
//     value is appended. Unix resolves a duplicated key to its FIRST
//     occurrence and Windows reads the first block entry, so a plain append
//     would be shadowed by an ambient value and the child would re-run the
//     test body instead of the helper — spawning grandchildren forever.

// helperMarker renders the value the parent writes and the child verifies.
func helperMarker(parentPID int) string { return "child-of-" + strconv.Itoa(parentPID) }

// helperKey reports whether the environment key belongs to a helper marker.
func helperKey(key string) bool {
	return strings.HasPrefix(key, "DSHCTL_TEST_") || strings.HasPrefix(key, "DSHCTL_HOST_")
}

// envForChild returns the current environment with every helper marker removed,
// plus the given entries appended.
func envForChild(extra ...string) []string {
	environment := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found && helperKey(key) {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, extra...)
}
