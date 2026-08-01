package log_test

import (
	"bytes"
	"encoding/json"
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
	custom := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	logger.Named("http").Named("auth").Info("msg")
	if !strings.Contains(buf.String(), "logger=http.auth") {
		t.Fatalf("expected dotted component name, got %q", buf.String())
	}
}

func TestKeyRedactor(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(), log.WithRedactor(log.NewKeyRedactor("password", "token"))))

	logger.Info("login", log.String("user", "alice"), log.String("Password", "hunter2"))
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
	bearer := regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithRedactor(log.NewPatternScrubber(bearer))))

	logger.Info("auth Bearer abc123.def", log.String("hdr", "Bearer secrettoken"))
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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(), log.WithDeduplicateFields()))
	logger.Info("dup", log.String("k", "first"), log.String("k", "last"))
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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithMaxFieldBytes(5), log.WithMaxMessageBytes(4)))
	logger.Info("abcdefgh", log.String("k", "0123456789"))
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
	var got error
	logger := log.Must(log.New(log.WithOutput(failWriter{err: sentinel}), log.WithLevel(log.INFO),
		log.WithErrorHandler(func(err error) { got = err })))

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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithAsyncQueue(16)))
	defer logger.Close()

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
	logger := log.Must(log.New(log.WithOutput(sb), log.WithLevel(log.INFO)))
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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

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
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

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

// --- merged from json_encoding_test.go ---

// Every entry must parse as JSON no matter what bytes reach it, and the values
// must survive the round trip intact.
func TestJSONFormatterAlwaysEmitsValidJSON(t *testing.T) {
	cases := []struct {
		name      string
		opts      []log.Option
		message   string
		fields    []log.Field
		wantKey   string
		wantValue string
	}{
		{
			name:      "quote in key",
			fields:    []log.Field{log.String(`ke"y`, "v")},
			wantKey:   `ke"y`,
			wantValue: "v",
		},
		{
			name:      "newline in key",
			fields:    []log.Field{log.String("a\nb", "v")},
			wantKey:   "a\nb",
			wantValue: "v",
		},
		{
			name:      "key that would close the object and forge a field",
			fields:    []log.Field{log.String(`x":"y","injected":"1`, "v")},
			wantKey:   `x":"y","injected":"1`,
			wantValue: "v",
		},
		{
			name:      "control characters in value",
			fields:    []log.Field{log.String("k", "a\x00b\x1fc")},
			wantKey:   "k",
			wantValue: "a\x00b\x1fc",
		},
		{
			name:      "non-printable astral rune",
			fields:    []log.Field{log.String("k", "\U0001D173")},
			wantKey:   "k",
			wantValue: "\U0001D173",
		},
		{
			name:      "emoji survives unescaped",
			fields:    []log.Field{log.String("k", "ok 🎉")},
			wantKey:   "k",
			wantValue: "ok 🎉",
		},
		{
			name:      "invalid utf8 becomes the replacement rune",
			fields:    []log.Field{log.String("k", "a\xffb")},
			wantKey:   "k",
			wantValue: "a�b",
		},
		{
			name:      "truncation that lands mid-rune",
			opts:      []log.Option{log.WithMaxFieldBytes(2)},
			fields:    []log.Field{log.String("k", "aé")},
			wantKey:   "k",
			wantValue: "a...[+2 bytes]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.Must(log.New(append(
				[]log.Option{log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()},
				tc.opts...)...))
			msg := tc.message
			if msg == "" {
				msg = "m"
			}
			l.Info(msg, tc.fields...)

			var out map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
			}
			got, ok := out[tc.wantKey]
			if !ok {
				t.Fatalf("key %q missing from %v", tc.wantKey, out)
			}
			if got != tc.wantValue {
				t.Errorf("value = %q, want %q", got, tc.wantValue)
			}
			if len(out) != 4 { // timestamp, level, message, the one field
				t.Errorf("unexpected field count %d in %v -- a field may have been forged", len(out), out)
			}
		})
	}
}

func TestJSONFormatterEscapesMessage(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))
	l.Info("line one\nline two\ttabbed \"quoted\"")

	if n := bytes.Count(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")); n != 0 {
		t.Errorf("message newline was not escaped, entry spans %d extra lines: %s", n, buf.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
	}
	if out["message"] != "line one\nline two\ttabbed \"quoted\"" {
		t.Errorf("message did not round trip: %q", out["message"])
	}
}

func TestTruncationStopsOnRuneBoundary(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(), log.WithMaxMessageBytes(4)))
	l.Info("héllo")

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
	}
	msg, _ := out["message"].(string)
	if !strings.HasPrefix(msg, "hé") {
		t.Errorf("truncated message = %q, want it to keep the whole é", msg)
	}
	if strings.ContainsRune(msg, '�') {
		t.Errorf("truncation split a rune: %q", msg)
	}
}
