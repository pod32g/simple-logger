package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestNewLogger verifies that New builds a working logger with the default encoder
func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Info("Info message")

	if buf.String() == "" {
		t.Errorf("Expected non-empty log output, got empty")
	}
}

// TestLogger_Debug verifies that the logger does not log debug messages if the level is higher than DEBUG
func TestLogger_Debug(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Debug("Debug message")

	if buf.String() != "" {
		t.Errorf("Expected no output for Debug message when level is INFO, got %v", buf.String())
	}
}

// TestLogger_Info verifies that the logger logs info messages when the level is INFO
func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Info("Info message")

	if !containsLogMessage(buf.String(), "INFO", "Info message") {
		t.Errorf("Expected 'INFO - Info message' in output, got %v", buf.String())
	}
}

func TestLogger_InfoStringAndInfo1(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Info("plain message")
	logger.Info("value", log.Any("value", 42))

	output := buf.String()
	if !containsLogMessage(output, "INFO", "plain message") {
		t.Fatalf("expected plain message, got %q", output)
	}
	if !containsLogMessage(output, "INFO", "value") || !strings.Contains(output, "42") {
		t.Fatalf("expected value message with number, got %q", output)
	}
}

func TestLogger_InfoFieldsDefault(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Info("user login", log.String("user", "alice"), log.Int("attempt", 3))

	output := buf.String()
	if !containsLogMessage(output, "INFO", "user login") {
		t.Fatalf("expected message, got %q", output)
	}
	if !strings.Contains(output, "user=alice") || !strings.Contains(output, "attempt=3") {
		t.Fatalf("expected structured fields, got %q", output)
	}
}

func TestLogger_InfoFieldsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	logger.Info("user login", log.String("user", "alice"), log.Int("attempt", 3))

	var data map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if data["message"] != "user login" {
		t.Fatalf("expected message field, got %v", data["message"])
	}
	if data["user"] != "alice" {
		t.Fatalf("expected user field, got %v", data["user"])
	}
	if attempt, ok := data["attempt"].(float64); !ok || attempt != 3 {
		t.Fatalf("expected attempt field 3, got %v", data["attempt"])
	}
}

func TestLogger_InfoContext(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	ctx := log.WithFields(context.Background(), log.String("request_id", "abc123"))
	logger.InfoContext(ctx, "ctx message", log.Bool("authenticated", true))

	var data map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if data["message"] != "ctx message" {
		t.Fatalf("expected message field, got %v", data["message"])
	}
	if data["request_id"] != "abc123" {
		t.Fatalf("expected request_id, got %v", data["request_id"])
	}
	if auth, ok := data["authenticated"].(bool); !ok || !auth {
		t.Fatalf("expected authenticated true, got %v", data["authenticated"])
	}
}

func TestLogger_CustomContextExtractor(t *testing.T) {
	var buf bytes.Buffer
	type ctxKey struct{}
	key := ctxKey{}
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithContextExtractor(func(ctx context.Context) []log.Field {
			if val, ok := ctx.Value(key).(string); ok {
				return []log.Field{log.String("span", val)}
			}
			return nil
		})))

	ctx := context.WithValue(context.Background(), key, "trace-1")
	logger.InfoContext(ctx, "message")

	output := buf.String()
	if !strings.Contains(output, "span=trace-1") {
		t.Fatalf("expected span field in output, got %q", output)
	}
}

func TestLogger_ContextExtractorNilDisables(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithContextExtractor(nil)))

	ctx := log.WithFields(context.Background(), log.String("request_id", "abc123"))
	logger.InfoContext(ctx, "message")

	output := buf.String()
	if strings.Contains(output, "request_id") {
		t.Fatalf("did not expect request_id when extractor disabled, got %q", output)
	}
}

func TestLogger_SamplerDropsEntries(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithSampler(log.SamplerFunc(func(log.LogLevel, string, []log.Field) bool { return false }))))

	logger.Info("should be dropped")

	if buf.Len() != 0 {
		t.Fatalf("expected sampler to drop entry, got %q", buf.String())
	}
}

