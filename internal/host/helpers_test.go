package host

import "testing"

// testTools is the inventory the tests hand the mechanism. It names the tools
// the real product trusts so the fixtures stay readable, and it is declared here
// rather than imported from the product: a mechanism's tests state their own
// inputs.
var testTools = Tools{
	Port:    []string{"lsof", "ss", "netstat"},
	Process: "ps",
}

// newTestHost returns a host over the real machine with a fixed inventory.
func newTestHost(t *testing.T) *Host {
	t.Helper()
	return New(testTools)
}
