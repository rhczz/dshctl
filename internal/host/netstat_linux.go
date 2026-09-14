//go:build linux

package host

// netstatArgs asks for the Linux dialect of the TCP table.
func netstatArgs() []string { return []string{"-an", "-t"} }
