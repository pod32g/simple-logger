package log_test

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"

	log "github.com/pod32g/simple-logger"
)

type countingCloser struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed atomic.Int64
}

func (c *countingCloser) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *countingCloser) Close() error {
	c.closed.Add(1)
	return nil
}

// A superseded writer must be closed when it is replaced, not held until the
// logger itself is closed -- a config watcher swaps the output on every reload.
func TestSupersededWriterIsClosedOnReplacement(t *testing.T) {
	for _, synchronized := range []bool{true, false} {
		name := "synchronized"
		if !synchronized {
			name = "unsynchronized"
		}
		t.Run(name, func(t *testing.T) {
			first := &countingCloser{}
			l := log.NewLogger(first, log.INFO, &log.DefaultFormatter{})
			l.SetSynchronized(synchronized)
			l.SetOutputWithCloser(first, first)

			var writers []*countingCloser
			for i := 0; i < 5; i++ {
				next := &countingCloser{}
				writers = append(writers, next)
				l.SetOutputWithCloser(next, next)
				l.InfoString("after swap")
			}

			if got := first.closed.Load(); got != 1 {
				t.Errorf("first writer closed %d times, want 1", got)
			}
			for i, w := range writers[:len(writers)-1] {
				if got := w.closed.Load(); got != 1 {
					t.Errorf("writer %d closed %d times, want 1", i, got)
				}
			}
			current := writers[len(writers)-1]
			if got := current.closed.Load(); got != 0 {
				t.Errorf("current writer closed %d times before Close, want 0", got)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			if got := current.closed.Load(); got != 1 {
				t.Errorf("current writer closed %d times after Close, want 1", got)
			}
		})
	}
}

// Swapping the output while unsynchronized writers are in flight must not close
// a writer somebody is still writing to.
func TestConcurrentWritesDuringOutputSwap(t *testing.T) {
	l := log.NewLogger(&countingCloser{}, log.INFO, &log.DefaultFormatter{})
	l.SetSynchronized(false)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					l.InfoString("concurrent")
				}
			}
		}()
	}

	for i := 0; i < 50; i++ {
		w := &countingCloser{}
		l.SetOutputWithCloser(w, w)
	}
	close(stop)
	wg.Wait()

	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}
