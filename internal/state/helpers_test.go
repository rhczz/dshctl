package state

import (
	"fmt"
	"time"
)

// testDoc is the document the store's tests exercise.
//
// It is deliberately not the product's record type: this package is a
// mechanism, and its tests prove the mechanism works for the shape it is
// handed. The fields mirror the record closely enough that the assertions stay
// readable, and the rules below are the test's own — the product's validation
// and stamping live with the product.
type testDoc struct {
	PID         int    `json:"pid"`
	StartedAt   int64  `json:"startedAt"`
	Port        int    `json:"port"`
	SpawnedPID  int    `json:"spawnedPid,omitempty"`
	URL         string `json:"url,omitempty"`
	NodeVersion string `json:"nodeVersion,omitempty"`
	NodePath    string `json:"nodePath,omitempty"`
	RepoDir     string `json:"repoDir,omitempty"`
	Phase       string `json:"phase"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// testPhaseRunning is the phase the fixtures use.
const testPhaseRunning = "running"

// maxTestBytes is the bound the fixtures use.
const maxTestBytes = 64 << 10

// testStore returns the store under test with this package's rules.
func testStore(path string) Store[testDoc] {
	return Store[testDoc]{
		Path:     path,
		MaxBytes: maxTestBytes,
		Validate: func(doc testDoc) error {
			if doc.PID <= 0 {
				return fmt.Errorf("the record's pid is invalid: %d", doc.PID)
			}
			return nil
		},
		Stamp: func(doc *testDoc) { doc.UpdatedAt = time.Now().Unix() },
	}
}

// validDoc is a document every rule accepts.
func validDoc() testDoc {
	return testDoc{PID: 4242, StartedAt: 1_700_000_000, Port: 3080, Phase: testPhaseRunning}
}
