package log_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
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

func TestMain(m *testing.M) {
	m.Run()
}
