//go:build windows

package host

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// Windows API declarations used by the process and port probes.
var (
	modIPHelper       = syscall.NewLazyDLL("iphlpapi.dll")
	procExtendedTCP   = modIPHelper.NewProc("GetExtendedTcpTable")
	modKernel32       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess   = modKernel32.NewProc("OpenProcess")
	procGetTimes      = modKernel32.NewProc("GetProcessTimes")
	procCloseHandle   = modKernel32.NewProc("CloseHandle")
	procQueryFullPath = modKernel32.NewProc("QueryFullProcessImageNameW")
)

// Access rights and table selectors.
const (
	// processQueryLimitedInformation is enough to read the times and the image
	// name of any process, and it works without elevation.
	processQueryLimitedInformation = 0x1000
	// processTerminate is what TerminateProcess demands of its handle. Without
	// it every termination request fails with ERROR_ACCESS_DENIED and stop
	// can never end the server it started.
	processTerminate = 0x0001
	// tcpTableOwnerPIDListener lists listening TCP endpoints with their owner.
	tcpTableOwnerPIDListener = 3
	// errorInsufficientBuffer tells us the required size.
	errorInsufficientBuffer = 122
	// addressFamilyINET is Winsock's AF_INET, the IPv4 table selector.
	//
	// The table API rejects AF_UNSPEC (0) with ERROR_INVALID_PARAMETER — asking
	// for "both families at once" is not a thing this call does; the two tables
	// have to be requested by name. Passing 0 made every port probe fail on
	// Windows with a GetExtendedTcpTable failure, so `status`, `start` and
	// `stop` all reported that the port state was unknown instead of answering.
	addressFamilyINET = 2
	// addressFamilyINET6 is Winsock's AF_INET6, the IPv6 table selector.
	addressFamilyINET6 = 23
	// windowsEpochOffset is the number of 100ns ticks between 1601-01-01 and
	// 1970-01-01, the epoch difference between Windows FILETIME and Unix time.
	windowsEpochOffset = 116444736000000000
)

// tcpRow is one listening endpoint, whichever table it came from.
type tcpRow struct {
	localPort uint32
	owningPID uint32
}

// mibTCPRow mirrors MIB_TCPROW_OWNER_PID.
type mibTCPRow struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// mibTCPTable mirrors MIB_TCPTABLE_OWNER_PID.
type mibTCPTable struct {
	NumEntries uint32
	Table      [1]mibTCPRow
}

// mibTCP6Row mirrors MIB_TCP6ROW_OWNER_PID, which is 56 bytes: both addresses
// are 16 bytes and both scope ids are present.
//
// The IPv6 row is not the IPv4 row with wider addresses. Every field sits at a
// different offset, and the structure has a remote scope id between the remote
// address and the remote port — the field whose absence made the row read as 52
// bytes. Parsing a 56-byte row with that layout takes the port out of the middle
// of an address and reports every IPv6 listener as a free port, which is the one
// answer a start must never be given.
type mibTCP6Row struct {
	LocalAddr     [16]byte
	LocalScopeID  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeID uint32
	RemotePort    uint32
	State         uint32
	OwningPID     uint32
}

// mibTCP6Table mirrors MIB_TCP6TABLE_OWNER_PID.
type mibTCP6Table struct {
	NumEntries uint32
	Table      [1]mibTCP6Row
}

// Listening reports which process is listening on a loopback port.
//
// The table is read from iphlpapi rather than by parsing netstat output, so the
// answer is exact, needs no external tool, and names the owning process. It is
// also a single point of failure by design: unlike Unix, which falls back from
// lsof to ss to netstat, Windows has one API to ask, and when it refuses the
// port is reported as unknown rather than as free.
func (h *Host) Listening(_ context.Context, port int) (PortResult, error) {
	rows, err := listenTable()
	if err != nil {
		return PortResult{}, fmt.Errorf("%s: %w", i18nLine(MsgPortIndeterminate, port), err)
	}
	for _, row := range rows {
		if portOf(row.localPort) == port {
			return PortResult{Listening: true, PID: int(row.owningPID)}, nil
		}
	}
	return PortResult{}, nil
}

// listenTable reads the listening TCP table for both address families, so an
// IPv6-only listener cannot hide from the port probe.
func listenTable() ([]tcpRow, error) {
	var rows []tcpRow
	for _, family := range []uint32{addressFamilyINET, addressFamilyINET6} {
		familyRows, err := listenTableForFamily(family)
		if err != nil {
			return nil, err
		}
		rows = append(rows, familyRows...)
	}
	return rows, nil
}

