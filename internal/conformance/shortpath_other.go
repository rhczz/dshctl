//go:build !windows

package conformance

// shortPath is the identity elsewhere: only Windows has 8.3 names.
func shortPath(string) string { return "" }
