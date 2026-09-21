package kernel

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/domain"
)

// corruptRecordContent is not a runtime record in any shape dshctl writes: a
// half-written file, a hand-edited one, and a file left by something that is not
// dshctl all look like this.
const corruptRecordContent = "{\"pid\": 4321, \"started"

// TestStatusReportsACorruptRecordAsStaleWithoutRetiringIt pins the reporting
// side of a record that cannot be read. It describes nothing usable, so status
// says so — but status is read-only, so the bytes stay exactly where they are
// for a mutating command to clear.
func TestStatusReportsACorruptRecordAsStaleWithoutRetiringIt(t *testing.T) {
	f := newFixture(t)
	f.seedCorruptRecord(t, corruptRecordContent)

	status, err := f.Status(context.Background(), f.Settings.Port)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != domain.StateStopped {
		t.Fatalf("state = %q, want %q: a corrupt record describes no server", status.State, domain.StateStopped)
	}
	if !status.RecordStale {
		t.Fatal("a record that cannot be parsed must be reported as stale")
	}
	if status.StaleRecord != nil {
		t.Fatalf("StaleRecord = %+v, want nil: there is no parsed record to hand over", status.StaleRecord)
	}
	if status.RecordedPID != 0 || status.URLFromRecord != "" {
		t.Fatalf("status = %+v, want no fields taken from an unparsable record", status)
	}
	data, readErr := os.ReadFile(f.Settings.StateFile())
	if readErr != nil {
		t.Fatalf("the corrupt record was removed by status: %v", readErr)
	}
	if string(data) != corruptRecordContent {
		t.Fatalf("record content = %q, want it left untouched", data)
	}
}

// TestDoctorExplainsACorruptRecord pins the diagnosis. The row has to tell the
// operator two things the state alone does not: the file is not readable as a
// record, and what will happen to it. A row that only said "stale" would send
// them looking for a pid that was never there.
func TestDoctorExplainsACorruptRecord(t *testing.T) {
	f := newFixture(t)
	f.seedCorruptRecord(t, corruptRecordContent)

	checks := f.Doctor(context.Background())
	record := checkNamed(t, checks, "运行记录")
	if record.Status != CheckWarn {
		t.Fatalf("运行记录 check = %+v, want a warning", record)
	}
	if !strings.Contains(record.Detail, f.Record.Path) || !strings.Contains(record.Detail, "无法解析") {
		t.Fatalf("运行记录 detail = %q, want it to name the unparsable file", record.Detail)
	}
	if !strings.Contains(record.Detail, "下次 start/stop 会重建它") {
		t.Fatalf("运行记录 detail = %q, want the promise this test checks below", record.Detail)
	}
	// Nothing is listening and no server exists, so the port row is the ordinary
	// idle one even though a record is present.
	if port := checkNamed(t, checks, "端口"); port.Status != CheckOK {
		t.Fatalf("端口 check = %+v, want ok while the port is free", port)
	}
}

// TestStopClearsACorruptRecord pins the promise doctor makes about a record it
// cannot parse: the next start or stop rebuilds it.
//
// A record that cannot be parsed describes nothing, so a stop has no pid to
// verify and nothing usable to keep. Leaving the bytes behind would make every
// later read trip over them in the same way and would contradict the diagnosis
// the operator was just given, so the mutating command retires them — while
// signalling nothing at all, because there is no verified owner to signal.
func TestStopClearsACorruptRecord(t *testing.T) {
	f := newFixture(t)
	f.seedCorruptRecord(t, corruptRecordContent)

	result, err := f.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if result.Status.State != domain.StateStopped {
		t.Fatalf("state = %q, want %q", result.Status.State, domain.StateStopped)
	}
	f.wantNoSignals(t)
	// What the operator was promised ("下次 start/stop 会重建它") must hold: after
	// a stop there is no usable record left.
	if _, statErr := os.Lstat(f.Settings.StateFile()); !os.IsNotExist(statErr) {
		t.Fatalf("stop left the corrupt record at %s (err=%v)", f.Settings.StateFile(), statErr)
	}
}

// TestStartReplacesACorruptRecord pins the other half of the promise: a start
// proceeds over an unreadable record and leaves a valid one behind. Without
// this, an interrupted write would wedge every later start.
func TestStartReplacesACorruptRecord(t *testing.T) {
	f := newFixture(t)
	f.seedCorruptRecord(t, corruptRecordContent)
	f.host.spontaneouslyServed = true

	result, err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	record, ok := f.stateRecord(t)
	if !ok {
		t.Fatal("a successful start must leave a readable record")
	}
	if record.SpawnedPID != result.SpawnedPID || record.PID == 0 {
		t.Fatalf("record = %+v, want the process this start created (spawned %d)", record, result.SpawnedPID)
	}
}

// checkNamed returns one doctor row, failing when it is missing.
func checkNamed(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("doctor did not report %q: %+v", name, checks)
	return Check{}
}
