//go:build unix

package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// timeNow is a seam so the elapsed-time arithmetic can be asserted.
func timeNow() int64 { return time.Now().Unix() }

// stubTool writes an executable that ignores its arguments and prints output.
func stubTool(t *testing.T, dir, name, output string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "EOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return path
}

// TestInspectRejectsImpossiblePIDs pins that a nonsense pid never reaches the
// kernel and never appears alive.
func TestInspectRejectsImpossiblePIDs(t *testing.T) {
	host := New(testTools)
	for _, pid := range []int{0, -1} {
		facts := host.Inspect(context.Background(), pid)
		if facts.Alive {
			t.Fatalf("Inspect(%d).Alive = true", pid)
		}
		if host.Alive(context.Background(), pid) {
			t.Fatalf("Alive(%d) = true", pid)
		}
		if err := host.Signal(pid, Graceful); err == nil {
			t.Fatalf("Signal(%d) must be refused", pid)
		}
	}
}

// TestAliveReportsThisProcess pins the existence probe.
func TestAliveReportsThisProcess(t *testing.T) {
	host := New(testTools)
	if !host.Alive(context.Background(), os.Getpid()) {
		t.Fatal("this process must be reported as alive")
	}
	// The pid is above Linux's largest possible pid (kernel.pid_max defaults to
	// 4194304 on 64-bit systems), so it cannot name a process anywhere.
	if host.Alive(context.Background(), 5_000_000) {
		t.Fatal("a pid that cannot exist was reported as alive")
	}
}

// TestAliveTreatsAPermissionRefusedSignalAsExistence pins the EPERM boundary of
// the existence probe: pid 1 exists on every Unix and belongs to another user,
// so a non-root runner's kill(1, 0) is refused with EPERM — and that refusal is
// proof the process is there, not proof that it is gone.
//
// The boundary is what keeps a server owned by a different user from being
// reported as gone and restarted on top of itself. Root is not special-cased
// here: where the signal is accepted instead, the same expression answers true
// through its other branch, so the assertion holds for every runner.
func TestAliveTreatsAPermissionRefusedSignalAsExistence(t *testing.T) {
	if !New(testTools).Alive(context.Background(), 1) {
		t.Fatal("Alive(1) reported pid 1 as gone: a permission refusal (EPERM, the answer a non-root runner gets) is evidence that the process exists")
	}
}

// TestSignalRejectsNonsense pins that no signal is ever aimed at pid 0, which
// would mean "every process in the group".
func TestSignalRejectsNonsense(t *testing.T) {
	if err := New(testTools).Signal(0, Force); err == nil {
		t.Fatal("Signal(0) must be refused")
	}
}

// TestInspectNeverReportsAProcessAsGoneWhenItCannotLook pins the contract that
// outranks every detail: a probe that could not read anything still knows the
// process exists, because the existence check is the kernel's and needs no tool.
func TestInspectNeverReportsAProcessAsGoneWhenItCannotLook(t *testing.T) {
	home := t.TempDir()
	// A ps that exists but cannot be run: the file has no execute bit, which is
	// what a broken installation or a restricted mount looks like.
	ps := filepath.Join(home, "ps")
	if err := os.WriteFile(ps, []byte("#!/bin/sh\necho 00:01 node\n"), 0o644); err != nil {
		t.Fatalf("write ps: %v", err)
	}
	host := &Host{tools: testTools, lookPath: func(name string) (string, error) {
		if name == "ps" {
			return ps, nil
		}
		return "", errTestLookup
	}}

	facts := host.Inspect(context.Background(), os.Getpid())
	if !facts.Alive {
		t.Fatalf("facts = %+v, want a live process: a tool that cannot be run says nothing", facts)
	}
	if facts.Command != "" {
		t.Fatalf("Command = %q, want nothing invented by an unusable tool", facts.Command)
	}
}
