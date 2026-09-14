//go:build unix

package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAcquireCreatesAnOwnerOnlyLockFile pins the mode of the lock file.
//
// The pid inside is not a secret, but the state directory holds documents that
// are, and a lock file that anybody can rewrite would let another user replace
// the record while an operation runs. os.OpenFile only applies the mode when it
// creates the file, so this pins that the create path really asks for 0600.
//
// The process umask is set explicitly: otherwise the assertion would depend on
// the developer's shell profile rather than on the code.
func TestAcquireCreatesAnOwnerOnlyLockFile(t *testing.T) {
	previous := syscall.Umask(0o022)
	defer syscall.Umask(previous)

	path := lockPath(t)
	held, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		held.Release()
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	held.Release()
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat after Release: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after Release = %v, want 0600", info.Mode().Perm())
	}
}

// TestHeldReportsAnErrorWhenTheParentIsARegularFile pins that a lock path whose
// directory slot is occupied by a file is an error, never "the lock is free".
//
// The state directory is disposable, so a plain missing directory is not a
// problem, but a *file* where the directory belongs is a different answer: the
// lock cannot be inspected at all. Reporting free there is the failure that
// matters, because a caller would then start an operation that a real holder is
// already running.
//
// The test is Unix-only because the operating systems answer differently: Unix
// reports ENOTDIR, which reaches this package as a distinct error, while
// Windows maps the same shape to "the path does not exist" and Held answers
// free (pinned in lock_windows_test.go). Acquire refuses on both, which is the
// half every platform shares.
func TestHeldReportsAnErrorWhenTheParentIsARegularFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	holder, locked, err := Held(filepath.Join(parent, "dshctl.lock"))
	if err == nil {
		t.Fatalf("Held = (%d, %v, nil), want an error for an uninspectable lock", holder, locked)
	}
	if locked {
		t.Fatalf("Held reported locked=%v for a lock it could not read", locked)
	}
}

// TestTimeoutErrorReportsNoHolderForAMalformedRecord pins that a pid is only
// reported when it was really read from the record.
//
// The record is a diagnostic and can be anything: a truncated write from a
// killed holder, a foreign file that happens to sit at the path, or bytes
// written by a version with a different format. Reporting whatever those bytes
// happen to parse to puts a pid in the message that names an unrelated process,
// and an operator reading "pid=4711" goes looking for a dshctl operation that
// does not exist. The honest answer is 0, which the message renders without any
// pid claim at all.
//
// This test only runs where locking is advisory: Windows byte-range locks are
// mandatory, so a second handle cannot rewrite the record of a holder in the
// same process and the malformed record could not be staged there.
func TestTimeoutErrorReportsNoHolderForAMalformedRecord(t *testing.T) {
	cases := []struct {
		name   string
		record string
	}{
		{"empty", ""},
		{"not a number", "not a pid\n"},
		{"oversized", strings.Repeat("9", recordBytes*2) + "\n"},
		{"trailing junk", "1234 and more\n"},
		{"negative", "-1\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := lockPath(t)
			holder, err := Acquire(context.Background(), path, time.Second)
			if err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			defer holder.Release()

			// Rewrite the record behind the holder's back, through a separate
			// handle: the holder's own file handle keeps the lock, exactly as
			// if a leftover or a foreign file occupied the path.
			record, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
			if err != nil {
				t.Fatalf("open record: %v", err)
			}
			if _, err := record.WriteString(testCase.record); err != nil {
				record.Close()
				t.Fatalf("write record: %v", err)
			}
			if err := record.Close(); err != nil {
				t.Fatalf("close record: %v", err)
			}

			_, err = Acquire(context.Background(), path, 50*time.Millisecond)
			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("Acquire = %v, want a TimeoutError while the lock is held", err)
			}
			if timeoutErr.Holder != 0 {
				t.Fatalf("Holder = %d for a %s record, want 0", timeoutErr.Holder, testCase.name)
			}
			if strings.Contains(timeoutErr.Error(), "pid=") {
				t.Fatalf("message = %q, want no pid claim for a %s record",
					timeoutErr.Error(), testCase.name)
			}
		})
	}
}

// The test below is Unix-only because its premise cannot be staged on Windows:
// removing a lock file that a holder still has open is refused by the operating
// system there ("being used by another process"), so the shape it pins — a
// holder left on an unlinked inode with a waiter behind it — does not exist.
// What Windows does instead is pinned in lock_windows_test.go.

// TestDeletingTheLockFileDoesNotDeadlockTheWaiter is the regression test for
// removing the state directory while an operation runs: the waiting operation
// must still end up holding the lock at the fresh path after the first holder
// releases, instead of waiting forever on an unlinked inode.
//
// What this test cannot observe without a seam is whether the two holders ever
// overlapped: the waiter is expected to acquire the recreated file only after
// the first holder releases, and the assertion that pins that sequencing is the
// fresh acquire succeeding promptly once the release happened.
func TestDeletingTheLockFileDoesNotDeadlockTheWaiter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dshctl.lock")

	first, err := Acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	// A second operation is waiting while the state directory is removed.
	secondResult := make(chan error, 1)
	go func() {
		second, err := Acquire(context.Background(), path, 3*time.Second)
		if err != nil {
			secondResult <- err
			return
		}
		second.Release()
		secondResult <- nil
	}()

	time.Sleep(100 * time.Millisecond)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove state dir: %v", err)
	}
	// The first holder now holds a lock on an unlinked inode; releasing it must
	// let the waiter through rather than deadlocking.
	first.Release()

	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatalf("the waiting operation failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting operation never acquired the lock")
	}
}
