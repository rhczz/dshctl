package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
)

// TestAnnouncedURLFiltersByPort is the regression test for the shared log: the
// log holds the announced addresses of every instance in the state directory,
// and reporting another port's token would hand the operator a working token
// for a different server. The address is accepted only when it names the port
// the caller asked about.
func TestAnnouncedURLFiltersByPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.log")
	other := "http://127.0.0.1:4000/?token=other-token"
	mine := "http://127.0.0.1:3080/?token=my-token"
	content := "dsh web: " + other + "\n" + "dsh web: " + mine + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write shared log: %v", err)
	}

	// The truncated flag is asserted too: a found address must not be reported
	// alongside a truncation the caller would then have to explain.
	if got, truncated := announcedURL(path, 3080); truncated || got != mine {
		t.Fatalf("announcedURL(3080) = (%q, %v), want (%q, false)", got, truncated, mine)
	}
	if got, truncated := announcedURL(path, 4000); truncated || got != other {
		t.Fatalf("announcedURL(4000) = (%q, %v), want (%q, false)", got, truncated, other)
	}
	if got, _ := announcedURL(path, 9999); got != "" {
		t.Fatalf("announcedURL(9999) = %q, want nothing", got)
	}
}

// TestWebURLUsesTheLogFallbackForItsOwnPort pins the record-less path: a running
// server whose record carries no address falls back to the address in the
// shared log, filtered to this port.
func TestWebURLUsesTheLogFallbackForItsOwnPort(t *testing.T) {
	f := newFixture(t)
	other := "http://127.0.0.1:4000/?token=other-token"
	mine := "http://127.0.0.1:" + strconv.Itoa(f.Settings.Port) + "/?token=my-token"
	for _, line := range []string{"dsh web: " + other, "dsh web: " + mine} {
		if err := f.Log.Line(line); err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}
	// A running server with an address-less record.
	f.startServer(t, 4321, "")

	address, err := f.WebURL(context.Background())
	if err != nil {
		t.Fatalf("WebURL: %v", err)
	}
	if address != mine {
		t.Fatalf("WebURL = %q, want %q", address, mine)
	}
}

// TestWebURLRefusesWhenNothingRuns pins that the command answers only while a
// server of ours is running: printing a dead address — and the token in it — is
// a "fact" an operator only discovers in the browser.
func TestWebURLRefusesWhenNothingRuns(t *testing.T) {
	f := newFixture(t)
	if err := f.Log.Line("dsh web: http://127.0.0.1:" + strconv.Itoa(f.Settings.Port) + "/?token=stale"); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	_, err := f.WebURL(context.Background())
	if err == nil {
		t.Fatal("WebURL must refuse while nothing is running")
	}
	if code := exitcode.Of(err); code != exitcode.NotRunning {
		t.Fatalf("exit code = %d, want %d (%v)", code, exitcode.NotRunning, err)
	}
}