func TestLogger_EveryNSampler(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithSampler(log.NewEveryNSampler(2))))

	logger.Info("first")
	logger.Info("second")
	logger.Info("third")

	output := buf.String()
	if !strings.Contains(output, "first") {
		t.Fatalf("expected first message to be logged, got %q", output)
	}
	if strings.Contains(output, "second") {
		t.Fatalf("did not expect second message when sampling every 2 entries, got %q", output)
	}
	if !strings.Contains(output, "third") {
		t.Fatalf("expected third message to be logged, got %q", output)
	}
}

// TestLogger_Warn verifies that the logger logs warning messages when the level is WARN
func TestLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.WARN)))

	logger.Warn("Warn message")

	if !containsLogMessage(buf.String(), "WARN", "Warn message") {
		t.Errorf("Expected 'WARN - Warn message' in output, got %v", buf.String())
	}
}

// TestLogger_Error verifies that the logger logs error messages when the level is ERROR
func TestLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.ERROR)))

	logger.Error("Error message")

	if !containsLogMessage(buf.String(), "ERROR", "Error message") {
		t.Errorf("Expected 'ERROR - Error message' in output, got %v", buf.String())
	}
}

// TestLogger_SetLevel verifies that the logger level can be changed
func TestLogger_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.WARN)))

	logger.SetLevel(log.INFO)
	logger.Info("Info message")

	if !containsLogMessage(buf.String(), "INFO", "Info message") {
		t.Errorf("Expected 'INFO - Info message' in output, got %v", buf.String())
	}
}

// TestLogger_JsonLogMessage verifies that the logger correctly logs messages in JSON format
func TestLogger_JsonLogMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	logger.Info("JSON Info message")

	if !isValidJSON(buf.String()) {
		t.Errorf("Expected valid JSON log message, got %v", buf.String())
	}
}

// TestLogger_CustomFormatter verifies that the logger correctly logs messages using a custom formatter
func TestLogger_CustomFormatter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithEncoder(&MyCustomEncoder{})))

	logger.Info("Custom Info message")

	expected := "**CUSTOM LOG** [INFO] Custom Info message\n"
	if buf.String() != expected {
		t.Errorf("Expected '%v', got '%v'", expected, buf.String())
	}
}

// TestLogger_SetOutput verifies that changing the output writer works
func TestLogger_SetOutput(t *testing.T) {
	var buf1 bytes.Buffer
	var buf2 bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf1), log.WithLevel(log.INFO)))
	logger.SetOutput(&buf2)
	logger.Info("changed")
	if buf1.Len() != 0 {
		t.Errorf("expected no output on original writer, got %v", buf1.String())
	}
	if buf2.Len() == 0 {
		t.Errorf("expected output on new writer")
	}
}

func TestLogger_SetOutputs(t *testing.T) {
	var buf1 bytes.Buffer
	var buf2 bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO), log.WithOutputs(&buf1, &buf2)))
	logger.Info("multi")

	if buf1.Len() == 0 || buf2.Len() == 0 {
		t.Fatalf("expected output in both buffers, got buf1=%q buf2=%q", buf1.String(), buf2.String())
	}
}

func TestLogger_Hook(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	recorded := make([]string, 0)
	logger.AddHook(log.HookFunc(func(level log.LogLevel, message string, fields []log.Field) {
		recorded = append(recorded, fmt.Sprintf("%s:%s:%d", logLevelToString(level), message, len(fields)))
	}))

	logger.Info("hook message", log.String("k", "v"))

	if len(recorded) != 1 {
		t.Fatalf("expected hook to fire once, got %d", len(recorded))
	}
	if recorded[0] != "INFO:hook message:1" {
		t.Fatalf("unexpected hook payload %q", recorded[0])
	}
}

