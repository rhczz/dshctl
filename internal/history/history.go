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
var ErrCorrupt = errors.New("更新历史无法解析")

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
		return File{}, false, fmt.Errorf("无法读取更新历史 %s: %w", s.Path, err)
	case !info.Mode().IsRegular():
		// A directory or device at the history path is residue, not a history.
		// Reading it would either fail forever or follow something outside the
		// state directory.
		return File{}, false, fmt.Errorf("%w: %s 不是普通文件", ErrCorrupt, s.Path)
	case info.Size() > maxFileBytes:
		return File{}, false, fmt.Errorf("%w: %s 过大 (%d 字节)", ErrCorrupt, s.Path, info.Size())
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return File{}, false, fmt.Errorf("无法读取更新历史 %s: %w", s.Path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return File{}, false, fmt.Errorf("%w: %s 内容为空", ErrCorrupt, s.Path)
	}
	// Unknown fields are accepted on purpose. The file is written by one build
	// of dshctl and read by another — after an upgrade or a downgrade — so a
	// field this build does not know is normal, not corruption.
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
	}
	for _, group := range file.Repos {
		if group.Repo == "" {
			return File{}, false, fmt.Errorf("%w: %s 有一组没有仓库路径", ErrCorrupt, s.Path)
		}
		for _, record := range group.Records {
			if record.Commit == "" {
				return File{}, false, fmt.Errorf("%w: %s 中 %s 有一条没有 commit 的位置",
					ErrCorrupt, s.Path, group.Repo)
			}
		}
	}
	return file, true, nil
}

// Save writes the history atomically with owner-only permissions.
func (s Store) Save(file File) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("无法序列化更新历史: %w", err)
	}
	return atomically.WriteFile(s.Path, append(data, '\n'), 0o600)
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
// is capped at MaxRecords.
func Visit(records []Record, position Record) []Record {
	for index, existing := range records {
		if existing.Commit == position.Commit {
			records = records[index+1:]
			break
		}
	}
	records = append([]Record{position}, records...)
	if len(records) > MaxRecords {
		records = records[:MaxRecords]
	}
	return records
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
