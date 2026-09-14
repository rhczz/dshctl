// Package state stores the runtime record of the Web server dshctl started.
//
// The record exists to answer one question without ever guessing: "is this pid
// still the process I started?" A bare pid cannot answer it, because an
// operating system recycles pids and every check-then-signal sequence is a race.
// The record therefore carries the process start time as a fingerprint: a pid
// whose start time differs from the recorded one describes a different process,
// and dshctl treats it as stale rather than as its own server.
//
// The rules for this file are the strict ones, and they differ from the config
// file on purpose: the record is written and read only by dshctl, so anything
// that is not a regular file of record size — a symlink, a directory, a device —
// is residue to be cleared rather than followed. A configuration file belongs to
// the operator, who may legitimately keep it behind a symlink; it is read with
// those rules instead (see internal/config).
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/rhczz/dshctl/internal/atomically"
)

// Phase names what dshctl was doing when it wrote the record.
type Phase string

const (
	// PhaseRunning means the server answered on its port and is expected there.
	//
	// A record exists only for a server that reached that point: it is written
	// after the port answers, so there is no separate "starting" phase to record.
	PhaseRunning Phase = "running"
)

// ErrCorrupt reports a record that exists but cannot be understood. Callers
// treat it as "there is nothing usable here", never as "the server is gone".
var ErrCorrupt = errors.New("运行记录无法解析")

// maxRecordBytes bounds the record file. A record is a handful of fields; a file
// larger than this is not one, and reading it whole would be a way to make a
// reporting command allocate without limit.
const maxRecordBytes = 64 << 10

// Record is the runtime state of the server dshctl started.
type Record struct {
	// PID is the process holding the port: the server itself, not the wrapper
	// dshctl launched. pnpm runs package scripts in a child process, so the pid
	// dshctl spawns is usually not the one that listens, and the listener is what
	// ownership is about.
	PID int `json:"pid"`
	// StartedAt is that process's start time in Unix seconds. It is the
	// fingerprint that makes PID reuse detectable.
	StartedAt int64 `json:"startedAt"`
	// Port is the loopback port the server was asked to bind.
	Port int `json:"port"`
	// URL is the token-carrying address the server announced, when known.
	URL string `json:"url,omitempty"`
	// SpawnedPID is the process dshctl started, which is the leader of the
	// process group the server lives in. It is kept so the whole group can be
	// ended as a unit; it is not what ownership is decided from.
	SpawnedPID int `json:"spawnedPid,omitempty"`
	// NodeVersion is the Node release the server was started with, when known.
	// It is recorded because --node can differ from the settings document, so
	// the record is the only place that says what this instance runs.
	NodeVersion string `json:"nodeVersion,omitempty"`
	// NodePath is the node binary the server was started with, when known.
	NodePath string `json:"nodePath,omitempty"`
	// Phase is what dshctl observed the last time it wrote the record.
	Phase Phase `json:"phase"`
	// UpdatedAt is when the record was last written, in Unix seconds.
	UpdatedAt int64 `json:"updatedAt"`
}

// Store reads and writes the record inside the state directory.
type Store struct {
	// Path is the record file location.
	Path string
}

// Load reads the record.
//
// Returns:
//   - the record, and true when one was read successfully.
//   - false and a nil error when no record exists.
//   - ErrCorrupt (wrapped) when the file exists but cannot be decoded.
func (s Store) Load() (Record, bool, error) {
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Record{}, false, nil
	case err != nil:
		return Record{}, false, fmt.Errorf("无法读取运行记录 %s: %w", s.Path, err)
	case !info.Mode().IsRegular():
		// A directory or device at the record path is residue, not a record. It
		// is reported as corrupt so the caller can clear it; reading it would
		// either fail forever or follow something outside the state directory.
		return Record{}, false, fmt.Errorf("%w: %s 不是普通文件", ErrCorrupt, s.Path)
	case info.Size() > maxRecordBytes:
		return Record{}, false, fmt.Errorf("%w: %s 过大 (%d 字节)", ErrCorrupt, s.Path, info.Size())
	}

	data, err := readRecordFile(s.Path)
	if err != nil {
		return Record{}, false, fmt.Errorf("无法读取运行记录 %s: %w", s.Path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Record{}, false, fmt.Errorf("%w: %s 内容为空", ErrCorrupt, s.Path)
	}

	// Unknown fields are accepted on purpose. The record is written by one build
	// of dshctl and read by another — after an upgrade, a downgrade, or when two
	// installations share a home directory — so a field this build does not know
	// is normal, not corruption. Refusing it made `stop` delete the only pointer
	// to a running server and report success while the server kept serving.
	var record Record
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&record); err != nil {
		return Record{}, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
	}
	// Exactly one document: trailing content means the file is not the record
	// this build wrote.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Record{}, false, fmt.Errorf("%w: %s 在记录之后还有内容", ErrCorrupt, s.Path)
	}
	if record.PID <= 0 {
		return Record{}, false, fmt.Errorf("%w: %s 记录的 pid 无效: %d", ErrCorrupt, s.Path, record.PID)
	}
	return record, true, nil
}

