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

// Logger represents a logging instance
type Logger struct {
	level      LogLevel
	output     io.Writer
	LogMessage func(level LogLevel, message string)
}

// NewLogger creates a new Logger instance
func NewLogger(output io.Writer, level LogLevel) *Logger {
	return &Logger{
		level:  level,
		output: output,
		LogMessage: func(level LogLevel, message string) {
			// Default log message behavior (plain text)
			defaultLogMessage(output, level, message)
		},
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

// defaultLogMessage logs a message in plain text format if the level is appropriate
func defaultLogMessage(output io.Writer, level LogLevel, message string) {
	_, file, line, ok := runtime.Caller(3) // Adjusting caller depth for accurate file and line info
	if !ok {
		file = "unknown"
		line = 0
	}
	file = filepath.Base(file)
	now := time.Now().Format("2006-01-02 15:04:05")

	logMsg := fmt.Sprintf("%s - %s:%d - [%s] %s\n", now, file, line, logLevelToString(level), message)
	fmt.Fprint(output, logMsg)

	if level == FATAL {
		os.Exit(1)
	}
}

func (l *Logger) JsonLogMessage(level LogLevel, message string) {
	if level < l.level {
		return
	}

	_, file, line, ok := runtime.Caller(2)
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

	// Marshal the log entry into JSON
	jsonLog, err := json.Marshal(logEntry)
	if err != nil {
		// Fallback to plain text logging in case of JSON marshalling error
		defaultLogMessage(l.output, level, fmt.Sprintf("Error formatting JSON log: %v, original message: %s", err, message))
		return
	}

	// Write the JSON log entry directly to the output
	fmt.Fprintln(l.output, string(jsonLog))

	if level == FATAL {
		os.Exit(1)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(v ...interface{}) {
	if l.level <= DEBUG {
		l.LogMessage(DEBUG, fmt.Sprint(v...))
	}
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	if l.level <= INFO {
		l.LogMessage(INFO, fmt.Sprint(v...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	if l.level <= WARN {
		l.LogMessage(WARN, fmt.Sprint(v...))
	}
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	if l.level <= ERROR {
		l.LogMessage(ERROR, fmt.Sprint(v...))
	}
}

// Fatal logs a fatal message and exits the application
func (l *Logger) Fatal(v ...interface{}) {
	if l.level <= FATAL {
		l.LogMessage(FATAL, fmt.Sprint(v...))
	}
}
