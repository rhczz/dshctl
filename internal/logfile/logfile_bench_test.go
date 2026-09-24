package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchmarkLog builds a log of the given size with realistic line shapes, so a
// tail or a follow sees a file that looks like years of accumulated service
// output rather than a synthetic block of repeated bytes.
func benchmarkLog(b *testing.B, size int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "dsh-web.log")
	line := strings.Repeat("x", 128) + "\n"
	count := size / len(line)
	body := strings.Repeat(line, count)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		b.Fatalf("seed log: %v", err)
	}
	return path
}

// BenchmarkTailReadsABoundedWindow pins what `dshctl logs` pays on a log that
// has grown far past the tail window: the read walks at most maxTailBytes back
// from the end, so the cost stays constant while the file itself grows for
// months. The requested line count exceeds what even the window holds, which is
// what forces the walk all the way to the byte bound.
func BenchmarkTailReadsABoundedWindow(b *testing.B) {
	path := benchmarkLog(b, 10*maxTailBytes)
	b.ResetTimer()
	for b.Loop() {
		data, _, err := readTailLines(path, 100_000, maxTailBytes)
		if err != nil {
			b.Fatalf("readTailLines: %v", err)
		}
		if len(data) > maxTailBytes+512 {
			b.Fatalf("the tail read %d bytes, want the window bound", len(data))
		}
	}
}

// BenchmarkSectionWrites pins the log's append cost: a service that logs a line
// every second pays this for as long as it runs, so a regression here compounds
// over months of uptime.
func BenchmarkSectionWrites(b *testing.B) {
	path := filepath.Join(b.TempDir(), "dsh-web.log")
	logger := New(path, 0, Format{Prefix: "=====", Product: "dshctl", Layout: "2006-01-02 15:04:05"})

	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		if err := logger.Line(fmt.Sprintf("line %d: %s", index, strings.Repeat("x", 96))); err != nil {
			b.Fatalf("Line: %v", err)
		}
	}
}
