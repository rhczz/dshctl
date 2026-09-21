package service

import (
	"fmt"
	"time"

	"github.com/rhczz/dshctl/internal/domain"
	"github.com/rhczz/dshctl/internal/state"
)

// This file is the product side of the runtime record.
//
// The store in internal/state is a mechanism: a bounded, strict, atomically
// replaced JSON document. What the document *is* — a record of one instance,
// with a pid and a start time that identify the process — belongs to this
// layer, which is also where the bound, the validation and the timestamp live.
// A second product would use the same store with its own document.

// maxRecordBytes bounds the record file. A record is a handful of fields; a file
// larger than this is not one, and reading it whole would be a way to make a
// reporting command allocate without limit.
const maxRecordBytes = 64 << 10

// recordStore returns the store for one instance's runtime record.
func recordStore(path string) state.Store[domain.Record] {
	return state.Store[domain.Record]{
		Path:     path,
		MaxBytes: maxRecordBytes,
		Validate: validateRecord,
		Stamp:    stampRecord,
	}
}

// validateRecord rejects a record that cannot identify a process.
//
// A record without a pid describes nothing: it is not a stale record of a
// server that ended, it is a document that was never one of ours. Refusing it is
// what keeps "no record" and "a record nobody can use" apart.
func validateRecord(record domain.Record) error {
	if record.PID <= 0 {
		return fmt.Errorf("记录的 pid 无效: %d", record.PID)
	}
	return nil
}

// stampRecord writes the moment the record was last touched.
func stampRecord(record *domain.Record) {
	record.UpdatedAt = time.Now().Unix()
}
