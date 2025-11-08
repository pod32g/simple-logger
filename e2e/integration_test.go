package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/otlp"
	"github.com/pod32g/simple-logger/bridge/slogbridge"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

type logEntry struct {
	level   log.LogLevel
	message string
}

func TestAsyncStatsEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.EnableAsync(log.AsyncOptions{QueueSize: 4, DropStrategy: log.DropNew})

	for i := 0; i < 200; i++ {
		logger.InfoString("async-drop-test")
	}

	stats := logger.AsyncStats()
	logger.DisableAsync()

	if stats.Dropped == 0 {
		t.Fatalf("expected drops to occur, got stats=%+v", stats)
	}
	if stats.QueueSize != 4 {
		t.Fatalf("unexpected queue size: %#v", stats)
	}
}

func TestHookFiltersEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.DEBUG, &log.DefaultFormatter{IncludeCaller: false})

	var mu sync.Mutex
	allEntries := make([]logEntry, 0, 2)
	filtered := make([]logEntry, 0, 1)

	logger.AddHook(log.HookFunc(func(level log.LogLevel, message string, _ []log.Field) {
		mu.Lock()
		allEntries = append(allEntries, logEntry{level: level, message: message})
		mu.Unlock()
	}))

	logger.AddHook(log.HookFunc(func(level log.LogLevel, message string, _ []log.Field) {
		mu.Lock()
		filtered = append(filtered, logEntry{level: level, message: message})
		mu.Unlock()
	}),
		log.WithHookLevels(log.ERROR),
		log.WithHookFilter(func(level log.LogLevel, message string, _ []log.Field) bool {
			return level == log.ERROR && message == "important"
		}),
	)

	logger.InfoFields("ignored", log.String("key", "value"))
	logger.ErrorFields("important", log.String("key", "value"))
	logger.ErrorFields("other", log.String("key", "value"))

	mu.Lock()
	defer mu.Unlock()
	if len(allEntries) != 3 {
		t.Fatalf("expected three hook calls, got %d", len(allEntries))
	}
	if len(filtered) != 1 {
		t.Fatalf("expected exactly one filtered hook entry, got %d", len(filtered))
	}
	if filtered[0].message != "important" {
		t.Fatalf("unexpected message recorded: %q", filtered[0].message)
	}
}

func TestSlogBridgeEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})
	handler := slogbridge.NewHandler(logger, slog.LevelInfo)
	slogger := slog.New(handler)

	slogger.Info("bridge", slog.String("user", "alice"), slog.Int("count", 2))

	var payload map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload["message"] != "bridge" {
		t.Fatalf("unexpected message: %v", payload["message"])
	}
	if payload["user"] != "alice" {
		t.Fatalf("expected user field, got %v", payload["user"])
	}
	if payload["count"] != float64(2) {
		t.Fatalf("expected count=2, got %v", payload["count"])
	}
}

type fakeExporter struct {
	received chan *logspb.ResourceLogs
}

func (f *fakeExporter) Export(_ context.Context, rl *logspb.ResourceLogs) error {
	f.received <- rl
	return nil
}

func (f *fakeExporter) Shutdown(context.Context) error { return nil }

func TestOTLPHookEndToEnd(t *testing.T) {
	exporter := &fakeExporter{received: make(chan *logspb.ResourceLogs, 1)}
	hook := otlp.NewHook(exporter, otlp.WithServiceName("svc"))

	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.AddHook(hook)

	logger.InfoFields("otlp", log.String("key", "value"))

	select {
	case rl := <-exporter.received:
		if len(rl.ScopeLogs) == 0 || len(rl.ScopeLogs[0].LogRecords) == 0 {
			t.Fatalf("expected log record in exported data")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for OTLP export")
	}
}
