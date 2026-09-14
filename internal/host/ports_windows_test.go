//go:build windows

package host

import (
	"context"
	"os"
	"testing"
	"time"
)

// These tests can only run on Windows. They are deliberately small and written so
// that every expectation can be checked by reading the implementation: a test in
// this file that is wrong would fail in CI on a platform nobody debugs locally.

// TestPortOfSwapsTheTableByteOrder pins the byte order of a MIB_TCPROW port field.
//
// iphlpapi stores the port as a network-order 16-bit value inside a
// little-endian DWORD, so the two bytes of the host-order port are reversed in the
// word: 3080 (0x0C08) is stored as 0x080C, and 8080 (0x1F90) as 0x901F. The
// listening table is the only source of port ownership on Windows, so a lookup
// that compared the wrong byte order would report a plainly occupied port as free
// and dshctl would start a second server on it.
//
// The end-to-end proof of the direction is TestListeningFindsThisProcess in
// probe_windows_test.go, which binds a real loopback port and asks the probe:
// if portOf read the word the other way round, that test could not pass.
func TestPortOfSwapsTheTableByteOrder(t *testing.T) {
	cases := []struct {
		name string
		raw  uint32
		want int
	}{
		// 3080 = 0x0C08: the word carries the low byte of the port first.
		{"3080", 0x0000080C, 3080},
		// 8080 = 0x1F90. Writing the port's own hex value here (0x00001F90) would
		// be the byte-swapped reading of the word, which is port 36895.
		{"8080", 0x0000901F, 8080},
		// 80 = 0x0050, so the word is 0x5000.
		{"80", 0x00005000, 80},
		// Asymmetric pair: 256 = 0x0100 is stored as 0x0001 and 1 = 0x0001 as
		// 0x0100, so a reader that only swapped the high half would pass one and
		// fail the other.
		{"256", 0x00000001, 256},
		{"1", 0x00000100, 1},
		// The highest port is its own byte swap, which is why it alone cannot
		// distinguish the two readings.
		{"highest port", 0x0000FFFF, 65535},
		{"no port", 0, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := portOf(testCase.raw); got != testCase.want {
				t.Fatalf("portOf(%#08x) = %d, want %d", testCase.raw, got, testCase.want)
			}
		})
	}
}

// TestProcessStartTimeReadsALiveProcess pins the Windows fingerprint: the creation
// time of a process that is running right now has to come back as a plausible Unix
// timestamp.
//
// The start time is what tells a reused pid apart from the server a state record
// describes, so a conversion that is off by the FILETIME epoch (1601 instead of
// 1970) or that loses the 100ns ticks would make every record look foreign — and a
// server that is refused as "not ours" is never stopped.
func TestProcessStartTimeReadsALiveProcess(t *testing.T) {
	pid := os.Getpid()
	handle, err := openProcess(pid)
	if err != nil {
		t.Fatalf("openProcess(%d): %v: reading this process's own times needs no elevation", pid, err)
	}
	defer procCloseHandle.Call(handle)

	startedAt, ok := processStartTime(handle)
	if !ok {
		t.Fatalf("processStartTime(%d) reported no creation time", pid)
	}
	now := time.Now().Unix()
	if startedAt <= 0 {
		t.Fatalf("startedAt = %d, want a positive Unix timestamp", startedAt)
	}
	if startedAt > now+60 {
		t.Fatalf("startedAt = %d is in the future (now = %d)", startedAt, now)
	}
	// The test binary started seconds ago, so anything older than an hour means
	// the conversion, not the clock, is wrong.
	if now-startedAt > 3600 {
		t.Fatalf("startedAt = %d is more than an hour before now = %d", startedAt, now)
	}
}

// TestProcessStartTimeRejectsAHandleThatNamesNothing pins that a pid the API will
// not open yields no fingerprint rather than a wrong one.
//
// A creation time of 0 or of the FILETIME epoch would compare unequal to every real
// start time, so an invented value is as harmful as a missing one: it makes a live
// server look like a stranger. Reading is the only thing these calls do — no
// handle here is ever used to terminate anything.
func TestProcessStartTimeRejectsAHandleThatNamesNothing(t *testing.T) {
	// Pid 0 is the system idle process, which OpenProcess always refuses. The
	// second value is a poser: Windows recycles released pids, so the highest pid
	// a machine ever hands out tracks how many processes are alive at once rather
	// than how many were ever created, and a value in the millions is never
	// reached. It is also not a multiple of 4, which is the only alignment Windows
	// pids have.
	const bogusPID = 1<<22 + 2
	for _, pid := range []int{0, bogusPID} {
		handle, err := openProcess(pid)
		if err == nil {
			procCloseHandle.Call(handle)
			t.Fatalf("openProcess(%d) succeeded, want a refusal for a pid that names nothing", pid)
		}
		if handle != 0 {
			t.Fatalf("openProcess(%d) = %#x, want the zero handle on failure", pid, handle)
		}
		if startedAt, ok := processStartTime(handle); ok {
			t.Fatalf("processStartTime(%#x) = (%d, true), want not-ok for a handle that names nothing", handle, startedAt)
		}
		if facts := New().Inspect(context.Background(), pid); facts.Alive {
			t.Fatalf("Inspect(%d).Alive = true, want false", pid)
		}
	}
}
