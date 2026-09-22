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

// MaxRecords bounds one checkout's stack. The oldest positions fall off: the
// file stays small enough to read during an incident, and an operator who needs
// something older can still name it explicitly with `dshctl update <sha>`.
const MaxRecords = 50

// maxFileBytes bounds the history file. A document larger than this is not one,
// and reading it whole would let a corrupt file allocate without limit.
const maxFileBytes = 64 << 10

// ErrCorrupt reports a history file that exists but cannot be understood.
// Callers treat it as "refuse to guess", never as "there is no history".
// ErrCorrupt reports a history that exists but cannot be understood; it is a
// value with a method so its text is rendered in the reader's language while
// errors.Is still recognises it.
type corruptHistory struct{}

func (corruptHistory) Error() string { return i18nLine(MsgCorruptHistory) }
func (corruptHistory) Is(target error) bool {
	_, ok := target.(corruptHistory)
	return ok
}

var ErrCorrupt error = corruptHistory{}

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
		return File{}, false, fmt.Errorf("%s: %w", i18nLine(MsgReadFailed, s.Path), err)
	case !info.Mode().IsRegular():
		// A directory or device at the history path is residue, not a history.
		// Reading it would either fail forever or follow something outside the
		// state directory.
		return File{}, false, fmt.Errorf("%w: %s", ErrCorrupt, i18nLine(MsgNotRegularFile, s.Path))
	case info.Size() > maxFileBytes:
		return File{}, false, fmt.Errorf("%w: %s", ErrCorrupt, i18nLine(MsgTooLarge, s.Path, info.Size()))
	}

	data, err := state.ReadDocument(s.Path)
	if err != nil {
		return File{}, false, fmt.Errorf("%s: %w", i18nLine(MsgReadFailed, s.Path), err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return File{}, false, fmt.Errorf("%w: %s", ErrCorrupt, i18nLine(MsgEmpty, s.Path))
	}
	// The document is an object. A top-level null decodes into an empty value
	// without an error, which would turn a mangled file into "no history" —
	// exactly the answer that lets a rollback guess.
	if trimmed[0] != '{' {
		return File{}, false, fmt.Errorf("%w: %s", ErrCorrupt, i18nLine(MsgNotJSONObject, s.Path))
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
			return fmt.Errorf("%s", i18nLine(MsgGroupNoRepo))
		}
		if _, duplicate := seen[group.Repo]; duplicate {
			// Two groups for one checkout would make "where can it roll back
			// to" depend on which group a caller happened to read.
			return fmt.Errorf("%s", i18nLine(MsgGroupRepeated, group.Repo))
		}
		seen[group.Repo] = struct{}{}
		if len(group.Records) == 0 {
			return fmt.Errorf("%s", i18nLine(MsgGroupEmpty, group.Repo))
		}
		positions := make(map[string]struct{}, len(group.Records))
		for _, record := range group.Records {
			if record.Commit == "" {
				return fmt.Errorf("%s", i18nLine(MsgRecordNoCommit, group.Repo))
			}
			if record.At <= 0 {
				// A position without a time cannot be read back as one: the
				// view would print 1970 and the record would claim a move that
				// never happened.
				return fmt.Errorf("%s", i18nLine(MsgRecordNoTime, group.Repo, record.Commit))
			}
			if _, duplicate := positions[record.Commit]; duplicate {
				// Two entries for one position would make the step arithmetic
				// ambiguous, and a valid stack never repeats a commit.
				return fmt.Errorf("%s", i18nLine(MsgRecordRepeated, group.Repo, record.Commit))
			}
			positions[record.Commit] = struct{}{}
		}
	}
	return nil
}

// Save writes the history atomically with owner-only permissions.
//
// A document the reader would reject is refused rather than written: a caller
// must not be able to produce a file its own Load reports as corrupt, whether
// because it is too large or because a position is malformed.
func (s Store) Save(file File) error {
	if err := validate(file); err != nil {
		return fmt.Errorf("%s: %w", i18nLine(MsgRefuseInvalid), err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: %w", i18nLine(MsgEncodeFailed), err)
	}
	payload := append(data, '\n')
	if len(payload) > maxFileBytes {
		return fmt.Errorf("%s", i18nLine(MsgWriteTooLarge, len(payload), s.Path))
	}
	return atomically.WriteFile(s.Path, payload, 0o600)
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
// deduplicated — and is capped at MaxRecords.
func Visit(records []Record, position Record) []Record {
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
	if len(kept) > MaxRecords {
		kept = kept[:MaxRecords]
	}
	return kept
}

// Step returns the n-th position back from current, counting current as step
// zero: n=1 is where the previous move started.
//
// The virtual stack is Visit(records, current), so a current position the stack
// already holds makes everything newer than it unreachable — those positions
// are not "before" where the operator is now.
//
// Returns false when n is not a positive step or the stack is shorter than n+1
// entries.
func Step(records []Record, current Record, n int) (Record, bool) {
	if n < 1 {
		return Record{}, false
	}
	virtual := Visit(records, current)
	if n >= len(virtual) {
		return Record{}, false
	}
	return virtual[n], true
}