// listenTableForFamily reads the listening TCP table for one address family.
func listenTableForFamily(family uint32) ([]tcpRow, error) {
	var size uint32
	// The argument order is the API's, not the obvious one:
	// GetExtendedTcpTable(table, size, order, addressFamily, tableClass, reserved).
	// The family and the table class are both small integers and were passed the
	// other way round, which makes the call fail with ERROR_INVALID_PARAMETER
	// (87) for every port: the probe reported "unknown" instead of ever
	// answering, so on Windows `start` refused everything and `stop` refused to
	// end a server it had started.
	status, _, _ := procExtendedTCP.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		1, // bOrder: sort by address
		uintptr(family),
		uintptr(tcpTableOwnerPIDListener),
		0, // reserved
	)
	if status != errorInsufficientBuffer && status != 0 {
		return nil, fmt.Errorf("%s", i18nLine(MsgTcpTableFailed, status))
	}
	if size == 0 {
		return nil, nil
	}
	buffer := make([]byte, size)
	status, _, _ = procExtendedTCP.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
		1,
		uintptr(family),
		uintptr(tcpTableOwnerPIDListener),
		0,
	)
	if status != 0 {
		return nil, fmt.Errorf("%s", i18nLine(MsgTcpTableFailed, status))
	}
	rows := make([]tcpRow, 0, 8)
	if family == addressFamilyINET6 {
		table := (*mibTCP6Table)(unsafe.Pointer(&buffer[0]))
		count := int(table.NumEntries)
		if count <= 0 {
			return nil, nil
		}
		for _, row := range unsafe.Slice(&table.Table[0], count) {
			rows = append(rows, tcpRow{localPort: row.LocalPort, owningPID: row.OwningPID})
		}
		return rows, nil
	}
	table := (*mibTCPTable)(unsafe.Pointer(&buffer[0]))
	count := int(table.NumEntries)
	if count <= 0 {
		return nil, nil
	}
	for _, row := range unsafe.Slice(&table.Table[0], count) {
		rows = append(rows, tcpRow{localPort: row.LocalPort, owningPID: row.OwningPID})
	}
	return rows, nil
}

// portOf converts a table port field, which is stored big-endian in the low
// half of the word, into a host-order port number.
func portOf(raw uint32) int {
	return int((raw&0xFF)<<8 | (raw>>8)&0xFF)
}

// Inspect reports what the operating system will say about a process.
//
// A process that exists but may not be opened — a protected or elevated one —
// is still reported as alive: not being allowed to look is not the same as the
// process being gone, and the difference decides whether a runtime record is
// retired or kept.
func (h *Host) Inspect(_ context.Context, pid int) Facts {
	facts := Facts{PID: pid}
	if pid <= 0 {
		return facts
	}
	handle, err := openProcess(pid)
	if err != nil {
		facts.Alive = processAccessDenied(err)
		facts.Source = i18nLine(MsgOpenProcessDenied)
		return facts
	}
	defer procCloseHandle.Call(handle)
	facts.Alive = true
	if startedAt, ok := processStartTime(handle); ok {
		facts.StartedAt = startedAt
		facts.Source = "GetProcessTimes"
	}
	if image := processImagePath(handle); image != "" {
		facts.Command = image
	}
	if facts.Source == "" {
		facts.Source = "OpenProcess"
	}
	return facts
}

// processImagePath reads the executable path of an open process.
func processImagePath(handle uintptr) string {
	buffer := make([]uint16, syscall.MAX_LONG_PATH)
	size := uint32(len(buffer))
	ok, _, _ := procQueryFullPath.Call(
		handle,
		0, // no extra flags
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ok == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:size])
}

// Alive reports whether the process exists and has not exited.
//
// A process owned by another user, or protected from inspection, answers
// OpenProcess with ERROR_ACCESS_DENIED — the Windows equivalent of Unix's
// EPERM, and equally proof that the process is there.
func (h *Host) Alive(_ context.Context, pid int) bool {
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
		return false
	}
	const stillActive = 259
	return code == stillActive
}

// processAccessDenied reports whether OpenProcess refused because of
// permissions rather than because the pid names nothing.
func processAccessDenied(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

// Signal delivers a termination request.
//
// Windows has no portable graceful signal, so both strengths end the process.
// dshctl only ever calls this on a process whose identity it verified.
func (h *Host) Signal(pid int, _ Request) error {
	if pid <= 0 {
		return fmt.Errorf("%s", i18nLine(MsgRefuseSignal, pid))
	}
	handle, err := openProcessAccess(pid, processTerminate)
	if err != nil {
		return fmt.Errorf("%s: %w", i18nLine(MsgPIDNotFound, pid), err)
	}
	defer procCloseHandle.Call(handle)
	if err := syscall.TerminateProcess(syscall.Handle(handle), 1); err != nil {
		return fmt.Errorf("%s: %w", i18nLine(MsgSignalFailed, pid, "kill"), err)
	}
	return nil
}

// openProcess opens a handle with the least privilege that can read times.
func openProcess(pid int) (uintptr, error) {
	return openProcessAccess(pid, processQueryLimitedInformation)
}

// openProcessAccess opens a handle with a specific access right. The right
// matters: TerminateProcess requires PROCESS_TERMINATE, and a handle opened
// without it fails with ERROR_ACCESS_DENIED on every use.
func openProcessAccess(pid int, access uint32) (uintptr, error) {
	handle, _, callErr := procOpenProcess.Call(
		uintptr(access),
		0,
		uintptr(pid),
	)
	if handle == 0 {
		if callErr == nil {
			callErr = errors.New(i18nLine(MsgOpenProcessEmpty))
		}
		return 0, callErr
	}
	return handle, nil
}

// processStartTime converts a process creation time into Unix seconds.
func processStartTime(handle uintptr) (int64, bool) {
	var creation, exit, kernel, user syscall.Filetime
	ok, _, _ := procGetTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ok == 0 {
		return 0, false
	}
	ticks := int64(creation.HighDateTime)<<32 | int64(creation.LowDateTime)
	if ticks <= windowsEpochOffset {
		return 0, false
	}
	seconds := (ticks - windowsEpochOffset) / 10000000
	return seconds, true
}
