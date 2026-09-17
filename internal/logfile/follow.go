package logfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// StreamFrom follows this logger's file starting at position, which is how a
// caller that already printed part of the file continues without a gap.
//
// The position is the offset a previous read stopped at, and it is a
// continuation only: 0 means "attach", so an existing file is followed from its
// end (only a file that appears later is read from its beginning). A position
// the file has outgrown — the replacement is shorter — is treated as "this is a
// new generation" and the file is read from the beginning rather than from a
// stale offset.
//
// Parameters:
//   - ctx: cancellation ends the follow.
//   - w: destination.
//   - position: byte offset to continue from, or 0 to attach at the end of an
//     existing file.
func (l *Logger) StreamFrom(ctx context.Context, w io.Writer, position int64) error {
	interval := l.poll
	if interval <= 0 {
		interval = defaultPoll
	}
	return followFrom(ctx, l.Path, w, interval, position, l.settled)
}

// Stream follows this logger's file and writes everything appended after the
// call returns.
//
// A file that already exists is followed from its end; a file created after the
// follow started is streamed from its beginning. Replacement (rotation by
// rename) and truncation are both detected by comparing the open file with the
// path, so a follower keeps working across `dshctl build` and across an operator
// deleting the log.
//
// Parameters:
//   - ctx: cancellation ends the follow.
//   - w: destination.
//
// Returns:
//   - the context error on cancellation, or an error when writing to w fails.
func (l *Logger) Stream(ctx context.Context, w io.Writer) error {
	return l.StreamFrom(ctx, w, 0)
}

// followFrom implements the tail -F loop.
//
// The handle lives for one read and is closed again before the poll sleeps. A
// follower that kept the log open would pin it: Windows refuses to rename or
// delete a file that another handle has open, so `dshctl logs -f` running in one
// terminal would stop the operator (or a rotation script) from replacing the log
// in another. The file's identity is what carries the follow across ticks, and
// the position is what decides where the next read starts.
//
// settled, when not nil, is called once after the first pass through the loop
// has finished. The starting position is fixed by then — the first attach has
// happened, or an absent file has been observed — so anything written after the
// call is streamed. It exists for tests, which otherwise have to guess how long
// the follower's goroutine needs to be scheduled.
func followFrom(ctx context.Context, path string, w io.Writer, interval time.Duration, position int64, settled func()) error {
	var (
		file *os.File
		// read is the file the last read came from. It outlives the handle on
		// purpose: FileInfo compares by identity, which is what tells a rotation
		// (a different file at the same path) from an ordinary append.
		read   os.FileInfo
		offset int64
		// A file that exists when the follow starts is followed from its end; a
		// file created afterwards is streamed from its beginning. A caller that
		// already read part of the file passes the position instead.
		skipExisting = position == 0 && fileExists(path)
		startAt      = position
	)

	closeFile := func() {
		if file != nil {
			_ = file.Close()
			file = nil
		}
	}
	defer closeFile()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for pass := 0; ; pass++ {
		// The first pass has fixed where this follow reads from — its attach
		// happened, or the file was observed to be absent — so a caller told
		// about it can write content that is guaranteed to be streamed.
		if pass == 1 && settled != nil {
			settled()
			settled = nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			// Rotated away or not created yet; wait for it to appear. A
			// caller's position applied to the file that existed when the tail
			// ran; once that file is gone the position is void, and whatever
			// appears next is a new generation read from its beginning.
			closeFile()
			read = nil
			offset = 0
			startAt = 0
			if err := wait(ctx, ticker); err != nil {
				return err
			}
			continue
		} else if err != nil {
			return fmt.Errorf("无法读取 %s: %w", path, err)
		}

		reopened, openErr := openForFollow(path)
		if openErr != nil {
			// The path changed again between the stat and the open; the next
			// tick starts over rather than guessing.
			if err := wait(ctx, ticker); err != nil {
				return err
			}
			continue
		}
		descriptor, statErr := reopened.Stat()
		if statErr != nil {
			_ = reopened.Close()
			if err := wait(ctx, ticker); err != nil {
				return err
			}
			continue
		}

		// Attach, or re-attach, when any of three things is true: nothing has
		// been read yet; the path now names a different file (rotation by
		// rename); or the file is shorter than what was already read (truncated
		// in place). The third case also covers an inode that was unlinked and
		// then reused for the replacement — same identity, different content —
		// which identity alone cannot see.
		if read == nil || !os.SameFile(read, descriptor) || descriptor.Size() < offset {
			switch {
			case startAt > 0 && startAt <= descriptor.Size():
				// Continue where the caller stopped reading. A position past the
				// end means the file was replaced by a shorter one, so the new
				// content is read from its beginning.
				offset = startAt
			case skipExisting:
				offset = descriptor.Size()
			default:
				offset = 0
			}
			// The caller's position applies to one attach only. After the
			// handoff is consumed, a later re-attach — truncation, replacement —
			// reads the new content from its beginning; keeping the stale
			// position would jump the follower backwards or skip lines.
			startAt = 0
			skipExisting = false
		}
		read = descriptor
		file = reopened

		if descriptor.Size() > offset {
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				closeFile()
				return fmt.Errorf("无法定位 %s: %w", path, err)
			}
			written, err := io.Copy(w, file)
			if err != nil {
				closeFile()
				return err
			}
			offset += written
		}
		closeFile()

		if err := wait(ctx, ticker); err != nil {
			return err
		}
	}
}

// wait blocks until the next poll, or returns the context error.
func wait(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		return nil
	}
}

// fileExists reports whether path names an existing file.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
