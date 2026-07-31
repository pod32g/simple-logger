package otlp

import (
	"context"
	"sync"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// batchExporter records each export call and how many records it carried.
type batchExporter struct {
	mu       sync.Mutex
	batches  [][]*logspb.LogRecord
	block    chan struct{}
	deadline chan time.Duration
}

func (b *batchExporter) Export(ctx context.Context, rl *logspb.ResourceLogs) error {
	if b.deadline != nil {
		d, ok := ctx.Deadline()
		var remaining time.Duration
		if ok {
			remaining = time.Until(d)
		}
		b.deadline <- remaining
	}
	if b.block != nil {
		<-b.block
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(rl.ScopeLogs) > 0 {
		b.batches = append(b.batches, rl.ScopeLogs[0].LogRecords)
	}
	return nil
}

func (b *batchExporter) Shutdown(context.Context) error { return nil }

func (b *batchExporter) counts() (batches, records int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, batch := range b.batches {
		batches++
		records += len(batch)
	}
	return
}

func TestHookBatchesRecords(t *testing.T) {
	exp := &batchExporter{}
	hook := NewHook(exp, WithBatchSize(10), WithFlushInterval(time.Hour))
	defer hook.Close(context.Background())

	for i := 0; i < 30; i++ {
		hook.Fire(log.INFO, "msg", nil)
	}
	if err := hook.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	batches, records := exp.counts()
	if records != 30 {
		t.Errorf("exported %d records, want 30", records)
	}
	if batches > 4 { // 3 full batches, plus at most one partial from the flush
		t.Errorf("exported %d batches, want them grouped", batches)
	}
}

// A log call must not wait on the collector.
func TestFireDoesNotBlockOnSlowExporter(t *testing.T) {
	exp := &batchExporter{block: make(chan struct{})}
	hook := NewHook(exp, WithBatchSize(1), WithFlushInterval(time.Millisecond))
	defer func() { close(exp.block); hook.Close(context.Background()) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			hook.Fire(log.INFO, "msg", nil)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Fire blocked while the exporter was stalled")
	}
}

// A full queue costs telemetry, not application latency, and says so in Stats.
func TestFullQueueDropsAndCounts(t *testing.T) {
	exp := &batchExporter{block: make(chan struct{})}
	hook := NewHook(exp, WithQueueSize(4), WithBatchSize(1), WithFlushInterval(time.Hour))
	defer func() { close(exp.block); hook.Close(context.Background()) }()

	for i := 0; i < 200; i++ {
		hook.Fire(log.INFO, "msg", nil)
	}
	if got := hook.Stats().Dropped; got == 0 {
		t.Error("expected drops once the queue filled")
	}
}

func TestExportCarriesDeadline(t *testing.T) {
	exp := &batchExporter{deadline: make(chan time.Duration, 1)}
	hook := NewHook(exp, WithBatchSize(1), WithExportTimeout(250*time.Millisecond))
	defer hook.Close(context.Background())

	hook.Fire(log.INFO, "msg", nil)
	select {
	case remaining := <-exp.deadline:
		if remaining <= 0 || remaining > 250*time.Millisecond {
			t.Errorf("export deadline = %v, want just under 250ms", remaining)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("export never happened")
	}
}

func TestExportTimeoutCanBeDisabled(t *testing.T) {
	exp := &batchExporter{deadline: make(chan time.Duration, 1)}
	hook := NewHook(exp, WithBatchSize(1), WithExportTimeout(0))
	defer hook.Close(context.Background())

	hook.Fire(log.INFO, "msg", nil)
	select {
	case remaining := <-exp.deadline:
		if remaining != 0 {
			t.Errorf("expected no deadline, got %v remaining", remaining)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("export never happened")
	}
}

// Close must ship what is still queued rather than discard it.
func TestCloseExportsPendingRecords(t *testing.T) {
	exp := &batchExporter{}
	hook := NewHook(exp, WithBatchSize(1000), WithFlushInterval(time.Hour))

	for i := 0; i < 7; i++ {
		hook.Fire(log.INFO, "msg", nil)
	}
	if err := hook.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, records := exp.counts(); records != 7 {
		t.Errorf("Close exported %d of 7 queued records", records)
	}
}

func TestFireAfterCloseIsSafe(t *testing.T) {
	exp := &batchExporter{}
	hook := NewHook(exp, WithBatchSize(1))
	if err := hook.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	hook.Fire(log.INFO, "msg", nil) // must not panic on the closed queue
	if err := hook.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFlushRespectsContext(t *testing.T) {
	exp := &batchExporter{block: make(chan struct{})}
	hook := NewHook(exp, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer func() { close(exp.block); hook.Close(context.Background()) }()

	hook.Fire(log.INFO, "msg", nil)
	time.Sleep(50 * time.Millisecond) // worker is now stuck in Export

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := hook.Flush(ctx); err == nil {
		t.Error("expected Flush to give up when its context expired")
	}
}
