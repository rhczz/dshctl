package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveRefusesADocumentTooLargeToRead pins the writer's half of the size
// bound: Load refuses a document larger than MaxBytes, so Save must refuse to
// produce one. A caller that could write a document its own reader rejects
// would trade a loud failure at write time for a permanent one at read time —
// exactly the asymmetry the deployment history's store does not have.
func TestSaveRefusesADocumentTooLargeToRead(t *testing.T) {
	type document struct {
		Note string `json:"note"`
	}
	box := Store[document]{
		Path:     filepath.Join(t.TempDir(), "document.json"),
		MaxBytes: 128,
	}
	value := document{Note: string(make([]byte, 4096))}

	err := box.Save(value)
	if err == nil {
		t.Fatal("Save wrote a document larger than the reader's bound")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want the size bound reported", err)
	}
	if _, statErr := os.Lstat(box.Path); !os.IsNotExist(statErr) {
		t.Fatalf("the oversize document reached the disk: %v", statErr)
	}
}