func TestLogger_IncludeStacktrace(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.ERROR), log.WithStacktrace()))

	logger.Error("stack please")

	output := buf.String()
	if !strings.Contains(output, "stacktrace") {
		t.Fatalf("expected stacktrace in output, got %q", output)
	}
}

func TestRegisterFieldEncoder(t *testing.T) {
	type customType struct{ Value string }

	log.RegisterFieldEncoder[customType](
		func(c customType) (string, bool) { return "custom:" + c.Value, true },
		func(c customType) (interface{}, bool) { return map[string]string{"value": c.Value}, true },
	)

	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	logger.Info("encoded", log.Any("ct", customType{"foo"}))
	if !strings.Contains(buf.String(), "custom:foo") {
		t.Fatalf("expected custom encoder output, got %q", buf.String())
	}

	// The same registered encoder must apply under a different output encoding.
	buf.Reset()
	jsonLogger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))
	jsonLogger.Info("encoded", log.Any("ct", customType{"bar"}))

	var payload map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON payload: %v", err)
	}
	value, ok := payload["ct"].(map[string]interface{})
	if !ok || value["value"] != "bar" {
		t.Fatalf("expected JSON custom encoder output, got %v", payload["ct"])
	}
}

func TestLogger_AsyncLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithAsyncQueue(8)))

	logger.Info("async message")
	logger.Close()

	if !strings.Contains(buf.String(), "async message") {
		t.Fatalf("expected async message to be flushed, got %q", buf.String())
	}
}

func TestLogger_AsyncDrop(t *testing.T) {
	// Gate the worker on its first write so the queue state is deterministic
	// rather than racing the worker (which made this test flaky).
	g := newGatedWriter()
	logger := log.Must(log.New(log.WithOutput(g), log.WithLevel(log.INFO), log.WithAsyncQueue(1)))

	logger.Info("first") // worker pulls this and parks inside Write
	<-g.started          // queue is now empty, worker parked

	logger.Info("second") // sits in the queue
	logger.Info("third")  // queue full -> DropNew drops the newest

	if dropped := logger.AsyncStats().Dropped; dropped != 1 {
		t.Fatalf("expected exactly 1 drop, got %d", dropped)
	}

	close(g.release)
	logger.Close()

	output := g.String()
	if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Fatalf("expected first and second messages logged, got %q", output)
	}
	if strings.Contains(output, "third") {
		t.Fatalf("expected third message to be dropped (DropNew), got %q", output)
	}
}

func TestLogger_AsyncBatchingFlushesOnBatchSize(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithAsyncQueue(8), log.WithAsyncBatch(3, 0)))

	logger.Info("batch-one")
	logger.Info("batch-two")
	logger.Info("batch-three")

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		out := buf.String()
		if strings.Contains(out, "batch-one") && strings.Contains(out, "batch-two") && strings.Contains(out, "batch-three") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	logger.Close()
	out := buf.String()
	if !strings.Contains(out, "batch-one") || !strings.Contains(out, "batch-two") || !strings.Contains(out, "batch-three") {
		t.Fatalf("expected batched messages to flush, got %q", out)
	}
}

func TestLogger_AsyncFlushInterval(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithAsyncQueue(4), log.WithAsyncBatch(5, 15*time.Millisecond)))

	logger.Info("interval-message")

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "interval-message") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !strings.Contains(buf.String(), "interval-message") {
		logger.Close()
		t.Fatalf("expected message flushed by interval, got %q", buf.String())
	}

	logger.Close()
}

// TestLogger_EncoderOption verifies that WithEncoder selects the encoding.
func TestLogger_EncoderOption(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithEncoder(&log.JSONEncoder{})))
	logger.Info("json msg")
	if !isValidJSON(buf.String()) {
		t.Errorf("expected JSON formatted message, got %v", buf.String())
	}
}

