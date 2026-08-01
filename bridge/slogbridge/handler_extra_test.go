package slogbridge_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/slogbridge"
)

func TestHandlerEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	h := slogbridge.NewHandler(logger, slog.LevelWarn)

	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelInfo) {
		t.Errorf("Info should be disabled at Warn threshold")
	}
	if !h.Enabled(ctx, slog.LevelWarn) {
		t.Errorf("Warn should be enabled at Warn threshold")
	}
	if !h.Enabled(ctx, slog.LevelError) {
		t.Errorf("Error should be enabled at Warn threshold")
	}
}

// TestHandlerEnabledThroughSlog exercises the Enabled gate via the real
// slog.Logger path, which calls Enabled before constructing a record.
func TestHandlerEnabledThroughSlog(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	sl := slog.New(slogbridge.NewHandler(logger, slog.LevelWarn))

	sl.Info("below-threshold")
	if buf.Len() != 0 {
		t.Fatalf("expected Info to be filtered by Enabled gate, got %q", buf.String())
	}

	sl.Warn("at-threshold")
	if !strings.Contains(buf.String(), "at-threshold") {
		t.Fatalf("expected Warn to pass the Enabled gate, got %q", buf.String())
	}
}

type lazyValuer struct{ v string }

func (l lazyValuer) LogValue() slog.Value { return slog.StringValue(l.v) }

func TestHandlerAttrKinds(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG), log.WithJSON()))
	h := slogbridge.NewHandler(logger, slog.LevelDebug)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "kinds", 0)
	record.AddAttrs(
		slog.Uint64("u", 42),
		slog.Float64("f", 3.5),
		slog.Duration("d", 2*time.Second),
		slog.Time("t", time.Unix(0, 0).UTC()),
		slog.Group("req", slog.String("id", "abc")),
		slog.Any("lazy", lazyValuer{v: "resolved"}),
	)

	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle failed: %v", err)
	}

	out := buf.String()
	for _, want := range []string{`"u"`, `"f"`, `"d"`, `"t"`, `"req.id"`, `"abc"`, `"lazy"`, `"resolved"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %s in output, got %s", want, out)
		}
	}
}

func TestHandlerLevelMapping(t *testing.T) {
	cases := []struct {
		level slog.Level
		token string
	}{
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError + 4, "ERROR"}, // custom level above Error -> default branch
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
		h := slogbridge.NewHandler(logger, slog.LevelDebug)
		record := slog.NewRecord(time.Now(), tc.level, "m", 0)
		if err := h.Handle(context.Background(), record); err != nil {
			t.Fatalf("handle failed: %v", err)
		}
		if !strings.Contains(buf.String(), tc.token) {
			t.Errorf("slog level %v: expected %q token, got %q", tc.level, tc.token, buf.String())
		}
	}
}
