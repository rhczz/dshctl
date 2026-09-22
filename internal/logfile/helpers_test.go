package logfile

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe to read while a follower writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write implements io.Writer.
func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

// String returns what has been written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newCancelContext returns a cancellable context whose cancel is safe to call
// from a deferred cleanup.
func newCancelContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// waitForText blocks until the buffer holds text or the deadline passes.
func waitForText(t *testing.T, sink *syncBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.String(), text) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", text, sink.String())
}

// renameOnto renames one file onto another, tolerating a transient refusal.
//
// The follower keeps the log open for the length of one read and closes it again
// before the next poll — that is what stops `dshctl logs -f` from pinning a
// Windows rotation — but during that read Windows can answer a rename with a
// permission error. A test that replaces the log while a follower runs has to
// tolerate the window, exactly as an operator's rotation script does; without
// the retry the test fails whenever the follower happened to be reading at that
// instant, which says nothing about replacement handling.
func renameOnto(t *testing.T, source, target string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Rename(source, target)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rename %s to %s: %v", source, target, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// awaitSettled makes the follower report when its first pass has fixed the
// position it reads from, and returns a function that waits for that report.
//
// A test that writes the line it expects to be streamed has to know the pass
// happened: the follower decides its starting position inside its own goroutine,
// so sleeping first is a bet on the scheduler, and a loaded runner loses it.
func awaitSettled(t *testing.T, logger *Logger) func() {
	t.Helper()
	settled := make(chan struct{}, 1)
	logger.SetSettledHook(func() {
		select {
		case settled <- struct{}{}:
		default:
		}
	})
	return func() {
		t.Helper()
		select {
		case <-settled:
		case <-time.After(3 * time.Second):
			t.Fatal("the follower never completed its first pass")
		}
	}
}

// testFormat is the marker shape the tests exercise. It matches the product's
// format so the expectations stay readable, and it is declared here rather than
// imported from the product: a mechanism's test proves the mechanism works for
// the shape it is handed.
var testFormat = Format{Prefix: "=====", Product: "dshctl", Layout: "2006-01-02 15:04:05"}
