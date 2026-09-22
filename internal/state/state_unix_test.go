//go:build unix

package state

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

// TestAFIFOAtTheRecordPathIsRejectedAndThenCleared pins the hang guard and the
// cleanup that follows it.
//
// Opening a FIFO for reading blocks until a writer appears, so os.ReadFile on
// the record path would never return and every command that reads the record
// would hang instead of reporting a problem. The metadata check refuses it
// before a byte is read, reported as corrupt, and Remove then unlinks the
// residue itself — a state directory that is disposable by design must not be
// left permanently unreadable by a leftover node.
func TestAFIFOAtTheRecordPathIsRejectedAndThenCleared(t *testing.T) {
	box := store(t)
	if err := syscall.Mkfifo(box.Path, 0o600); err != nil {
		t.Skipf("FIFOs are unavailable: %v", err)
	}

	_, ok, err := box.Load()
	if ok {
		t.Fatal("a FIFO at the record path is not a record")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
	if err := box.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(box.Path); !os.IsNotExist(err) {
		t.Fatalf("the FIFO survived Remove: %v", err)
	}
}

// TestSaveIsOwnerOnly pins the permissions of a record that carries a token URL.
//
// The mode bits are a Unix concept: on Windows a writable file reports 0666
// whatever mode was asked for, so the assertion lives here rather than in the
// portable file.
func TestSaveIsOwnerOnly(t *testing.T) {
	box := store(t)
	if err := box.Save(testDoc{PID: 1, Port: 1, Phase: testPhaseRunning}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(box.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}
