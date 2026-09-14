// Package logfile owns dshctl's single log file: section markers, size
// rotation, tailing, following across replacement, and section extraction.
//
// Server output, build output and update output share one file so that
// `dshctl logs` explains both what the server is doing and why the last build
// failed.
//
// Rotation copies the file to <path>.old and then truncates the *same* inode.
// It never renames, because the detached server holds an append handle on that
// inode for its whole life: a rename would send every later line to the backup
// file, and the next rotation would delete it along with them.
package logfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// sectionPrefix opens and closes every section marker line.
const sectionPrefix = "====="

// defaultPoll is how often Follow looks for new content.
const defaultPoll = 250 * time.Millisecond

// copyBufferBytes is the chunk size used while rotating.
const copyBufferBytes = 256 << 10

// tailChunkBytes is the chunk size used when reading backwards from the end.
const tailChunkBytes = 64 << 10

// followState unknown keeps the marker-parsing honest: a line that is not a
// marker must never end the section in progress.
var sectionPattern = regexp.MustCompile(
	`^` + sectionPrefix + ` (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) dshctl (\S+) ` + sectionPrefix + `$`)

// Logger appends dshctl's records to one file.
type Logger struct {
	// Path is the log file location.
	Path string
	// RotateBytes is the size at which the file rolls to <Path>.old;
	// 0 disables rotation.
	RotateBytes int64
	// Now supplies the section timestamp; nil uses time.Now.
	Now func() time.Time
	// poll is how often Follow re-checks the file; 0 uses defaultPoll.
	poll time.Duration
}

// New returns a logger for path.
func New(path string, rotateBytes int64) *Logger {
	return &Logger{Path: path, RotateBytes: rotateBytes, Now: time.Now, poll: defaultPoll}
}

// SetPollInterval overrides the follow interval; it exists for tests.
func (l *Logger) SetPollInterval(interval time.Duration) {
	if interval > 0 {
		l.poll = interval
	}
}

// BackupPath is where the previous generation of the log is kept.
func (l *Logger) BackupPath() string { return l.Path + ".old" }

// Section appends a marker line that opens a new section.
//
// A leading newline is always written first, so a marker can never be glued
// onto a line a child process left unterminated.
//
// Parameters:
//   - title: short command name such as "build"; it must not contain spaces.
//
// Returns:
//   - an error when the title is invalid or the record cannot be written.
func (l *Logger) Section(title string) error {
	if err := ValidateTitle(title); err != nil {
		return err
	}
	stamp := l.timestamp()
	return l.append(fmt.Sprintf("\n%s %s dshctl %s %s\n", sectionPrefix, stamp, title, sectionPrefix))
}

// Line appends one line of text, starting a new line when the file does not
// already end at one.
func (l *Logger) Line(text string) error {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if !l.endsWithNewline() {
		text = "\n" + text
	}
	return l.append(text)
}

