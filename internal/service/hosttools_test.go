package service

import "testing"

// TestTheProbeInventoryTrustsTheToolThatNamesOwners pins the product's order.
//
// The order is a decision, not a detail: lsof names the owning process, ss does
// the same on Linux, and netstat is the last resort that may only be able to say
// "something is listening". A probe that cannot name the owner makes a start
// wait for a process it cannot confirm, so putting a table reader first is a
// change no test would otherwise notice.
func TestTheProbeInventoryTrustsTheToolThatNamesOwners(t *testing.T) {
	if len(hostTools.Port) != 3 {
		t.Fatalf("hostTools.Port = %v, want three tools", hostTools.Port)
	}
	if hostTools.Port[0] != "lsof" || hostTools.Port[1] != "ss" || hostTools.Port[2] != "netstat" {
		t.Fatalf("hostTools.Port = %v, want lsof, ss, netstat in that order", hostTools.Port)
	}
	if hostTools.Process != "ps" {
		t.Fatalf("hostTools.Process = %q, want ps", hostTools.Process)
	}
}
