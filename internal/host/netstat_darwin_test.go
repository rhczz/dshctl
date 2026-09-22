//go:build darwin

package host

import (
	"context"
	"slices"
	"testing"
)

// TestNetstatArgsAskForTheBSDDialect pins the argument vector the probe hands to
// the real netstat.
//
// The Linux and BSD netstat binaries do not accept the same selector: the BSD
// dialect needs `-p tcp` to print the protocol column the parser keys on, and
// `-n` so that addresses stay numeric instead of being resolved through DNS —
// which would make a port probe depend on the network being reachable.
func TestNetstatArgsAskForTheBSDDialect(t *testing.T) {
	want := []string{"-an", "-p", "tcp"}
	got := netstatArgs()
	if !slices.Equal(got, want) {
		t.Fatalf("netstatArgs() = %v, want %v", got, want)
	}
}

// TestNetstatFindsASocketThisProcessHolds runs the real BSD netstat through the
// probe, so the arguments and the parser are pinned against the tool the platform
// actually ships rather than against captured output.
func TestNetstatFindsASocketThisProcessHolds(t *testing.T) {
	host := New(testTools)
	if _, ok := host.tool("netstat"); !ok {
		t.Skip("netstat is unavailable, so the netstat probe has nothing to ask")
	}
	_, port := bindLoopback(t, "tcp", "127.0.0.1:0")

	result, handled, err := host.listenViaNetstat(context.Background(), port)
	if err != nil {
		t.Fatalf("listenViaNetstat(%d): %v", port, err)
	}
	if !handled || !result.Listening {
		t.Fatalf("listenViaNetstat(%d) = (%+v, %v), want a final listening answer", port, result, handled)
	}
	// BSD netstat does not report the owning process, and the probe must not
	// pretend it does: an invented pid would be signalled by a later stop.
	if result.PID != 0 {
		t.Fatalf("listenViaNetstat(%d).PID = %d, want 0 because netstat cannot name the owner", port, result.PID)
	}

	// The free-port half is retried with a fresh port: the reservation is
	// released before the probe runs, so a process that took it in between is a
	// fact about the machine, not a failure of the probe.
	for attempt := 0; attempt < 5; attempt++ {
		free := reserveLoopbackPort(t)
		if free == port {
			continue
		}
		result, handled, err = host.listenViaNetstat(context.Background(), free)
		if err != nil {
			t.Fatalf("listenViaNetstat(%d): %v", free, err)
		}
		if !handled {
			t.Fatalf("listenViaNetstat(%d) declined to answer", free)
		}
		if result.Listening {
			continue
		}
		return
	}
	t.Fatal("no released loopback port stayed free long enough to be probed")
}
