package slogbridge_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/slogbridge"
)

func TestHandlerForwardsRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG), log.WithJSON()))
	h := slogbridge.NewHandler(logger, slog.LevelDebug)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	record.AddAttrs(slog.String("user", "alice"), slog.Bool("active", true))

	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle failed: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("\"user\"")) {
		t.Fatalf("expected forwarded attributes, got %s", buf.String())
	}
}

func TestHandlerWithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	h := slogbridge.NewHandler(logger, slog.LevelInfo)
	h = h.WithAttrs([]slog.Attr{slog.String("env", "prod")}).(*slogbridge.Handler)
	h = h.WithGroup("http").(*slogbridge.Handler)

	record := slog.NewRecord(time.Now(), slog.LevelError, "failure", 0)
	record.AddAttrs(slog.Int("status", 500))

	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle failed: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("env=prod")) {
		t.Fatalf("expected env attribute, got %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("http.status=500")) {
		t.Fatalf("expected grouped attribute, got %s", out)
	}
}
