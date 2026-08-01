package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

// blockingWriter blocks the first Write until release is closed, pinning the
// async worker so the queue can be driven into a known state.
type blockingWriter struct {
	once    sync.Once
	release chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	b.once.Do(func() { <-b.release })
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func TestFlushBarrierSurvivesDropOldest(t *testing.T) {
	w := &blockingWriter{release: make(chan struct{})}
	l := log.Must(log.New(log.WithOutput(w), log.WithLevel(log.INFO), log.WithAsyncQueue(4), log.WithAsyncDropOldest()))
	defer l.Close()

	l.Info("A") // the worker picks this up and blocks inside Write
	waitFor(t, func() bool { return l.AsyncStats().QueueLength == 0 })

	flushed := make(chan struct{})
	go func() { l.Flush(); close(flushed) }()
	waitFor(t, func() bool { return l.AsyncStats().QueueLength > 0 }) // barrier queued

	for i := 0; i < 8; i++ { // overfill: log.DropOldest must not evict the barrier
		l.Info("filler")
	}
	close(w.release)

	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush did not return: the barrier was dropped")
	}
	if got := l.AsyncStats().Dropped; got == 0 {
		t.Errorf("expected entries to be dropped under log.DropOldest, got %d", got)
	}
}

func TestFlushBarrierNotDroppedWhenOnlyBarriersQueued(t *testing.T) {
	w := &blockingWriter{release: make(chan struct{})}
	l := log.Must(log.New(log.WithOutput(w), log.WithLevel(log.INFO), log.WithAsyncQueue(2), log.WithAsyncDropOldest()))
	defer l.Close()

	l.Info("A")
	waitFor(t, func() bool { return l.AsyncStats().QueueLength == 0 })

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ { // fill the queue with barriers only
		wg.Add(1)
		go func() { defer wg.Done(); l.Flush() }()
	}
	waitFor(t, func() bool { return l.AsyncStats().QueueLength == 2 })

	l.Info("arrives with no droppable entry available")
	close(w.release)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a Flush barrier was stranded")
	}
}

func TestAsyncPreservesCallerAndStacktrace(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	sink := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	})

	l := log.Must(log.New(log.WithOutput(sink), log.WithLevel(log.INFO), log.WithJSON(), log.WithCaller(), log.WithStacktrace(), log.WithAsyncQueue(16)))
	l.Error("boom")
	l.Flush()
	l.Close()

	mu.Lock()
	got := buf.String()
	mu.Unlock()

	if !strings.Contains(got, `"file":"async_reliability_test.go"`) {
		t.Errorf("async entry lost its call site: %s", got)
	}
	if strings.Contains(got, "asyncWorker") {
		t.Errorf("stacktrace was captured on the worker rather than the caller: %s", got)
	}
	if !strings.Contains(got, "TestAsyncPreservesCallerAndStacktrace") {
		t.Errorf("stacktrace does not contain the logging goroutine: %s", got)
	}
}

func TestSyncCallerStillResolved(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(), log.WithCaller()))
	l.Error("boom")
	if !strings.Contains(buf.String(), `"file":"async_reliability_test.go"`) {
		t.Errorf("synchronous caller resolution regressed: %s", buf.String())
	}
}

func TestFatalDrainsAsyncQueue(t *testing.T) {
	if os.Getenv("FATAL_DRAIN_CHILD") == "1" {
		l := log.Must(log.New(log.WithOutput(os.Stdout), log.WithLevel(log.INFO), log.WithAsyncQueue(128), log.WithAsyncBatch(32, 0)))
		for i := 0; i < 5; i++ {
			l.Info("queued-entry")
		}
		l.Fatal("fatal-entry")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalDrainsAsyncQueue")
	cmd.Env = append(os.Environ(), "FATAL_DRAIN_CHILD=1")
	out, _ := cmd.CombinedOutput()
	got := string(out)

	if n := strings.Count(got, "queued-entry"); n != 5 {
		t.Errorf("Fatal wrote %d of 5 queued entries before exiting:\n%s", n, got)
	}
	if !strings.Contains(got, "fatal-entry") {
		t.Errorf("fatal entry missing:\n%s", got)
	}
	if strings.Index(got, "fatal-entry") < strings.LastIndex(got, "queued-entry") {
		t.Errorf("fatal entry was written before the queued entries:\n%s", got)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
