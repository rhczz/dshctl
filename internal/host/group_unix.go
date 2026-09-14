//go:build unix

package host

import (
	"errors"
	"fmt"
	"syscall"
)

// DescendsFrom reports whether pid is a member of the process group the
// ancestor leads, which is how a detached server started through a wrapper is
// recognized as dshctl's own.
//
// Unix answers it with the process group: the wrapper becomes the group leader
// (setsid) and every process it starts inherits that group, so membership is
// exactly descent for the shape dshctl creates. Descent is therefore
// inherited, not reflexive in general — DescendsFrom(pid, pid) holds exactly
// when pid leads its own group, which the spawned wrapper always does.
func (h *Host) DescendsFrom(ancestor, pid int) bool {
	if ancestor <= 0 || pid <= 0 {
		return false
	}
	group, err := syscall.Getpgid(pid)
	if err != nil || group <= 0 {
		return false
	}
	return group == ancestor
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
		return fmt.Errorf("拒绝向 pid %d 的进程组发送信号", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("无法结束进程组 %d: %w", pid, err)
	}
	return nil
}

// SignalGroup asks every process in a group to exit.
func (h *Host) SignalGroup(pid int, request Request) error {
	if pid <= 0 {
		return fmt.Errorf("拒绝向 pid %d 的进程组发送信号", pid)
	}
	signal := syscall.SIGTERM
	if request == Force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-pid, signal)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("无法向进程组 %d 发送 %v: %w", pid, signal, err)
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
