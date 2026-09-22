//go:build linux

package host

import (
	"context"
	"os"
	"slices"
	"testing"
)

// TestNetstatArgsAskForTheLinuxDialect pins the argument vector the probe hands
// to the real netstat.
//
// GNU netstat takes `-t` for TCP and rejects the BSD `-p tcp` form; `-n` keeps
// addresses numeric so a port probe never waits on DNS.
func TestNetstatArgsAskForTheLinuxDialect(t *testing.T) {
	want := []string{"-an", "-t"}
	got := netstatArgs()
	if !slices.Equal(got, want) {
		t.Fatalf("netstatArgs() = %v, want %v", got, want)
	}
}

// TestNetstatFindsASocketThisProcessHolds runs the real netstat through the
// probe where the distribution still ships it. net-tools is optional on modern
// images, so the absence of the tool is reported rather than silently accepted.
func TestNetstatFindsASocketThisProcessHolds(t *testing.T) {
	host := New(testTools)
	if _, ok := host.tool("netstat"); !ok {
		t.Skip("netstat is not installed, so the netstat probe has nothing to ask")
	}
	_, port := bindLoopback(t, "tcp", "127.0.0.1:0")

	result, handled, err := host.listenViaNetstat(context.Background(), port)
	if err != nil {
		t.Fatalf("listenViaNetstat(%d): %v", port, err)
	}
	if !handled || !result.Listening {
		t.Fatalf("listenViaNetstat(%d) = (%+v, %v), want a final listening answer", port, result, handled)
	}
	// netstat only reports a pid when it is asked with -p and is allowed to read
	// the socket's owner; the probe must not invent one either way.
	if result.PID != 0 {
		t.Fatalf("listenViaNetstat(%d).PID = %d, want 0 because netstat cannot name the owner", port, result.PID)
	}
}

// TestSSNamesTheSocketThisProcessHolds runs the real ss through the probe: it is
// the fallback Linux uses when lsof is not installed, which is the common case
// on a slim image.
func TestSSNamesTheSocketThisProcessHolds(t *testing.T) {
	host := New(testTools)
	if _, ok := host.tool("ss"); !ok {
		t.Skip("ss is not installed, so the ss probe has nothing to ask")
	}
	_, port := bindLoopback(t, "tcp", "127.0.0.1:0")

	result, handled, err := host.listenViaSS(context.Background(), port)
	if err != nil {
		t.Fatalf("listenViaSS(%d): %v", port, err)
	}
	if !handled || !result.Listening {
		t.Fatalf("listenViaSS(%d) = (%+v, %v), want a final listening answer", port, result, handled)
	}
	// ss prints users:(("node",pid=...)) only when it may look at the socket's
	// owner; when it may not, the probe answers "something listens" with no pid
	// rather than guessing.
	if result.PID != 0 && result.PID != os.Getpid() {
		t.Fatalf("listenViaSS(%d).PID = %d, want 0 or this process", port, result.PID)
	}
}

// TestSSReportsAFreePortAsFree pins ss's negative verdict, which — unlike
// lsof's — is final because ss reads the whole table.
func TestSSReportsAFreePortAsFree(t *testing.T) {
	host := New(testTools)
	if _, ok := host.tool("ss"); !ok {
		t.Skip("ss is not installed, so the ss probe has nothing to ask")
	}
	// Retried with a fresh port: the reservation is released before the probe
	// runs, and a port another process took in between is not this probe's
	// failure.
	for attempt := 0; attempt < 5; attempt++ {
		port := reserveLoopbackPort(t)
		result, handled, err := host.listenViaSS(context.Background(), port)
		if err != nil {
			t.Fatalf("listenViaSS(%d): %v", port, err)
		}
		if !handled {
			t.Fatal("ss reads the whole table, so its answer must be final")
		}
		if result.Listening {
			continue
		}
		return
	}
	t.Fatal("no released loopback port stayed free long enough to be probed")
}
