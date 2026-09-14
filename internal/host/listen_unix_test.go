//go:build unix

package host

import (
	"context"
	"os"
	"testing"
)

// TestLsofNamesTheSocketThisProcessHolds exercises the lsof probe directly, so
// the tool's argument vector and the pid it reports are pinned against the real
// lsof rather than against a stub that answers whatever it is told.
func TestLsofNamesTheSocketThisProcessHolds(t *testing.T) {
	host := New()
	if _, ok := host.tool("lsof"); !ok {
		t.Skip("lsof is unavailable, so the lsof probe has nothing to ask")
	}
	_, port := bindLoopback(t, "tcp", "127.0.0.1:0")

	result, handled, err := host.listenViaLsof(context.Background(), port)
	if err != nil {
		t.Fatalf("listenViaLsof(%d): %v", port, err)
	}
	if !handled || !result.Listening {
		t.Fatalf("listenViaLsof(%d) = (%+v, %v), want a final listening answer", port, result, handled)
	}
	if result.PID != os.Getpid() {
		t.Fatalf("lsof reported pid %d for a socket this process (%d) holds", result.PID, os.Getpid())
	}
}

// TestLsofDeclinesAFreePort pins that lsof's negative verdict is never final, on
// the real tool: the port is free, lsof says nothing matched, and the probe
// still refuses to end the chain with "free", because an unprivileged lsof omits
// sockets it cannot attribute to a process.
func TestLsofDeclinesAFreePort(t *testing.T) {
	host := New()
	if _, ok := host.tool("lsof"); !ok {
		t.Skip("lsof is unavailable, so the lsof probe has nothing to ask")
	}
	// The reserved port is released before it is probed, so another process can
	// take it in between; a fresh port is tried rather than failing the test for
	// something the test did not do.
	for attempt := 0; attempt < 5; attempt++ {
		port := reserveLoopbackPort(t)
		result, handled, err := host.listenViaLsof(context.Background(), port)
		if err != nil {
			t.Fatalf("listenViaLsof(%d): %v", port, err)
		}
		if result.Listening {
			continue
		}
		if handled {
			t.Fatal("lsof's silence must not end the probe chain")
		}
		return
	}
	t.Fatal("no released loopback port stayed free long enough to be probed")
}

// TestLsofDeclinesAnUnusableSelector pins why lsof's exit status 1 is never
// taken as a final answer, using the ambiguity itself.
//
// The real lsof reports a usage error — an unparsable port selector, for
// instance — with the same status it uses for "nothing matched". The probe
// therefore cannot tell the two apart from the status alone, and the property
// that keeps that harmless is the one asserted here: status 1 is declined, so a
// probe that reads the whole table still gets to speak.
func TestLsofDeclinesAnUnusableSelector(t *testing.T) {
	host := New()
	if _, ok := host.tool("lsof"); !ok {
		t.Skip("lsof is unavailable, so the lsof probe has nothing to ask")
	}
	// Nothing is signalled and no socket is touched: lsof only reads the table.
	result, handled, err := host.listenViaLsof(context.Background(), -1)
	if err != nil {
		t.Fatalf("lsof's status 1 is classified as \"nothing matched\", not as a failure: %v", err)
	}
	if result.Listening {
		t.Fatalf("listenViaLsof(-1) = %+v, want nothing", result)
	}
	if handled {
		t.Fatal("lsof's silence must never be a final answer")
	}
}
