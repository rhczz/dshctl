package host

import (
	"fmt"
	"strings"
	"testing"
)

// benchmarkNetstatOutput builds netstat output the way a busy machine answers:
// hundreds of established connections plus the one listener the probe asks
// about. `status` runs this on every call, so the parse has to stay linear in
// the output rather than degrading as the socket table grows over uptime.
func benchmarkNetstatOutput(listeners int) string {
	var builder strings.Builder
	builder.WriteString("Active Internet connections (including servers)\nProto Recv-Q Send-Q  Local Address          Foreign Address        (state)\n")
	for index := 0; index < listeners; index++ {
		fmt.Fprintf(&builder, "tcp4       0      0  10.0.0.%d.55%03d        203.0.113.9.443        ESTABLISHED\n", index%250, 30000+index)
	}
	fmt.Fprintf(&builder, "tcp4       0      0  127.0.0.1.3080         *.*                    LISTEN\n")
	return builder.String()
}

// BenchmarkParseNetstatListenerOnABusyTable pins the cost of one port probe
// against a 500-connection table. It is a canary: if the parse ever goes
// quadratic — or scans every line twice — years of uptime with many sockets
// would show up as a slow `status`.
func BenchmarkParseNetstatListenerOnABusyTable(b *testing.B) {
	output := benchmarkNetstatOutput(500)
	for b.Loop() {
		if !parseNetstatListener(output, 3080) {
			b.Fatal("the listener was not found in the table")
		}
	}
}

// BenchmarkParseNetstatListenerOnAQuietTable pins the same parse against a
// table with nothing but the listener: an idle machine must not pay for a scan
// it does not need.
func BenchmarkParseNetstatListenerOnAQuietTable(b *testing.B) {
	output := benchmarkNetstatOutput(0)
	for b.Loop() {
		if !parseNetstatListener(output, 3080) {
			b.Fatal("the listener was not found")
		}
	}
}
