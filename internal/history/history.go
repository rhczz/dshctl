// Package history records where dshctl has deployed each checkout.
//
// git is the authority for where a checkout is now; this package answers the
// other question — which positions dshctl has moved this tree to, newest first.
// That stack is what rollback walks: moving to a position already in it drops
// everything newer, so repeated rollbacks keep walking backwards instead of
// bouncing between two commits.
//
// The file is private to dshctl and written by replacement, so a reader never
// sees a half-written document. A file that cannot be understood is reported as
// ErrCorrupt rather than as an empty history: "no history" and "a history
// nobody can read" must not be the same answer, because only one of them is
// safe to roll back from.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/rhczz/dshctl/internal/atomically"
	"github.com/rhczz/dshctl/internal/state"
)

// maxRecordsDefault is the bound a caller that states none gets: the oldest
// positions fall off so the file stays small enough to read during an incident,
// and an operator who needs something older can name it with `dshctl update
// <sha>`. A product with a different appetite sets Store.MaxRecords.
const maxRecordsDefault = 50

// maxFileBytes bounds the history file. A document larger than this is not one,
// and reading it whole would let a corrupt file allocate without limit.
const maxFileBytes = 64 << 10

// ErrCorrupt reports a history file that exists but cannot be understood.
// Callers treat it as "refuse to guess", never as "there is no history".
var ErrCorrupt = errors.New("the deployment history cannot be parsed")

// Record is one position dshctl deployed a checkout at.
type Record struct {
	// Commit is the full revision the checkout was moved to.
	Commit string `json:"commit"`
	// Selector is what the operator asked for at the time ("latest", a tag, a
	// sha, "-n 2"), or empty when this position is the starting point of a move
	// rather than a deployment.
	Selector string `json:"selector,omitempty"`
	// At is when the move happened, in Unix seconds.
	At int64 `json:"at"`
}

// Group is one checkout's history.
type Group struct {
	// Repo is the absolute checkout path.
	Repo string `json:"repo"`
	// Records lists the positions this checkout has been at, newest first.
	Records []Record `json:"records"`
}

// File is the whole history document, one group per checkout.
type File struct {
	// Repos holds every checkout this state directory has deployed.
	Repos []Group `json:"repos"`
}

// Store reads and writes the history file.
type Store struct {
	// Path is the file location.
	Path string
	// MaxRecords bounds one checkout's stack: the oldest positions fall off so
	// the file stays small enough to read during an incident, and an operator
	// who needs something older can still name it explicitly with
	// `dshctl update <sha>`. Zero means the built-in default; the product that
	// owns the history states a different appetite here.
	MaxRecords int
}

// maxRecords is the bound this store applies.
func (s Store) maxRecords() int {
	if s.MaxRecords > 0 {
		return s.MaxRecords
	}
	return maxRecordsDefault
}

// Load reads the history.
//
// Returns:
//   - the file, and true, when one was read successfully.
//   - false and a nil error when no file exists.
//   - ErrCorrupt (wrapped) when the file exists but cannot be understood.
func (s Store) Load() (File, bool, error) {
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return File{}, false, nil
	case err != nil:
		return File{}, false, fmt.Errorf("the deployment history %s could not be read: %w", s.Path, err)
	case !info.Mode().IsRegular():
		// A directory or device at the history path is residue, not a history.
		// Reading it would either fail forever or follow something outside the
		// state directory.
		return File{}, false, fmt.Errorf("%w: %s is not a regular file", ErrCorrupt, s.Path)
	case info.Size() > maxFileBytes:
		return File{}, false, fmt.Errorf("%w: %s is too large (%d bytes)", ErrCorrupt, s.Path, info.Size())
	}

	data, err := state.ReadDocument(s.Path)
	if err != nil {
		return File{}, false, fmt.Errorf("the deployment history %s could not be read: %w", s.Path, err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return File{}, false, fmt.Errorf("%w: %s is empty", ErrCorrupt, s.Path)
	}
	// The document is an object. A top-level null decodes into an empty value
	// without an error, which would turn a mangled file into "no history" —
	// exactly the answer that lets a rollback guess.
	if trimmed[0] != '{' {
		return File{}, false, fmt.Errorf("%w: %s is not a JSON object", ErrCorrupt, s.Path)
	}
	// Unknown fields are accepted on purpose. The file is written by one build
	// of dshctl and read by another — after an upgrade or a downgrade — so a
	// field this build does not know is normal, not corruption.
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
	}
	if err := validate(file); err != nil {
		return File{}, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
	}
	return file, true, nil
}

// validate reports the first shape that is not a history document. Load calls
// it corruption and Save refuses to write it, so the writer can never produce a
// file its own reader rejects.
func validate(file File) error {
	seen := make(map[string]struct{}, len(file.Repos))
	for _, group := range file.Repos {
		if group.Repo == "" {
			return fmt.Errorf("one group has no checkout path")
		}
		if _, duplicate := seen[group.Repo]; duplicate {
			// Two groups for one checkout would make "where can it roll back
			// to" depend on which group a caller happened to read.
			return fmt.Errorf("the checkout %s appears in two groups", group.Repo)
		}
		seen[group.Repo] = struct{}{}
		if len(group.Records) == 0 {
			return fmt.Errorf("the group for %s has no positions", group.Repo)
		}
		positions := make(map[string]struct{}, len(group.Records))
		for _, record := range group.Records {
			if record.Commit == "" {
				return fmt.Errorf("the group for %s has a position without a commit", group.Repo)
			}
			if record.At <= 0 {
				// A position without a time cannot be read back as one: the
				// view would print 1970 and the record would claim a move that
				// never happened.
				return fmt.Errorf("the position %s of %s has no timestamp", group.Repo, record.Commit)
			}
			if _, duplicate := positions[record.Commit]; duplicate {
				// Two entries for one position would make the step arithmetic
				// ambiguous, and a valid stack never repeats a commit.
				return fmt.Errorf("the position %s of %s appears twice", group.Repo, record.Commit)
			}
			positions[record.Commit] = struct{}{}
		}
	}
	return nil
}

