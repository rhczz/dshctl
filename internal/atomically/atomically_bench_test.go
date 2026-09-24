package atomically

import (
	"path/filepath"
	"testing"
)

// BenchmarkWriteFileAndReplace pins the cost of one durable state write. The
// temporary file, its permissions, its sync, the rename and the directory sync
// are the whole point of this package: a write that grows cheaper by dropping
// one of them — the durability a record exists for — shows up here before it
// ships.
func BenchmarkWriteFileAndReplace(b *testing.B) {
	path := filepath.Join(b.TempDir(), "dshctl.state.json")
	payload := []byte(`{"pid":1234,"port":3080,"startedAt":1700000000}`)

	b.ResetTimer()
	for b.Loop() {
		if err := WriteFile(path, payload, 0o600); err != nil {
			b.Fatalf("WriteFile: %v", err)
		}
	}
}
