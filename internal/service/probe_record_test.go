package service

import (
	"context"
	"testing"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/run"
)

// TestRecordServesOrFailsKeepsTheFailure pins the fail-closed side of the
// cross-port guard: a port the platform probe cannot look at is not "nobody's
// server" — the failure is kept, so a build refuses instead of guessing that
// the record describes nothing.
//
// The plain recordServes may read "cannot look" as "not serving"; the refusing
// variant must not. This test keeps the two apart.
func TestRecordServesOrFailsKeepsTheFailure(t *testing.T) {
	f := newFixture(t)
	// The pid path is answered before the port is looked at; the survivor path
	// is the one that has to ask the platform. The record names a wrapper that
	// has moved on, so finding the listener requires the probe.
	record := domain.Record{PID: 9999, SpawnedPID: 8000, Port: f.Settings.Port, StartedAt: 1}
	f.host.listenErr = &run.ExitError{Command: "lsof -P -n -iTCP:3080 -sTCP:LISTEN", Code: 1}

	serving, err := f.recordServesOrFails(context.Background(), record)
	if err == nil {
		t.Fatal("a failed probe was read as 'the record serves nothing'")
	}
	if serving {
		t.Fatal("an unprobeable port was reported as served")
	}
}
