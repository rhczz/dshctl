//go:build windows

package host

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

// DescendsFrom reports whether pid descends from ancestor, which is how a
// detached server started through a wrapper is recognized as dshctl's own.
//
// Windows has no process group a signal can address, but it does have the
// parent-pid chain, and that is the same evidence: dshctl spawns the pnpm shim
// and the server that binds the port is one of its descendants. Asking the
// question through a snapshot rather than the (nonexistent) group is what makes
// a start on Windows able to recognize its own listener at all.
func (h *Host) DescendsFrom(ancestor, pid int) bool {
	if ancestor <= 0 || pid <= 0 {
		return false
	}
	if ancestor == pid {
		return true
	}
	parents, err := processParents()
	if err != nil {
		return false
	}
	current := uint32(pid)
	for hop := 0; hop < maxAncestorHops; hop++ {
		parent, ok := parents[current]
		if !ok || parent == 0 || parent == current {
			return false
		}
		if parent == uint32(ancestor) {
			return true
		}
		current = parent
	}
	return false
}

// maxAncestorHops bounds the walk: a cycle can only come from a corrupt
// snapshot, and a chain longer than this is not the shape dshctl creates.
const maxAncestorHops = 64

// processParents reads the parent pid of every process in one snapshot.
func processParents() (map[uint32]uint32, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snapshot)

	parents := make(map[uint32]uint32, 256)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
	}
	if len(parents) == 0 {
		return nil, errors.New("the process snapshot is empty")
	}
	return parents, nil
}

// GroupExists reports whether any process is left in the tree the pid roots.
//
// Windows cannot enumerate a process group. The tree taskkill ends is rooted at
// the pid, and taskkill ends children before their parents, so a dead root is
// the best available answer for "the tree is gone". A root that exists but may
// not be opened counts as existing, and a failure to read its state counts as
// existing too: the caller then forces the tree down and checks the port, which
// is the fact that actually matters — concluding "empty" on a failed probe
// would leave a server serving while the cleanup reports success.
func (h *Host) GroupExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := openProcess(pid)
	if err != nil {
		return processAccessDenied(err)
	}
	defer procCloseHandle.Call(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(syscall.Handle(handle), &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}

// KillGroup ends a process and everything it started.
//
// taskkill walks the tree, which is what reaches the script pnpm spawned.
func (h *Host) KillGroup(pid int) error {
	return taskkillTree(pid)
}

// SignalGroup ends the tree: Windows has no graceful signal to send.
func (h *Host) SignalGroup(pid int, _ Request) error {
	return taskkillTree(pid)
}

// taskkillTree ends a process and its descendants.
func taskkillTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run(); err != nil {
		return err
	}
	return nil
}
