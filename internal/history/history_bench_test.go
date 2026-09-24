package history

import (
	"fmt"
	"testing"
)

// BenchmarkVisit simulates the lifetime worst case: a checkout updated daily
// for years. Every deployment walks the whole stack once; a bound of 50 keeps
// that constant, but a regression that lets the stack grow would turn every
// update into an O(n) scan over an ever-longer file, and years of history would
// show it.
func BenchmarkVisitAtTheBound(b *testing.B) {
	var records []Record
	for index := 0; index < maxRecordsDefault; index++ {
		records = Visit(records, Record{Commit: fmt.Sprintf("%07d", index), At: int64(index)}, maxRecordsDefault)
	}
	position := Record{Commit: "newest", At: 10_000}

	b.ResetTimer()
	for b.Loop() {
		records = Visit(records, position, maxRecordsDefault)
	}
}

// BenchmarkStep is what every rollback pays: walking the virtual stack to find
// the n-th position. It is linear in the bound, which stays small — this is a
// guard against it becoming quadratic or unbounded, not a hot-path tuning tool.
func BenchmarkStep(b *testing.B) {
	var records []Record
	for index := 0; index < maxRecordsDefault; index++ {
		records = Visit(records, Record{Commit: fmt.Sprintf("%07d", index), At: int64(index)}, maxRecordsDefault)
	}
	current := Record{Commit: "newest", At: 10_000}

	b.ResetTimer()
	for b.Loop() {
		if _, ok := Step(records, current, 7, maxRecordsDefault); !ok {
			b.Fatal("step 7 was refused")
		}
	}
}
