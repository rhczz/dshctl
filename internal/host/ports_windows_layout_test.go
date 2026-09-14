//go:build windows

package host

import (
	"testing"
	"unsafe"
)

// TestTCPTableRowSizes pins the layouts the probe reads the kernel's tables
// with.
//
// The rows are parsed through unsafe over a buffer the operating system filled,
// so a structure that no longer matches MIB_TCPROW_OWNER_PID (24 bytes) or
// MIB_TCP6ROW_OWNER_PID (52 bytes) reads fields out of the middle of another
// one — which is exactly how every IPv6 listener came to be reported as a free
// port. The sizes are worth asserting on their own, because the mistake is
// invisible in review: both structures are plausible and only the offsets
// differ.
func TestTCPTableRowSizes(t *testing.T) {
	if got := unsafe.Sizeof(mibTCPRow{}); got != 24 {
		t.Fatalf("mibTCPRow is %d bytes, want the 24 of MIB_TCPROW_OWNER_PID", got)
	}
	if got := unsafe.Sizeof(mibTCP6Row{}); got != 52 {
		t.Fatalf("mibTCP6Row is %d bytes, want the 52 of MIB_TCP6ROW_OWNER_PID", got)
	}
	// The port field is where the structure claims it is: the kernel stores it
	// in network byte order in the low half of the word, so 0x1234 is written
	// as 0x3412 and read back by portOf.
	row := mibTCP6Row{LocalPort: 0x3412, OwningPID: 4321}
	if got := portOf(row.LocalPort); got != 0x1234 {
		t.Fatalf("portOf(LocalPort) = %#x, want 0x1234: the port is not where this structure says it is", got)
	}
}
