package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
	"github.com/rhczz/dshctl/internal/host"
)

// probeFailure is what a host whose port probe cannot look at the port answers.
var probeFailure = errors.New("the port probe could not run")

// TestStopRefusesWhenThePortCannotBeProbed pins the fail-closed side of the
// stop's first observation: a port the platform cannot look at is never read as
// "nothing to stop". The probe failing is a Preflight refusal before any
// signal, so a stop never acts on a port it could not see.
func TestStopRefusesWhenThePortCannotBeProbed(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	f.host.listenErr = probeFailure

	_, err := f.Stop(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), probeFailure.Error()) {
		t.Fatalf("error = %v, want the probe failure kept", err)
	}
	f.wantNoSignals(t)
	f.wantRecordOnDisk(t)
}

// TestStopRefusesWhenTheWaitCannotSeeThePort pins the fail-closed side of the
// wait for stillness: once the stop has delivered its signals, a probe that
// fails is a refusal — never a claim that the port is free. Without it, a stop
// whose follow-up probing broke would report success and retire the record of a
// server it never saw end.
func TestStopRefusesWhenTheWaitCannotSeeThePort(t *testing.T) {
	f := newFixture(t)
	f.startServer(t, 4321, "")
	// The wait happens after the signals: the first graceful request arms the
	// probe failure, so the stop reaches the wait with a port it can no longer
	// look at.
	f.host.signalErr = func(host.Request) error {
		f.host.mu.Lock()
		f.host.listenErr = probeFailure
		f.host.mu.Unlock()
		return nil
	}

	_, err := f.Stop(context.Background())
	wantCode(t, err, exitcode.Preflight)
	if !strings.Contains(err.Error(), probeFailure.Error()) {
		t.Fatalf("error = %v, want the probe failure kept", err)
	}
	// The graceful request was delivered; nothing else was, and the record
	// stays so the operator's next look describes the same server.
	f.wantSignals(t, []fakeSignal{{4321, host.Graceful}})
	f.wantRecordOnDisk(t)
}
