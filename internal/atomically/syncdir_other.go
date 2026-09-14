//go:build !unix

package atomically

// syncDir is a no-op where directory handles cannot be flushed.
func syncDir(string) error { return nil }
