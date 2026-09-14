//go:build darwin

package host

// netstatArgs asks for the BSD dialect of the TCP table, which includes the
// protocol column the parser keys on.
func netstatArgs() []string { return []string{"-an", "-p", "tcp"} }
