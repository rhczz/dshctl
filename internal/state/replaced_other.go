//go:build !windows

package state

// recordBeingReplaced reports whether a failed read only means "the file was
// being replaced just then". POSIX guarantees a reader sees either the old or the
// new document, so a failure is never transient and never excused.
func recordBeingReplaced(error) bool { return false }
