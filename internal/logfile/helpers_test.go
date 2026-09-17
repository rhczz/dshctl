package logfile

import (
	"context"
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