// An unsynchronized logger still writes; it just does not serialise writers.
func TestLogger_Unsynchronized(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithUnsynchronized()))
	logger.Info("unsynchronized line")
	if !strings.Contains(buf.String(), "unsynchronized line") {
		t.Errorf("expected the entry to be written, got %q", buf.String())
	}
}

func TestLogger_ConcurrentLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	const goroutines = 8
	const perGoroutine = 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				logger.Info(fmt.Sprintf("message-%d-%d", g, i))
			}
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	expectedCount := goroutines * perGoroutine
	if len(lines) != expectedCount {
		t.Fatalf("expected %d log lines, got %d", expectedCount, len(lines))
	}

	seen := make(map[string]bool, expectedCount)
	for _, line := range lines {
		parts := strings.SplitN(line, "] ", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected log format: %q", line)
		}
		msg := parts[1]
		if seen[msg] {
			t.Fatalf("duplicate message detected: %q", msg)
		}
		seen[msg] = true
	}

	for g := 0; g < goroutines; g++ {
		for i := 0; i < perGoroutine; i++ {
			msg := fmt.Sprintf("message-%d-%d", g, i)
			if !seen[msg] {
				t.Fatalf("missing log message %q", msg)
			}
		}
	}
}

func TestLoggerCloseReleasesCloser(t *testing.T) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO)))
	cw := &closingBuffer{}
	logger.SetOutputWithCloser(cw, cw)
	if err := logger.Close(); err != nil {
		t.Fatalf("unexpected error closing logger: %v", err)
	}
	if !cw.closed {
		t.Fatalf("expected closer to be closed")
	}
}

func TestLoggerSetOutputReplacesCloser(t *testing.T) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO)))
	cw := &closingBuffer{}
	logger.SetOutputWithCloser(cw, cw)
	logger.SetOutput(io.Discard)
	if !cw.closed {
		t.Fatalf("expected previous closer to be closed when output changes")
	}
}

// TestLoggerLoadConfigFromEnv verifies that configuration is correctly loaded from
// environment variables for logger tests
func TestLoggerLoadConfigFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("LOG_OUTPUT", "stderr")
	t.Setenv("LOG_FORMAT", "JSON")
	t.Setenv("LOG_ENABLE_CALLER", "false")

	cfg := log.LoadConfigFromEnv()

	if cfg.Level != log.DEBUG {
		t.Errorf("expected level DEBUG, got %v", cfg.Level)
	}
	if cfg.Output != "stderr" {
		t.Errorf("expected output stderr, got %q", cfg.Output)
	}
	if cfg.Format != "json" {
		t.Errorf("expected format json, got %q", cfg.Format)
	}
	if cfg.EnableCaller != false {
		t.Errorf("expected EnableCaller false, got %v", cfg.EnableCaller)
	}
}

// MyCustomEncoder is a test encoder.
type MyCustomEncoder struct{}

func (f *MyCustomEncoder) Encode(buf []byte, e log.Entry) []byte {
	return fmt.Appendf(buf, "**CUSTOM LOG** [%s] %s\n", e.Level, e.Message)
}

// Helper function to check if the output contains the expected log message
func containsLogMessage(output, level, message string) bool {
	return strings.Contains(output, level) && strings.Contains(output, message)
}