// Save writes the history atomically with owner-only permissions.
//
// A document the reader would reject is refused rather than written: a caller
// must not be able to produce a file its own Load reports as corrupt. A
// document that has outgrown maxFileBytes is not refused outright: the bound
// exists so the file stays small enough to read during an incident, and
// checkouts an operator deleted or moved years ago would otherwise keep their
// group forever and turn every later deployment into a permanent save failure.
// The groups whose most recent position is the oldest fall off — the same rule
// one checkout's oldest positions follow — until the document fits. One group
// that alone exceeds the bound is still refused: that is not accumulation,
// that is a record that cannot be read at any size.
func (s Store) Save(file File) error {
	if err := validate(file); err != nil {
		return fmt.Errorf("refusing to write an invalid deployment history: %w", err)
	}
	payload, err := marshal(file)
	if err != nil {
		return fmt.Errorf("the deployment history could not be encoded: %w", err)
	}
	for len(payload) > maxFileBytes && len(file.Repos) > 1 {
		file = dropStalestGroup(file)
		payload, err = marshal(file)
		if err != nil {
			return fmt.Errorf("the deployment history could not be encoded: %w", err)
		}
	}
	if len(payload) > maxFileBytes {
		return fmt.Errorf("the deployment history is too large (%d bytes); refusing to write %s", len(payload), s.Path)
	}
	return atomically.WriteFile(s.Path, payload, 0o600)
}

// marshal encodes a history document the way Save writes it.
func marshal(file File) ([]byte, error) {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// dropStalestGroup returns the document without the group whose most recent
// position is the oldest — the checkout a rollback is least likely to walk. A
// tie falls to the lexicographically last path, so the choice is deterministic.
func dropStalestGroup(file File) File {
	stalest := 0
	for index := 1; index < len(file.Repos); index++ {
		candidate, kept := file.Repos[index], file.Repos[stalest]
		switch {
		case latestAt(candidate) < latestAt(kept):
			stalest = index
		case latestAt(candidate) == latestAt(kept) && candidate.Repo > kept.Repo:
			stalest = index
		}
	}
	next := File{Repos: make([]Group, 0, len(file.Repos)-1)}
	next.Repos = append(next.Repos, file.Repos[:stalest]...)
	next.Repos = append(next.Repos, file.Repos[stalest+1:]...)
	return next
}

// latestAt reports the newest time in a group, independent of record order: a
// hand-edited stack may not be sorted, and eviction must not depend on it.
func latestAt(group Group) int64 {
	latest := int64(0)
	for _, record := range group.Records {
		if record.At > latest {
			latest = record.At
		}
	}
	return latest
}

// Records returns one checkout's positions, newest first, or nil when this
// state directory has never deployed it. The slice is a copy: a caller cannot
// change what a later read of the same value sees.
func (f File) Records(repo string) []Record {
	for _, group := range f.Repos {
		if group.Repo == repo {
			return append([]Record(nil), group.Records...)
		}
	}
	return nil
}

// With returns the file with one checkout's positions replaced. An empty
// records list removes the group; groups are ordered by path so the document
// has exactly one shape for one state.
func (f File) With(repo string, records []Record) File {
	next := File{Repos: make([]Group, 0, len(f.Repos)+1)}
	for _, group := range f.Repos {
		if group.Repo == repo {
			continue
		}
		next.Repos = append(next.Repos, group)
	}
	if len(records) > 0 {
		next.Repos = append(next.Repos, Group{Repo: repo, Records: append([]Record(nil), records...)})
	}
	sort.Slice(next.Repos, func(i, j int) bool { return next.Repos[i].Repo < next.Repos[j].Repo })
	return next
}

// Visit returns the stack with position on top. A position the stack already
// holds truncates everything newer than it; a new one is prepended. The result
// never repeats a commit — a stack that (through hand editing) does is
// deduplicated — and is capped at limit, which the caller states.
func Visit(records []Record, position Record, limit int) []Record {
	index := -1
	for at, existing := range records {
		if existing.Commit == position.Commit {
			index = at
			break
		}
	}
	kept := make([]Record, 0, len(records)+1)
	kept = append(kept, position)
	seen := map[string]struct{}{position.Commit: {}}
	for at, existing := range records {
		if at <= index {
			continue
		}
		if _, duplicate := seen[existing.Commit]; duplicate {
			continue
		}
		seen[existing.Commit] = struct{}{}
		kept = append(kept, existing)
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}

// Step returns the n-th position back from current, counting current as step
// zero: n=1 is where the previous move started.
//
// The virtual stack is Visit(records, current, MaxRecords()): a current position
// the stack already holds makes everything newer than it unreachable — those
// positions are not "before" where the operator is now.
//
// Returns false when n is not a positive step or the stack is shorter than n+1
// entries.
func Step(records []Record, current Record, n, limit int) (Record, bool) {
	if n < 1 {
		return Record{}, false
	}
	virtual := Visit(records, current, limit)
	if n >= len(virtual) {
		return Record{}, false
	}
	return virtual[n], true
}