// RotateIfNeeded copies the log to <Path>.old and truncates the live file when
// it exceeds RotateBytes.
//
// The copy is repeated until the size stops changing, so a line the server
// appends while the copy runs is carried into the backup too. The truncate
// itself cannot be atomic with a concurrent append: a line written in the
// instant between the final size check and the truncate belongs to neither
// file. That window is microseconds; the alternative — renaming — breaks the
// running server's append handle and loses every line written after the
// rotation instead of one.
//
// Returns:
//   - true when the file was rotated.
//   - an error when the size cannot be read, the copy fails, or the truncate
//     fails. A failed rotation leaves the live file intact.
func (l *Logger) RotateIfNeeded() (bool, error) {
	if l.RotateBytes <= 0 {
		return false, nil
	}
	info, err := os.Stat(l.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("无法读取日志大小 %s: %w", l.Path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("日志路径 %s 不是普通文件，请检查 DSH_LOG_FILE 配置", l.Path)
	}
	if info.Size() <= l.RotateBytes {
		return false, nil
	}
	if err := l.copyToBackupStable(); err != nil {
		return false, err
	}
	if err := os.Truncate(l.Path, 0); err != nil {
		return false, fmt.Errorf("日志轮转失败，无法清空 %s: %w", l.Path, err)
	}
	return true, nil
}

// copyToBackupStable copies the live file to the backup and repeats as long as
// it keeps growing, so everything written while the copy ran is preserved in
// the backup rather than falling between the copy and the truncate.
func (l *Logger) copyToBackupStable() error {
	for {
		info, err := os.Stat(l.Path)
		if err != nil {
			return fmt.Errorf("日志轮转失败，无法读取 %s: %w", l.Path, err)
		}
		if err := l.copyToBackup(info.Size()); err != nil {
			return err
		}
		again, err := os.Stat(l.Path)
		if err != nil {
			return fmt.Errorf("日志轮转失败，无法读取 %s: %w", l.Path, err)
		}
		if again.Size() == info.Size() {
			return nil
		}
	}
}

// copyToBackup replaces the backup with the current contents of the live file.
//
// A short copy is an error and leaves the live file untouched, so a full disk
// costs log history rather than the log itself.
func (l *Logger) copyToBackup(size int64) error {
	source, err := os.Open(l.Path)
	if err != nil {
		return fmt.Errorf("日志轮转失败，无法读取 %s: %w", l.Path, err)
	}
	defer source.Close()

	backup, err := os.OpenFile(l.BackupPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("日志轮转失败，无法写入 %s: %w", l.BackupPath(), err)
	}
	written, copyErr := io.CopyBuffer(backup, io.LimitReader(source, size), make([]byte, copyBufferBytes))
	syncErr := backup.Sync()
	closeErr := backup.Close()
	if copyErr != nil {
		return fmt.Errorf("日志轮转失败，复制到 %s 时出错: %w", l.BackupPath(), copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("日志轮转失败，同步 %s 时出错: %w", l.BackupPath(), syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("日志轮转失败，关闭 %s 时出错: %w", l.BackupPath(), closeErr)
	}
	if written != size {
		return fmt.Errorf("日志轮转失败，%s 只写入了 %d/%d 字节", l.BackupPath(), written, size)
	}
	return nil
}

// OpenAppend opens the log for appending, rotating first when needed.
//
// The caller owns the handle. When the handle is handed to a detached child the
// child keeps writing through it for its whole life, which is exactly why
// rotation truncates in place instead of renaming.
func (l *Logger) OpenAppend() (*os.File, error) {
	if _, err := l.RotateIfNeeded(); err != nil {
		return nil, err
	}
	return l.open()
}

// Size reports the current log size.
func (l *Logger) Size() (int64, error) {
	info, err := os.Stat(l.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("无法读取日志大小 %s: %w", l.Path, err)
	}
	return info.Size(), nil
}

// Exists reports whether the log file is present.
func (l *Logger) Exists() bool {
	info, err := os.Stat(l.Path)
	return err == nil && info.Mode().IsRegular()
}

// timestamp renders the section stamp.
func (l *Logger) timestamp() string {
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	return now().Format("2006-01-02 15:04:05")
}

// open opens the log for appending, creating its directory when needed.
func (l *Logger) open() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return nil, fmt.Errorf("无法创建日志目录 %s: %w", filepath.Dir(l.Path), err)
	}
	file, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("无法打开日志 %s: %w", l.Path, err)
	}
	return file, nil
}

// append writes text at the end of the log.
func (l *Logger) append(text string) error {
	file, err := l.open()
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return fmt.Errorf("无法写入日志 %s: %w", l.Path, err)
	}
	return nil
}

// endsWithNewline reports whether the live log currently ends at a line
// boundary. A missing or empty file counts as a boundary.
func (l *Logger) endsWithNewline() bool {
	info, err := os.Stat(l.Path)
	if err != nil || info.Size() == 0 {
		return true
	}
	file, err := os.Open(l.Path)
	if err != nil {
		return true
	}
	defer file.Close()
	buffer := make([]byte, 1)
	if _, err := file.ReadAt(buffer, info.Size()-1); err != nil {
		return true
	}
	return buffer[0] == '\n'
}