// Helper function to validate if a string is a valid JSON
func isValidJSON(s string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

func logLevelToString(level log.LogLevel) string {
	switch level {
	case log.DEBUG:
		return "DEBUG"
	case log.INFO:
		return "INFO"
	case log.WARN:
		return "WARN"
	case log.ERROR:
		return "ERROR"
	case log.FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

type closingBuffer struct {
	bytes.Buffer
	closed bool
}

func (c *closingBuffer) Close() error {
	c.closed = true
	return nil
}

func TestMain(m *testing.M) {
	m.Run()
}

// --- merged from printf_test.go ---

func TestFormattedMethods(t *testing.T) {
	cases := []struct {
		name  string
		emit  func(*log.Logger)
		level string
	}{
		{"Debugf", func(l *log.Logger) { l.Debugf("n=%d s=%s", 3, "x") }, "DEBUG"},
		{"Infof", func(l *log.Logger) { l.Infof("n=%d s=%s", 3, "x") }, "INFO"},
		{"Warnf", func(l *log.Logger) { l.Warnf("n=%d s=%s", 3, "x") }, "WARN"},
		{"Errorf", func(l *log.Logger) { l.Errorf("n=%d s=%s", 3, "x") }, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
			tc.emit(l)
			got := buf.String()
			if !strings.Contains(got, "n=3 s=x") {
				t.Errorf("message not formatted: %s", got)
			}
			if !strings.Contains(got, "["+tc.level+"]") {
				t.Errorf("wrong level, want %s: %s", tc.level, got)
			}
		})
	}
}

// The reason these methods exist: nothing is formatted when the entry is going
// to be dropped on level.
func TestFormattedMethodsSkipFormattingWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.ERROR)))

	formatted := 0
	arg := stringerFunc(func() string { formatted++; return "expensive" })

	l.Debugf("value=%s", arg)
	l.Infof("value=%s", arg)
	l.Warnf("value=%s", arg)
	if formatted != 0 {
		t.Errorf("argument was formatted %d times below the level threshold", formatted)
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected output: %s", buf.String())
	}

	l.Errorf("value=%s", arg)
	if formatted != 1 {
		t.Errorf("argument formatted %d times at ERROR, want 1", formatted)
	}
	if !strings.Contains(buf.String(), "value=expensive") {
		t.Errorf("entry missing: %s", buf.String())
	}
}

// Formatted messages take the same path as any other message: bound fields,
// redaction and hooks all still apply.
func TestFormattedMessagesGoThroughTheNormalPath(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(),
		log.WithRedactor(log.NewPatternScrubber(regexp.MustCompile(`secret-\w+`))))).
		With(log.String("component", "api"))

	var hooked string
	l.AddHook(log.HookFunc(func(_ log.LogLevel, msg string, _ []log.Field) { hooked = msg }))

	l.Infof("token %s rejected", "secret-abc")

	got := buf.String()
	if !strings.Contains(got, "[REDACTED]") || strings.Contains(got, "secret-abc") {
		t.Errorf("redaction did not apply to a formatted message: %s", got)
	}
	if !strings.Contains(got, `"component":"api"`) {
		t.Errorf("bound fields missing: %s", got)
	}
	if !strings.Contains(hooked, "[REDACTED]") {
		t.Errorf("hook saw %q, want the redacted message", hooked)
	}
}

type stringerFunc func() string

func (f stringerFunc) String() string { return f() }

func TestFatalfExitsAfterLogging(t *testing.T) {
	if os.Getenv("FATALF_CHILD") == "1" {
		l := log.Must(log.New(log.WithOutput(os.Stdout), log.WithLevel(log.INFO)))
		l.Fatalf("stopping after %d retries", 3)
		l.Info("unreachable")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestFatalfExitsAfterLogging")
	cmd.Env = append(os.Environ(), "FATALF_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected a non-zero exit status")
	}
	got := string(out)
	if !strings.Contains(got, "stopping after 3 retries") {
		t.Errorf("fatal message missing or unformatted: %s", got)
	}
	if strings.Contains(got, "unreachable") {
		t.Errorf("execution continued past Fatalf: %s", got)
	}
}

// --- merged from output_lifecycle_test.go ---

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
			l := log.Must(log.New(log.WithOutput(first), log.WithLevel(log.INFO)))
			l.SetOutputWithCloser(first, first)

			var writers []*countingCloser
			for i := 0; i < 5; i++ {
				next := &countingCloser{}
				writers = append(writers, next)
				l.SetOutputWithCloser(next, next)
				l.Info("after swap")
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
	l := log.Must(log.New(log.WithOutput(&countingCloser{}), log.WithLevel(log.INFO), log.WithUnsynchronized()))

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
					l.Info("concurrent")
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
