package log_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

// gatedWriter blocks on its first write until released, so a test can hold the
// async worker busy while the queue fills, making drop behavior deterministic.
type gatedWriter struct {
	buf     lockedBuffer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedWriter() *gatedWriter {
	return &gatedWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	g.once.Do(func() {
		close(g.started)
		<-g.release
	})
	return g.buf.Write(p)
}

func (g *gatedWriter) String() string { return g.buf.String() }

// TestAsyncConcurrentReconfiguration is the regression test for the
// send-on-closed-channel panic and the asyncOpts data race: many goroutines log
// continuously while another goroutine concurrently enables/disables async,
// switches the drop strategy, changes level, and swaps the output. Before the
// fix this panicked ("send on closed channel") and reported a data race under
// -race. It must now complete cleanly.
func TestAsyncConcurrentReconfiguration(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	defer logger.Close()

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Producers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				logger.Info("concurrent log line")
			}
		}()
	}

	// Reconfigurator.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			logger.SetLevel(log.INFO)
			logger.SetOutput(io.Discard)
			logger.SetLevel(log.DEBUG)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

func TestAsyncDropOldest(t *testing.T) {
	g := newGatedWriter()
	logger := log.Must(log.New(log.WithOutput(g), log.WithLevel(log.INFO), log.WithAsyncQueue(1), log.WithAsyncDropOldest()))

	logger.Info("a") // worker picks this up and blocks inside Write
	<-g.started      // queue is now empty and the worker is parked

	logger.Info("b") // sits in the queue
	logger.Info("c") // queue full -> evict "b", enqueue "c"
	logger.Info("d") // queue full -> evict "c", enqueue "d"

	stats := logger.AsyncStats()
	if stats.Dropped != 2 {
		t.Fatalf("expected 2 drops, got %d", stats.Dropped)
	}

	close(g.release)
	logger.Close()

	// The level token is "INFO" and timestamps are numeric, so the only source
	// of the letters a-d in the output is the message itself.
	out := g.String()
	if !strings.Contains(out, "a") || !strings.Contains(out, "d") {
		t.Fatalf("expected surviving entries a and d, got %q", out)
	}
	if strings.Contains(out, "b") || strings.Contains(out, "c") {
		t.Fatalf("expected oldest entries b and c to be dropped, got %q", out)
	}
}

func TestAsyncBlockWhenFull(t *testing.T) {
	g := newGatedWriter()
	logger := log.Must(log.New(log.WithOutput(g), log.WithLevel(log.INFO), log.WithAsyncQueue(1), log.WithAsyncBlocking()))

	logger.Info("a")
	<-g.started // worker parked inside Write("a")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("b")
		logger.Info("c") // blocks until the worker drains
	}()

	// Give the producer a chance to block on the full queue.
	time.Sleep(20 * time.Millisecond)
	close(g.release)
	wg.Wait()
	logger.Close()

	out := g.String()
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(out, want) {
			t.Fatalf("BlockWhenFull must deliver every entry; missing %q in %q", want, out)
		}
	}
	if dropped := logger.AsyncStats().Dropped; dropped != 0 {
		t.Fatalf("BlockWhenFull must not drop, got %d drops", dropped)
	}
}

func TestAsyncDropOldestKeepsNewest(t *testing.T) {
	g := newGatedWriter()
	logger := log.Must(log.New(log.WithOutput(g), log.WithLevel(log.INFO),
		log.WithAsyncQueue(1), log.WithAsyncDropOldest()))

	logger.Info("a")
	<-g.started

	logger.Info("b") // queued
	logger.Info("c") // queue full -> with DropOldest, evict "b" and keep "c"

	close(g.release)
	logger.Close()

	out := g.String()
	if !strings.Contains(out, "c") {
		t.Fatalf("under DropOldest the newest entry should survive, got %q", out)
	}
}

func TestAsyncStats(t *testing.T) {
	// Disabled async: zero-valued stats.
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	if s := logger.AsyncStats(); s.QueueSize != 0 || s.QueueLength != 0 || s.Dropped != 0 {
		t.Fatalf("expected zero stats when async disabled, got %+v", s)
	}

	// Enabled async with a parked worker: QueueLength reflects buffered entries.
	g := newGatedWriter()
	logger = log.Must(log.New(log.WithOutput(g), log.WithLevel(log.INFO),
		log.WithAsyncQueue(4), log.WithAsyncBlocking()))
	defer logger.Close()

	logger.Info("a")
	<-g.started // worker parked, queue empty

	logger.Info("b")
	logger.Info("c")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if logger.AsyncStats().QueueLength == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s := logger.AsyncStats()
	if s.QueueSize != 4 {
		t.Fatalf("expected QueueSize 4, got %d", s.QueueSize)
	}
	if s.QueueLength != 2 {
		t.Fatalf("expected QueueLength 2, got %d", s.QueueLength)
	}
	close(g.release)
}

// panicHook always panics when fired.
type panicHook struct{}

func (panicHook) Fire(level log.LogLevel, message string, fields []log.Field) {
	panic("boom")
}

// TestHookPanicDoesNotCrash verifies that a panicking hook is isolated and does
// not bring down the caller (sync) or the async worker.
func TestHookPanicDoesNotCrash(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithHook(panicHook{})))

	// Synchronous: must not panic, and the line must still be written.
	logger.Info("sync-after-hook")
	if !strings.Contains(buf.String(), "sync-after-hook") {
		t.Fatalf("expected log line written despite hook panic, got %q", buf.String())
	}

	// Asynchronous: the worker must survive the panic and keep draining.
	asyncLogger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithHook(panicHook{}), log.WithAsyncQueue(8)))
	asyncLogger.Info("async-after-hook")
	asyncLogger.Close()
	if !strings.Contains(buf.String(), "async-after-hook") {
		t.Fatalf("expected async log line after hook panic, got %q", buf.String())
	}
}

// TestFatalExitsInAsyncMode runs the logger in a subprocess and asserts that
// Fatal terminates the process with exit code 1 even when async logging is
// enabled and an aggressive sampler is installed — the documented contract.
func TestFatalExitsInAsyncMode(t *testing.T) {
	if os.Getenv("LOGGER_FATAL_SUBPROCESS") == "1" {
		logger := log.Must(log.New(log.WithOutput(os.Stdout), log.WithLevel(log.INFO),
			log.WithAsyncQueue(8),
			log.WithSampler(log.NewEveryNSampler(1000)))) // would drop almost everything
		logger.Fatal("fatal in async mode")
		// Must never reach here.
		os.Stdout.WriteString("REACHED-AFTER-FATAL\n")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalExitsInAsyncMode", "-test.v")
	cmd.Env = append(os.Environ(), "LOGGER_FATAL_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()

	if strings.Contains(string(out), "REACHED-AFTER-FATAL") {
		t.Fatalf("Fatal did not stop execution: %s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected the subprocess to exit non-zero, got err=%v, out=%s", err, out)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Fatalf("expected exit code 1 from Fatal, got %d (out=%s)", code, out)
	}
}
