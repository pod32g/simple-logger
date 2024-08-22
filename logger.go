package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LogLevel represents the severity of the log message
type LogLevel int

// Log levels
const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Formatter defines an interface for formatting log messages
type Formatter interface {
	Format(level LogLevel, message string) string
}

// Logger represents a logging instance
type Logger struct {
	level     LogLevel
	output    io.Writer
	formatter Formatter
}

// NewLogger creates a new Logger instance
func NewLogger(output io.Writer, level LogLevel, formatter Formatter) *Logger {
	return &Logger{
		level:     level,
		output:    output,
		formatter: formatter,
	}
}

// SetOutput changes the output destination for the logger
func (l *Logger) SetOutput(output io.Writer) {
	l.output = output
}

// SetLevel changes the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// SetFormatter allows changing the log message format
func (l *Logger) SetFormatter(formatter Formatter) {
	l.formatter = formatter
}

// logLevelToString converts a LogLevel to its string representation
func logLevelToString(level LogLevel) string {
	switch level {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// DefaultFormatter is a simple text-based log message formatter
type DefaultFormatter struct{}

func (f *DefaultFormatter) Format(level LogLevel, message string) string {
	_, file, line, ok := runtime.Caller(4)
	if !ok {
		file = "unknown"
		line = 0
	}
	file = filepath.Base(file)
	now := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%s - %s:%d - [%s] %s\n", now, file, line, logLevelToString(level), message)
}

// JSONFormatter formats log messages as JSON
type JSONFormatter struct{}

func (f *JSONFormatter) Format(level LogLevel, message string) string {
	_, file, line, ok := runtime.Caller(4)
	if !ok {
		file = "unknown"
		line = 0
	}
	file = filepath.Base(file)
	now := time.Now().Format(time.RFC3339)
	logEntry := map[string]interface{}{
		"timestamp": now,
		"level":     logLevelToString(level),
		"file":      file,
		"line":      line,
		"message":   message,
	}
	jsonLog, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to format log message", "message": "%s"}`, message)
	}
	return string(jsonLog)
}

// log logs a message using the current formatter
func (l *Logger) log(level LogLevel, v ...interface{}) {
	if level < l.level {
		return
	}
	message := fmt.Sprint(v...)
	formattedMessage := l.formatter.Format(level, message)
	fmt.Fprint(l.output, formattedMessage)

	if level == FATAL {
		os.Exit(1)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(v ...interface{}) {
	l.log(DEBUG, v...)
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	l.log(INFO, v...)
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	l.log(WARN, v...)
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	l.log(ERROR, v...)
}

// Fatal logs a fatal message and exits the application
func (l *Logger) Fatal(v ...interface{}) {
	l.log(FATAL, v...)
}
