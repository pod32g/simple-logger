package log_test

import (
	"bytes"
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
