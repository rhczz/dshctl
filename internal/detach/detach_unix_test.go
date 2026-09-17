//go:build unix

package detach

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestCommandGivesTheChildItsOwnSession pins the Unix mechanism this package
// exists for: setsid(2) is what lets a server outlive the terminal that ran
// dshctl, so a command built without it must not pass as detached.
//
// The attribute is asserted first and the kernel's answer second, because the
// two fail differently: a dropped attribute says the package stopped asking for
// detachment, while a child still sitting in dshctl's session says the request
// never reached the kernel.
func TestCommandGivesTheChildItsOwnSession(t *testing.T) {
	name, args := sleeper(t, 30*time.Second)
	cmd := Command(name, args...)
	if cmd.SysProcAttr == nil {
		t.Fatal("Command() returned a nil SysProcAttr: without it the server stays in the session that started dshctl and a closing terminal takes it down")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("SysProcAttr.Setsid is not set: without it the server stays in the session that started dshctl and a closing terminal takes it down")
	}

	process, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reapInCleanup(t, process)

	own, err := sessionID(os.Getpid())
	if err != nil {
		t.Fatalf("cannot read session id of pid %d: %v", os.Getpid(), err)
	}
	// The kernel is asked with a bounded wait: Start hands back a child that has
	// already exec'd, so the first read is expected to answer, and the bound
	// only keeps a scheduling hiccup from being reported as a session that was
	// never left.
	deadline := time.Now().Add(5 * time.Second)
	var child int
	for {
		if child, err = sessionID(process.PID); err != nil {
			t.Fatalf("cannot read session id of pid %d: %v", process.PID, err)
		}
		if child != own {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child (pid %d) is still in dshctl's session %d, want a session of its own", process.PID, own)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if child != process.PID {
		t.Fatalf("child (pid %d) session id = %d, want %d: a session leader's session id is its own pid", process.PID, child, process.PID)
	}
}

// sessionID reports a process's session id.
//
// The syscall is issued directly because Go's syscall package exports a Getsid
// wrapper only on some Unixes — linux, the platform dshctl runs on, has none —
// while the kernel entry point is the same everywhere the call exists.
func sessionID(pid int) (int, error) {
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(sid), nil
}