// ValidateTitle rejects a section title that the marker format cannot carry.
//
// The title is a single token between the marker's fixed fields and the reader
// matches it with \S+, so any whitespace at all produces a marker ParseSection
// does not recognize — which turns a build record into ordinary log text. Every
// Unicode space is therefore refused, not only the three ASCII ones a caller
// thinks of: a form feed or a no-break space is just as invisible and just as
// fatal to the round trip.
func ValidateTitle(title string) error {
	if title == "" {
		return errors.New("日志段落名不能为空")
	}
	if index := strings.IndexFunc(title, unicode.IsSpace); index >= 0 {
		return fmt.Errorf("日志段落名不能包含空白字符: %q", title)
	}
	return nil
}

// ParseSection reports the command name of a section marker line.
//
// The timestamp is validated as well as the shape, so an ordinary log line that
// happens to contain five tokens can never be mistaken for a section boundary.
//
// Returns:
//   - the section name.
//   - true when the line is a marker this package wrote.
func ParseSection(line string) (string, bool) {
	match := sectionPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return "", false
	}
	return match[2], true
}

// Tail writes the last lines of path to w.
//
// Parameters:
//   - path: log file to read.
//   - lines: number of trailing lines; values below 1 print nothing.
//   - w: destination.
//
// Returns:
//   - the number of lines written.
//   - an error when the file cannot be read.
func Tail(path string, lines int, w io.Writer) (int, error) {
	written, _, err := TailFrom(path, lines, w)
	return written, err
}

// TailFrom writes the last lines of path and reports where it stopped.
//
// The position is what makes `logs --follow` gapless: the follow starts reading
// exactly where the tail finished, so a line written while the tail was being
// printed is still delivered instead of falling between two separate passes.
//
// A missing file is not an error here: there is nothing to print, and the
// follow that takes the returned position reads whatever appears later. A
// caller that needs "the log is not there" to be an error checks it itself
// (dshctl logs does).
//
// Returns:
//   - the number of lines written.
//   - the file offset after the last line written, or 0 when nothing was read.
//   - an error when the file cannot be read.
func TailFrom(path string, lines int, w io.Writer) (int, int64, error) {
	if lines < 1 {
		return 0, 0, nil
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		return 0, 0, nil
	} else if statErr != nil {
		return 0, 0, fmt.Errorf("无法读取 %s 的大小: %w", path, statErr)
	}
	data, readSize, err := readTailLines(path, lines, maxTailBytes)
	if err != nil {
		return 0, 0, err
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		// Nothing to print; the follow starts at the end of the file so it does
		// not repeat content the tail chose not to show.
		return 0, readSize, nil
	}
	all := strings.Split(text, "\n")
	for _, line := range all {
		if _, err := fmt.Fprintln(w, strings.TrimSuffix(line, "\r")); err != nil {
			return 0, 0, err
		}
	}
	return len(all), readSize, nil
}

// maxTailBytes bounds how much a Tail reads when the requested line count is
// large, so a runaway log cannot exhaust memory.
const maxTailBytes = 8 << 20

