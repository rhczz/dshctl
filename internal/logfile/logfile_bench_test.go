package logfile

import (
	"bytes"
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
	if err := writeBytes(path, []byte(body)); err != nil {
		b.Fatalf("seed log: %v", err)
	}
	return path
}

func writeBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// BenchmarkTailFullFileReadsTheLastLines pins what `dshctl logs -n 200` pays on
// a log that has grown to the rotation threshold: the tail is bounded by
// maxTailBytes, so this stays constant instead of growing with the file.
func BenchmarkTailFullFileRead(b *testing.B) {
	path := benchmarkLog(b, maxTailBytes)
	sink := &bytes.Buffer{}
	b.ResetTimer()
	for b.Loop() {
		_, _, err := readTailLines(path, 200, maxTailBytes)
		if err != nil {
			b.Fatalf("readTailLines: %v", err)
		}
		sink.Reset()
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

// BenchmarkWriteFileAndReplace pins the cost of one atomic state write — the
// settings document and the runtime record are rewritten on every mutating
// command, so this is the per-operation floor.
func BenchmarkWriteFileAndReplace(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "dsh-web-3080.state.json")
	payload := fmt.Appendf(nil, `{"pid":%d,"port":3080,"startedAt":1234567890,"url":"http://127.0.0.1:3080/?token=x"}`, 1234)
	b.ResetTimer()
	for b.Loop() {
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
}
