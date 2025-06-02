package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var builderPool = sync.Pool{
	New: func() interface{} {
		return new(strings.Builder)
	},
}

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

var levelStrings = [...]string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// logLevelToString converts a LogLevel to its string representation
func logLevelToString(level LogLevel) string {
	if level >= 0 && int(level) < len(levelStrings) {
		return levelStrings[level]
	}
	return "UNKNOWN"
}

// DefaultFormatter is a simple text-based log message formatter
// DefaultFormatter is a simple text-based log message formatter.
// The IncludeCaller flag controls whether file and line information
// is added to each log entry. Including caller information is
// convenient for debugging but adds overhead, so it can be disabled
// for better performance.
type DefaultFormatter struct {
	IncludeCaller bool
}

func appendTwoDigits(b *strings.Builder, val int) {
	b.WriteByte(byte('0' + val/10))
	b.WriteByte(byte('0' + val%10))
}

func appendFourDigits(b *strings.Builder, val int) {
	b.WriteByte(byte('0' + val/1000))
	b.WriteByte(byte('0' + val/100%10))
	b.WriteByte(byte('0' + val/10%10))
	b.WriteByte(byte('0' + val%10))
}

func appendTimestamp(b *strings.Builder, t time.Time) {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	appendFourDigits(b, y)
	b.WriteByte('-')
	appendTwoDigits(b, int(m))
	b.WriteByte('-')
	appendTwoDigits(b, d)
	b.WriteByte(' ')
	appendTwoDigits(b, hh)
	b.WriteByte(':')
	appendTwoDigits(b, mm)
	b.WriteByte(':')
	appendTwoDigits(b, ss)
}

func (f *DefaultFormatter) Format(level LogLevel, message string) string {
	var file string
	var line int
	if f.IncludeCaller {
		var ok bool
		_, file, line, ok = runtime.Caller(4)
		if !ok {
			file = "unknown"
			line = 0
		}
		file = filepath.Base(file)
	}

	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	appendTimestamp(b, time.Now())
	if f.IncludeCaller {
		b.WriteString(" - ")
		b.WriteString(file)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(line))
	}
	b.WriteString(" - [")
	b.WriteString(logLevelToString(level))
	b.WriteString("] ")
	b.WriteString(message)
	b.WriteByte('\n')
	s := b.String()
	builderPool.Put(b)
	return s
}

// JSONFormatter formats log messages as JSON
// JSONFormatter formats log messages as JSON. The IncludeCaller flag controls
// whether caller information is included in the output.
type JSONFormatter struct {
	IncludeCaller bool
}

func (f *JSONFormatter) Format(level LogLevel, message string) string {
	var file string
	var line int
	if f.IncludeCaller {
		var ok bool
		_, file, line, ok = runtime.Caller(4)
		if !ok {
			file = "unknown"
			line = 0
		}
		file = filepath.Base(file)
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.WriteString(`{"timestamp":"`)
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteString(`","level":"`)
	b.WriteString(logLevelToString(level))
	b.WriteString(`","message":`)
	b.WriteString(strconv.Quote(message))
	if f.IncludeCaller {
		b.WriteString(`,"file":`)
		b.WriteString(strconv.Quote(file))
		b.WriteString(`,"line":`)
		b.WriteString(strconv.Itoa(line))
	}
	b.WriteString("}\n")
	s := b.String()
	builderPool.Put(b)
	return s
}

// log logs a message using the current formatter
func (l *Logger) log(level LogLevel, v ...interface{}) {
	if level < l.level {
		return
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	fmt.Fprint(b, v...)
	message := b.String()
	builderPool.Put(b)
	formattedMessage := l.formatter.Format(level, message)
	io.WriteString(l.output, formattedMessage)

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