// readTailLines reads the last count lines of a file, reading backwards in
// chunks so that a short tail of a huge file stays cheap. The returned size is
// the file size the read itself observed, which is what the caller reports as
// the position the follow must continue from: a line appended while the tail
// read belongs to the follow, and a line appended before it must not be
// re-printed.
//
// The window is cut at a line boundary, so what comes back is whole lines: the
// last count of them, or fewer when the file does not hold that many. Only when
// the byte window runs out before the start of the tail is reached does the
// first line of the window become a fragment, and then it is dropped, because
// half a line is indistinguishable from a whole one.
func readTailLines(path string, count int, limit int64) ([]byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("无法读取 %s 的大小: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		// A directory or a device where the log belongs is a mistake, not an
		// empty log: on Unix the read below fails by itself, while on Windows a
		// directory is opened happily and answers with no bytes at all.
		return nil, 0, fmt.Errorf("日志路径 %s 不是普通文件，无法读取", path)
	}
	size := info.Size()
	if size == 0 {
		return nil, 0, nil
	}

	position := size
	newlines := 0
	chunks := make([][]byte, 0, 8)
	for position > 0 {
		readSize := int64(tailChunkBytes)
		if position < readSize {
			readSize = position
		}
		position -= readSize
		chunk := make([]byte, readSize)
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, fmt.Errorf("无法读取 %s: %w", path, err)
		}
		newlines += bytes.Count(chunk, []byte{'\n'})
		chunks = append(chunks, chunk)
		// One newline more than the requested count is enough to place the cut
		// inside the window whatever the last line looks like: the newline that
		// terminates a file ending at a line boundary is not a separator.
		if newlines > count {
			break
		}
		if size-position >= limit {
			break
		}
	}

	var buffer bytes.Buffer
	for index := len(chunks) - 1; index >= 0; index-- {
		buffer.Write(chunks[index])
	}
	data := buffer.Bytes()

	cut, whole := tailCut(data, count)
	switch {
	case whole:
		data = data[cut:]
	case position > 0:
		// The window began mid-file and does not even hold the requested number
		// of lines, so its first line is a fragment whose start is outside the
		// window.
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			return nil, size, nil
		}
		data = data[index+1:]
	}
	return data, size, nil
}

// tailCut reports where the last count lines of data begin.
//
// It walks backwards over the newlines that separate one line from the next, so
// the answer is a line boundary whenever data holds that many lines. whole is
// false when data holds fewer than count lines, in which case cut is zero and
// the caller has to decide what the window's first line is worth.
func tailCut(data []byte, count int) (cut int, whole bool) {
	end := len(data)
	if end > 0 && data[end-1] == '\n' {
		// The last line is terminated, so the newline that ends it is not one of
		// the separators being counted.
		end--
	}
	for kept := 0; kept < count; kept++ {
		index := bytes.LastIndexByte(data[:end], '\n')
		if index < 0 {
			return 0, false
		}
		end = index
	}
	return end + 1, true
}

// maxScanBytes bounds how much of the log section extraction will walk. A build
// that produces more than this cannot have its section recovered, and the
// caller is told so rather than being told the record does not exist.
const maxScanBytes = 32 << 20

// Section extraction outcome flags, reported so callers can distinguish "there
// is no such record" from "the record is outside what was read".
const (
	// Found reports that a matching section was returned.
	Found = iota
	// NotFound reports that the log holds no matching section.
	NotFound
	// Truncated reports that the log is larger than the scan window and no
	// matching section was inside it.
	Truncated
)

// LastSection returns the body of the last section whose name is in titles.
//
// Parameters:
//   - path: log file to read.
//   - titles: section names to accept, for example build and update.
//
// Returns:
//   - the section body without the marker line.
//   - the outcome: Found, NotFound, or Truncated.
//   - an error when the file cannot be read.
func LastSection(path string, titles []string) ([]string, int, error) {
	wanted := make(map[string]struct{}, len(titles))
	for _, title := range titles {
		wanted[title] = struct{}{}
	}
	data, truncated, err := readTailBytes(path, maxScanBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, NotFound, nil
	}
	if err != nil {
		return nil, NotFound, err
	}

	var (
		body    []string
		current []string
		active  bool
	)
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if kind, ok := ParseSection(line); ok {
			if active {
				body = current
				found = true
			}
			current = nil
			_, active = wanted[kind]
			continue
		}
		if active {
			current = append(current, line)
		}
	}
	if active {
		body = current
		found = true
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	if !found && truncated {
		return nil, Truncated, nil
	}
	if !found {
		return nil, NotFound, nil
	}
	return body, Found, nil
}

// readTailBytes reads at most limit trailing bytes and reports whether the read
// window began mid-file.
func readTailBytes(path string, limit int64) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("无法读取 %s 的大小: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("日志路径 %s 不是普通文件，无法读取", path)
	}
	offset := int64(0)
	truncated := false
	if info.Size() > limit {
		offset = info.Size() - limit
		truncated = true
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("无法定位 %s: %w", path, err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, false, fmt.Errorf("无法读取 %s: %w", path, err)
	}
	return data, truncated, nil
}
