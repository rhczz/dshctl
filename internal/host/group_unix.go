//go:build unix

package host

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// DescendsFrom reports whether pid belongs to the tree of processes the
// ancestor spawned, which is how a detached server started through a wrapper
// is recognized as dshctl's own.
//
// Two pieces of evidence answer, strongest first:
//
//   - The process group. The wrapper becomes the group leader (setsid) and
//     every process started inside the group inherits it. A group outlives its
//     leader, so the answer survives the wrapper itself — the property
//     survivor adoption rests on.
//   - The parent-pid chain. A package-script runner may leave the group: pnpm
//     11 runs every script in a shell it detaches into its own session so a
//     terminal interrupt can be forwarded to the script alone, and the server
//     that binds the port is then outside the wrapper's group although the
//     wrapper plainly started it. The chain is the same evidence Windows
//     answers with, and it is sound while it climbs: a process names its real
//     parent while that parent lives, and an orphan is reparented to init
//     rather than to whatever pid its dead parent's value was recycled to, so
//     a chain that reaches a live ancestor names a tree that ancestor really
//     spawned. A chain through processes that are gone simply stops answering
//     — the refusal is the honest result, never a guess.
//
// Descent is therefore inherited, not reflexive in general — DescendsFrom(pid,
// pid) holds exactly when pid leads its own group, which the spawned wrapper
// always does.
func (h *Host) DescendsFrom(ancestor, pid int) bool {
	if ancestor <= 0 || pid <= 0 {
		return false
	}
	if group, err := syscall.Getpgid(pid); err == nil && group == ancestor {
		return true
	}
	return h.parentChainReaches(ancestor, pid)
}

// parentChainReaches walks the parent-pid chain from pid toward ancestor.
//
// The walk stops at init, at a parent that cannot be read, and at the hop
// bound; every stop that is not a match answers "no evidence", which callers
// treat as "not ours" rather than as "ours".
func (h *Host) parentChainReaches(ancestor, pid int) bool {
	current := pid
	for hop := 0; hop < maxAncestorHops; hop++ {
		parent, ok := h.parentPID(current)
		if !ok {
			return false
		}
		if parent == ancestor {
			return true
		}
		if parent <= 1 || parent == current {
			return false
		}
		current = parent
	}
	return false
}

// parentPID reads the parent of a process.
//
// A direct kernel read is preferred where it exists, exactly as Inspect
// prefers it: it needs no external tool and cannot be denied by a policy that
// still allows signals. Where procfs does not exist, ps answers. A refusal is
// reported as "unknown" — the walk above turns it into "no evidence", never
// into "no parent".
func (h *Host) parentPID(pid int) (int, bool) {
	if parent, ok := parentFromProc(pid); ok {
		return parent, true
	}
	path, found := h.tool(h.tools.Process)
	if !found {
		return 0, false
	}
	output, status, err := runCapture(nil, path, "-p", strconv.Itoa(pid), "-o", "ppid=")
	if err != nil || status != 0 {
		return 0, false
	}
	parent, convErr := strconv.Atoi(strings.TrimSpace(output))
	if convErr != nil || parent <= 0 {
		return 0, false
	}
	return parent, true
}

// GroupExists reports whether any process is left in the group the pid leads.
//
// The question is asked with the kernel directly — signal 0 to the negated
// group id — because asking it through Getpgid confuses "the leader was
// reaped" with "the group is empty": a group whose leader died first still
// answers while a single member lives.
func (h *Host) GroupExists(pid int) bool {
	return groupExists(pid)
}

// KillGroup ends every process in a group without giving any of them a chance to
// clean up.
//
// The caller must have created the group itself: a group id is only meaningful
// while a member is alive, and signalling a recycled one would reach an
// unrelated tree.
func (h *Host) KillGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("refusing to signal the process group of pid %d", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("the process group %d could not be ended: %w", pid, err)
	}
	return nil
}

// SignalGroup asks every process in a group to exit.
func (h *Host) SignalGroup(pid int, request Request) error {
	if pid <= 0 {
		return fmt.Errorf("refusing to signal the process group of pid %d", pid)
	}
	signal := syscall.SIGTERM
	if request == Force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-pid, signal)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("the process group %d could not be sent %v: %w", pid, signal, err)
	}
	return nil
}

// groupExists reports whether any process is left in a group.
//
// Only "no such process group" counts as gone. Any other refusal — a permission
// problem, an interrupted syscall — means the question could not be answered,
// and that is reported as "still there" so a caller keeps waiting or forces the
// tree down instead of concluding it is empty and walking away.
func groupExists(group int) bool {
	if group <= 0 {
		return false
	}
	err := syscall.Kill(-group, syscall.Signal(0))
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true
	case errors.Is(err, syscall.ESRCH):
		return false
	default:
		return true
	}
}
