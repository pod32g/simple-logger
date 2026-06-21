package log_test

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestParseLevelAndString(t *testing.T) {
	cases := map[string]log.LogLevel{
		"debug": log.DEBUG, "INFO": log.INFO, "warn": log.WARN,
		"warning": log.WARN, "Error": log.ERROR, " fatal ": log.FATAL,
	}
	for s, want := range cases {
		got, err := log.ParseLevel(s)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	if _, err := log.ParseLevel("bogus"); err == nil {
		t.Error("expected error for unknown level")
	}
	if log.ERROR.String() != "ERROR" {
		t.Errorf("LogLevel.String() = %q", log.ERROR.String())
	}
}

func TestDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	custom := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	log.SetDefault(custom)
	if log.Default() != custom {
		t.Fatal("Default() should return the logger set via SetDefault")
	}
	log.Default().Info("via default")
	if !strings.Contains(buf.String(), "via default") {
		t.Fatalf("expected default logger output, got %q", buf.String())
	}
}

func TestNamedLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.Named("http").Named("auth").Info("msg")
	if !strings.Contains(buf.String(), "logger=http.auth") {
		t.Fatalf("expected dotted component name, got %q", buf.String())
	}
}

func TestKeyRedactor(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})
	logger.SetRedactor(log.NewKeyRedactor("password", "token"))

	logger.InfoFields("login", log.String("user", "alice"), log.String("Password", "hunter2"))
	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password value leaked: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") || !strings.Contains(out, "alice") {
		t.Fatalf("expected redaction placeholder and untouched field, got %q", out)
	}
}

func TestPatternScrubber(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	bearer := regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)
	logger.SetRedactor(log.NewPatternScrubber(bearer))

	logger.InfoFields("auth Bearer abc123.def", log.String("hdr", "Bearer secrettoken"))
	out := buf.String()
	if strings.Contains(out, "abc123") || strings.Contains(out, "secrettoken") {
		t.Fatalf("scrubber failed to mask secrets: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected placeholder, got %q", out)
	}
}

func TestDeduplicateFields(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})
	logger.SetDeduplicateFields(true)
	logger.InfoFields("dup", log.String("k", "first"), log.String("k", "last"))
	out := buf.String()
	if strings.Count(out, `"k"`) != 1 {
		t.Fatalf("expected a single k key, got %q", out)
	}
	if !strings.Contains(out, "last") || strings.Contains(out, "first") {
		t.Fatalf("expected last-wins dedup, got %q", out)
	}
}

func TestMaxFieldAndMessageBytes(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetMaxFieldBytes(5)
	logger.SetMaxMessageBytes(4)
	logger.InfoFields("abcdefgh", log.String("k", "0123456789"))
	out := buf.String()
	if !strings.Contains(out, "...[+") {
		t.Fatalf("expected truncation marker, got %q", out)
	}
	if strings.Contains(out, "0123456789") || strings.Contains(out, "abcdefgh") {
		t.Fatalf("expected truncated value and message, got %q", out)
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func TestWriteErrorHandler(t *testing.T) {
	sentinel := errors.New("disk full")
	logger := log.NewLogger(failWriter{err: sentinel}, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	var got error
	logger.SetErrorHandler(func(err error) { got = err })

	logger.Info("lost line")
	if !errors.Is(got, sentinel) {
		t.Fatalf("expected error handler to receive %v, got %v", sentinel, got)
	}
	if logger.WriteErrors() != 1 {
		t.Fatalf("expected WriteErrors()=1, got %d", logger.WriteErrors())
	}
}

func TestFlushDrainsWithoutTeardown(t *testing.T) {
	var buf lockedBuffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.EnableAsync(log.AsyncOptions{QueueSize: 16})
	defer logger.DisableAsync()

	logger.Info("first")
	logger.Flush()
	if !strings.Contains(buf.String(), "first") {
		t.Fatalf("Flush did not drain queued entry, got %q", buf.String())
	}
	// Worker still running: a second round must also flush.
	logger.Info("second")
	logger.Flush()
	if !strings.Contains(buf.String(), "second") {
		t.Fatalf("logger unusable after Flush, got %q", buf.String())
	}
}

type syncBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	synced bool
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}
func (s *syncBuffer) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.synced = true
	return nil
}

func TestSyncCallsUnderlyingSync(t *testing.T) {
	sb := &syncBuffer{}
	logger := log.NewLogger(sb, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.Info("x")
	if err := logger.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if !sb.synced {
		t.Fatal("expected Sync to call the writer's Sync method")
	}
}

func TestRecoverRepanicsAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	repanicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				repanicked = true
			}
		}()
		defer logger.Recover()
		panic("boom")
	}()

	if !repanicked {
		t.Fatal("Recover should re-panic")
	}
	if !strings.Contains(buf.String(), "recovered panic") || !strings.Contains(buf.String(), "boom") {
		t.Fatalf("expected panic logged with cause, got %q", buf.String())
	}
}

func TestRecoverAndContinueSwallows(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	reached := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecoverAndContinue should not re-panic, got %v", r)
			}
		}()
		func() {
			defer logger.RecoverAndContinue()
			panic("oops")
		}()
		reached = true
	}()

	if !reached {
		t.Fatal("execution should continue after RecoverAndContinue")
	}
	if !strings.Contains(buf.String(), "oops") {
		t.Fatalf("expected panic logged, got %q", buf.String())
	}
}
