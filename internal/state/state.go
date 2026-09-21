// Package state stores one small JSON document under strict rules.
//
// It is a mechanism, not a record format. What the document means — the fields,
// the identity a pid and a start time describe, the file name it lives under —
// belongs to the layer that owns that contract; this package knows how to read
// and write it so that a reader never sees a half-written document and a
// document nobody can understand is reported rather than guessed at.
//
// The rules are the strict ones, and they differ from the config file on
// purpose: these documents are written and read only by the program, so anything
// that is not a regular file of the bounded size — a symlink, a directory, a
// device — is residue to be cleared rather than followed. A configuration file
// belongs to the operator, who may legitimately keep it behind a symlink; it is
// read with those rules instead (see internal/config).
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/rhczz/dshctl/internal/atomically"
)

// ErrCorrupt reports a document that exists but cannot be understood. Callers
// treat it as "there is nothing usable here", never as "there is nothing here".
var ErrCorrupt = errors.New("文档无法解析")

// documentPermission is the mode a stored document gets.
//
// The store exists for state the program keeps to itself — a record of what it
// started, where it moved a tree — so the documents are private by
// construction rather than by asking every caller to remember.
const documentPermission = 0o600

// Store reads and writes one JSON document of type T.
//
// T is the caller's: the layer that owns the document's meaning also owns its
// shape, its bounds and its rules. The store owns only the mechanism — atomic
// replacement, the size bound, the strict read — so the same code stores a
// runtime record, a position stack, or a document that does not exist yet.
type Store[T any] struct {
	// Path is the document's location.
	Path string
	// MaxBytes bounds the document. A larger file is not one of these
	// documents, and reading it whole would be a way to make a reporting command
	// allocate without limit.
	MaxBytes int64
	// Validate applies the caller's rule for a usable document, or nil when any
	// decoded value is usable. It runs on read and before a write, so a document
	// that could not be written is also not read back as one.
	Validate func(T) error
	// Stamp, when set, is applied to the value before it is written: a document
	// that carries its own timestamp leaves the clock to the caller.
	Stamp func(*T)
}

// Load reads the document.
//
// Returns:
//   - the value, and true when one was read successfully.
//   - the zero value and false, with a nil error, when no document exists.
//   - ErrCorrupt (wrapped) when the file exists but cannot be understood.
func (s Store[T]) Load() (T, bool, error) {
	var zero T
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("无法读取文档 %s: %w", s.Path, err)
	case !info.Mode().IsRegular():
		// A directory or device at the path is residue, not a document. It is
		// reported as corrupt so the caller can clear it; reading it would
		// either fail forever or follow something outside the state directory.
		return zero, false, fmt.Errorf("%w: %s 不是普通文件", ErrCorrupt, s.Path)
	case info.Size() > s.MaxBytes:
		return zero, false, fmt.Errorf("%w: %s 过大 (%d 字节)", ErrCorrupt, s.Path, info.Size())
	}

	data, err := readDocumentFile(s.Path)
	if err != nil {
		return zero, false, fmt.Errorf("无法读取文档 %s: %w", s.Path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return zero, false, fmt.Errorf("%w: %s 内容为空", ErrCorrupt, s.Path)
	}

	// Unknown fields are accepted on purpose. A document is written by one build
	// and read by another — after an upgrade, a downgrade, or when two
	// installations share a home directory — so a field this build does not know
	// is normal, not corruption. Refusing it made `stop` delete the only pointer
	// to a running server and report success while the server kept serving.
	var value T
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&value); err != nil {
		return zero, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
	}
	// Exactly one document: trailing content means the file is not one of these.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return zero, false, fmt.Errorf("%w: %s 在文档之后还有内容", ErrCorrupt, s.Path)
	}
	if s.Validate != nil {
		if err := s.Validate(value); err != nil {
			return zero, false, fmt.Errorf("%w: %s: %v", ErrCorrupt, s.Path, err)
		}
	}
	return value, true, nil
}

// documentReadRetryWindow bounds how long a read keeps trying while the
// operating system reports that the file is being replaced.
const documentReadRetryWindow = 250 * time.Millisecond

// documentReadRetryDelay is the pause between two attempts.
const documentReadRetryDelay = 10 * time.Millisecond

// readDocumentFile reads the document, retrying briefly while the platform says
// the file is being replaced.
//
// A document is written by replacing it, and Windows refuses a read of a file
// that another handle is replacing ("being used by another process"). A report
// that ran at that instant would say it could not read a document that is
// perfectly readable a millisecond later, which turns a routine race into a
// failure. Only that answer is retried: a missing file, a corrupt document or a
// permission problem is reported as it is, on the first attempt.
func readDocumentFile(path string) ([]byte, error) {
	deadline := time.Now().Add(documentReadRetryWindow)
	for {
		data, err := os.ReadFile(path)
		if err == nil || !documentBeingReplaced(err) || time.Now().After(deadline) {
			return data, err
		}
		time.Sleep(documentReadRetryDelay)
	}
}

// Save writes the document atomically.
func (s Store[T]) Save(value T) error {
	if s.Stamp != nil {
		s.Stamp(&value)
	}
	if s.Validate != nil {
		if err := s.Validate(value); err != nil {
			return fmt.Errorf("拒绝写入无效的文档: %w", err)
		}
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("无法序列化文档: %w", err)
	}
	return atomically.WriteFile(s.Path, append(data, '\n'), documentPermission)
}

// Remove deletes the document; a missing file is not an error.
//
// A directory or other non-regular residue at the path is removed as well: the
// state directory is disposable by design, and such residue would otherwise make
// every later read fail the same way forever.
func (s Store[T]) Remove() error {
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("无法检查文档 %s: %w", s.Path, err)
	case info.IsDir():
		if err := os.RemoveAll(s.Path); err != nil {
			return fmt.Errorf("无法清理文档路径上的残留 %s: %w", s.Path, err)
		}
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("无法删除文档 %s: %w", s.Path, err)
	}
	return nil
}
