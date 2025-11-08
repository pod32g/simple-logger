package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	log "github.com/pod32g/simple-logger"
)

// TestNewLogger verifies that a new logger instance is created correctly with the default formatter
func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})

	logger.Info("Info message")

	if buf.String() == "" {
		t.Errorf("Expected non-empty log output, got empty")
	}
}

// TestLogger_Debug verifies that the logger does not log debug messages if the level is higher than DEBUG
func TestLogger_Debug(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})

	logger.Debug("Debug message")

	if buf.String() != "" {
		t.Errorf("Expected no output for Debug message when level is INFO, got %v", buf.String())
	}
}

// TestLogger_Info verifies that the logger logs info messages when the level is INFO
func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})

	logger.Info("Info message")

	if !containsLogMessage(buf.String(), "INFO", "Info message") {
		t.Errorf("Expected 'INFO - Info message' in output, got %v", buf.String())
	}
}

func TestLogger_InfoStringAndInfo1(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})

	logger.InfoString("plain message")
	logger.Info1("value", 42)

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
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	logger.InfoFields("user login", log.String("user", "alice"), log.Int("attempt", 3))

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
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})

	logger.InfoFields("user login", log.String("user", "alice"), log.Int("attempt", 3))

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
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{IncludeCaller: false})

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
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	type ctxKey struct{}
	key := ctxKey{}
	logger.SetContextExtractor(func(ctx context.Context) []log.Field {
		if val, ok := ctx.Value(key).(string); ok {
			return []log.Field{log.String("span", val)}
		}
		return nil
	})

	ctx := context.WithValue(context.Background(), key, "trace-1")
	logger.InfoContext(ctx, "message")

	output := buf.String()
	if !strings.Contains(output, "span=trace-1") {
		t.Fatalf("expected span field in output, got %q", output)
	}
}

func TestLogger_ContextExtractorNilDisables(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	ctx := log.WithFields(context.Background(), log.String("request_id", "abc123"))
	logger.SetContextExtractor(nil)
	logger.InfoContext(ctx, "message")

	output := buf.String()
	if strings.Contains(output, "request_id") {
		t.Fatalf("did not expect request_id when extractor disabled, got %q", output)
	}
}

func TestLogger_SamplerDropsEntries(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetSampler(log.SamplerFunc(func(level log.LogLevel, message string, fields []log.Field) bool {
		return false
	}))

	logger.Info("should be dropped")

	if buf.Len() != 0 {
		t.Fatalf("expected sampler to drop entry, got %q", buf.String())
	}
}

func TestLogger_EveryNSampler(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetSampler(log.NewEveryNSampler(2))

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
	logger := log.NewLogger(&buf, log.WARN, &log.DefaultFormatter{})

	logger.Warn("Warn message")

	if !containsLogMessage(buf.String(), "WARN", "Warn message") {
		t.Errorf("Expected 'WARN - Warn message' in output, got %v", buf.String())
	}
}

// TestLogger_Error verifies that the logger logs error messages when the level is ERROR
func TestLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ERROR, &log.DefaultFormatter{})

	logger.Error("Error message")

	if !containsLogMessage(buf.String(), "ERROR", "Error message") {
		t.Errorf("Expected 'ERROR - Error message' in output, got %v", buf.String())
	}
}

// TestLogger_SetLevel verifies that the logger level can be changed
func TestLogger_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.WARN, &log.DefaultFormatter{})

	logger.SetLevel(log.INFO)
	logger.Info("Info message")

	if !containsLogMessage(buf.String(), "INFO", "Info message") {
		t.Errorf("Expected 'INFO - Info message' in output, got %v", buf.String())
	}
}

// TestLogger_JsonLogMessage verifies that the logger correctly logs messages in JSON format
func TestLogger_JsonLogMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{})

	logger.Info("JSON Info message")

	if !isValidJSON(buf.String()) {
		t.Errorf("Expected valid JSON log message, got %v", buf.String())
	}
}

// TestLogger_CustomFormatter verifies that the logger correctly logs messages using a custom formatter
func TestLogger_CustomFormatter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &MyCustomFormatter{})

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
	logger := log.NewLogger(&buf1, log.INFO, &log.DefaultFormatter{})
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
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetOutputs(&buf1, &buf2)
	logger.Info("multi")

	if buf1.Len() == 0 || buf2.Len() == 0 {
		t.Fatalf("expected output in both buffers, got buf1=%q buf2=%q", buf1.String(), buf2.String())
	}
}

func TestLogger_Hook(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})

	recorded := make([]string, 0)
	logger.AddHook(log.HookFunc(func(level log.LogLevel, message string, fields []log.Field) {
		recorded = append(recorded, fmt.Sprintf("%s:%s:%d", logLevelToString(level), message, len(fields)))
	}))

	logger.InfoFields("hook message", log.String("k", "v"))

	if len(recorded) != 1 {
		t.Fatalf("expected hook to fire once, got %d", len(recorded))
	}
	if recorded[0] != "INFO:hook message:1" {
		t.Fatalf("unexpected hook payload %q", recorded[0])
	}
}

func TestLogger_IncludeStacktrace(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ERROR, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetIncludeStacktrace(true)

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
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.InfoFields("encoded", log.Any("ct", customType{"foo"}))
	if !strings.Contains(buf.String(), "custom:foo") {
		t.Fatalf("expected custom encoder output, got %q", buf.String())
	}

	buf.Reset()
	logger.SetFormatter(&log.JSONFormatter{IncludeCaller: false})
	logger.InfoFields("encoded", log.Any("ct", customType{"bar"}))

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
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.EnableAsync(log.AsyncOptions{QueueSize: 8})

	logger.Info("async message")
	logger.DisableAsync()

	if !strings.Contains(buf.String(), "async message") {
		t.Fatalf("expected async message to be flushed, got %q", buf.String())
	}
}

func TestLogger_AsyncDrop(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.EnableAsync(log.AsyncOptions{QueueSize: 1, Drop: true})

	logger.Info("first")
	logger.Info("second")
	logger.DisableAsync()

	output := buf.String()
	if !strings.Contains(output, "first") {
		t.Fatalf("expected first message to be logged, got %q", output)
	}
	if strings.Contains(output, "second") {
		t.Fatalf("expected second message to be dropped, got %q", output)
	}
}

// TestLogger_SetFormatter verifies that changing the formatter changes output format
func TestLogger_SetFormatter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})
	logger.SetFormatter(&log.JSONFormatter{})
	logger.Info("json msg")
	if !isValidJSON(buf.String()) {
		t.Errorf("expected JSON formatted message, got %v", buf.String())
	}
}

func TestLogger_SetSynchronized(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})
	if !logger.Synchronized() {
		t.Fatalf("expected synchronized writes by default")
	}
	logger.SetSynchronized(false)
	if logger.Synchronized() {
		t.Fatalf("expected unsynchronized writes after disabling")
	}
}

func TestLogger_ConcurrentLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.DEBUG, &log.DefaultFormatter{IncludeCaller: false})
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
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{})
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
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{})
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

// MyCustomFormatter is a test custom formatter
type MyCustomFormatter struct{}

func (f *MyCustomFormatter) Format(level log.LogLevel, message string) string {
	return fmt.Sprintf("**CUSTOM LOG** [%s] %s\n", logLevelToString(level), message)
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