// recordReadRetryWindow bounds how long a read keeps trying while the operating
// system reports that the file is being replaced.
const recordReadRetryWindow = 250 * time.Millisecond

// recordReadRetryDelay is the pause between two attempts.
const recordReadRetryDelay = 10 * time.Millisecond

// readRecordFile reads the record, retrying briefly while the platform says the
// file is being replaced.
//
// A record is written by replacing it, and Windows refuses a read of a file that
// another handle is replacing ("being used by another process"). A `status` that
// ran at that instant would report that it could not read a record that is
// perfectly readable a millisecond later, which turns a routine race into a
// failure. Only that answer is retried: a missing file, a corrupt document or a
// permission problem is reported as it is, on the first attempt.
func readRecordFile(path string) ([]byte, error) {
	deadline := time.Now().Add(recordReadRetryWindow)
	for {
		data, err := os.ReadFile(path)
		if err == nil || !recordBeingReplaced(err) || time.Now().After(deadline) {
			return data, err
		}
		time.Sleep(recordReadRetryDelay)
	}
}

// Save writes the record atomically, stamping UpdatedAt.
func (s Store) Save(record Record) error {
	record.UpdatedAt = time.Now().Unix()
	if record.PID <= 0 {
		return fmt.Errorf("拒绝写入 pid 无效的运行记录: %d", record.PID)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("无法序列化运行记录: %w", err)
	}
	return atomically.WriteFile(s.Path, append(data, '\n'), 0o600)
}

// Remove deletes the record; a missing file is not an error.
//
// A directory or other non-regular residue at the path is removed as well: the
// state directory is disposable by design, and such residue would otherwise make
// every later read fail the same way forever.
func (s Store) Remove() error {
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("无法检查运行记录 %s: %w", s.Path, err)
	case info.IsDir():
		if err := os.RemoveAll(s.Path); err != nil {
			return fmt.Errorf("无法清理运行记录路径上的残留 %s: %w", s.Path, err)
		}
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("无法删除运行记录 %s: %w", s.Path, err)
	}
	return nil
}

// Match reports whether the record describes the process that is running now.
//
// A record matches when the pid is the same and the start times agree. The
// comparison is deliberately one-sided: an unknown start time (the platform
// could not report it) still matches, because refusing to act then would leave
// the operator unable to stop a server that is plainly running. A *known*
// mismatch never matches.
//
// Parameters:
//   - record: stored record.
//   - startedAt: start time of the pid that exists now, in Unix seconds, or 0
//     when the platform could not determine it.
//   - tolerance: how far the two values may differ. A negative tolerance is read
//     as no tolerance at all — the exact fingerprint still matches — because the
//     only alternative reading makes a running server look like a stranger.
func Match(record Record, startedAt int64, tolerance time.Duration) bool {
	if startedAt <= 0 || record.StartedAt <= 0 {
		return true
	}
	// A negative tolerance is nonsense as a budget, but it must not become "no
	// record ever matches": that would make dshctl treat its own running server
	// as a foreign process, report it as recycled, and refuse to end it. Zero is
	// the strictest reading that still means "the same start time".
	if tolerance < 0 {
		tolerance = 0
	}
	delta := record.StartedAt - startedAt
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(tolerance/time.Second)
}

// Describe renders the record in one line for diagnostics.
func (r Record) Describe() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "pid=%d, started=%s, port=%d, phase=%s",
		r.PID, time.Unix(r.StartedAt, 0).Format(time.RFC3339), r.Port, r.Phase)
	if r.URL != "" {
		builder.WriteString(", url=")
		builder.WriteString(r.URL)
	}
	if r.NodeVersion != "" {
		builder.WriteString(", node=")
		builder.WriteString(r.NodeVersion)
	}
	return builder.String()
}
