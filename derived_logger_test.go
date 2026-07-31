package log_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestLoggerWithBindsFields(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	child := logger.With(log.String("request_id", "r1"))

	child.Info("handling")
	out := buf.String()
	if !strings.Contains(out, "request_id=r1") || !strings.Contains(out, "handling") {
		t.Fatalf("expected bound field on derived logger output, got %q", out)
	}

	// The parent must not carry the child's bound field.
	buf.Reset()
	logger.Info("parent line")
	if strings.Contains(buf.String(), "request_id") {
		t.Fatalf("parent logger leaked a derived bound field: %q", buf.String())
	}
}

func TestLoggerWithChains(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	l2 := logger.With(log.String("a", "1"))
	l3 := l2.With(log.String("b", "2"))

	l3.Info("msg")
	out := buf.String()
	if !strings.Contains(out, "a=1") || !strings.Contains(out, "b=2") {
		t.Fatalf("expected both bound fields, got %q", out)
	}

	buf.Reset()
	l2.Info("msg")
	if strings.Contains(buf.String(), "b=2") {
		t.Fatalf("intermediate logger should not carry the grandchild field: %q", buf.String())
	}
}

func TestLoggerWithEmptyReturnsSame(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	if logger.With() != logger {
		t.Fatal("With() with no fields should return the same logger")
	}
}

// TestLoggerWithSharesCore verifies that derived loggers share the parent's
// mutable state: level, output, and hooks.
func TestLoggerWithSharesCore(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	child := logger.With(log.String("c", "x"))

	// Level change on the parent is observed by the child.
	logger.SetLevel(log.WARN)
	child.Info("below-threshold")
	if buf.Len() != 0 {
		t.Fatalf("child should honor parent's level change, got %q", buf.String())
	}
	child.Warn("at-threshold")
	if !strings.Contains(buf.String(), "at-threshold") {
		t.Fatalf("expected child WARN to log, got %q", buf.String())
	}

	// Hooks added on the parent fire for the child.
	var fired int
	logger.AddHook(log.HookFunc(func(log.LogLevel, string, []log.Field) { fired++ }))
	child.Warn("with hook")
	if fired != 1 {
		t.Fatalf("expected shared hook to fire once, got %d", fired)
	}
}

func TestLoggerWithBoundFieldsPrecedeCallFields(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})
	child := logger.With(log.String("bound", "b"))

	ctx := log.WithFields(context.Background(), log.String("ctx", "c"))
	child.InfoContext(ctx, "msg", log.String("call", "k"))

	out := buf.String()
	ib, ic, ik := strings.Index(out, `"bound"`), strings.Index(out, `"ctx"`), strings.Index(out, `"call"`)
	if ib < 0 || ic < 0 || ik < 0 {
		t.Fatalf("expected bound, ctx and call fields all present, got %q", out)
	}
	if ib >= ic || ic >= ik {
		t.Fatalf("expected order bound < ctx < call, got positions %d,%d,%d in %q", ib, ic, ik, out)
	}
}

func TestLoggerWithWorksInAsyncMode(t *testing.T) {
	var buf lockedBuffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.EnableAsync(log.AsyncOptions{QueueSize: 8})
	child := logger.With(log.String("svc", "api"))

	child.Info("async bound")
	logger.DisableAsync() // flush via the shared async state

	out := buf.String()
	if !strings.Contains(out, "async bound") || !strings.Contains(out, "svc=api") {
		t.Fatalf("expected derived logger to use shared async state with bound field, got %q", out)
	}
}

func TestLoggerLevelAndEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	if logger.Level() != log.INFO {
		t.Fatalf("expected Level()=INFO, got %v", logger.Level())
	}
	if logger.Enabled(log.DEBUG) {
		t.Fatal("DEBUG should be disabled at INFO")
	}
	if !logger.Enabled(log.INFO) || !logger.Enabled(log.ERROR) {
		t.Fatal("INFO and ERROR should be enabled at INFO")
	}

	logger.SetLevel(log.ERROR)
	if logger.Level() != log.ERROR {
		t.Fatalf("expected Level()=ERROR after SetLevel, got %v", logger.Level())
	}
	if logger.Enabled(log.WARN) {
		t.Fatal("WARN should be disabled at ERROR")
	}
}
