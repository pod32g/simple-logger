package log

import (
	"bytes"
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

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
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

// WriterFormatter allows writing formatted output directly to an io.Writer
type WriterFormatter interface {
	FormatTo(level LogLevel, message string, w io.Writer)
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

func appendTwoDigitsBuf(b *bytes.Buffer, val int) {
	b.WriteByte(byte('0' + val/10))
	b.WriteByte(byte('0' + val%10))
}

func appendFourDigitsBuf(b *bytes.Buffer, val int) {
	b.WriteByte(byte('0' + val/1000))
	b.WriteByte(byte('0' + val/100%10))
	b.WriteByte(byte('0' + val/10%10))
	b.WriteByte(byte('0' + val%10))
}

func appendTimestampBuf(b *bytes.Buffer, t time.Time) {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	appendFourDigitsBuf(b, y)
	b.WriteByte('-')
	appendTwoDigitsBuf(b, int(m))
	b.WriteByte('-')
	appendTwoDigitsBuf(b, d)
	b.WriteByte(' ')
	appendTwoDigitsBuf(b, hh)
	b.WriteByte(':')
	appendTwoDigitsBuf(b, mm)
	b.WriteByte(':')
	appendTwoDigitsBuf(b, ss)
}

func appendTwoDigitsSlice(b []byte, val int) []byte {
	return append(b, byte('0'+val/10), byte('0'+val%10))
}

func appendFourDigitsSlice(b []byte, val int) []byte {
	return append(b,
		byte('0'+val/1000),
		byte('0'+val/100%10),
		byte('0'+val/10%10),
		byte('0'+val%10))
}

func appendTimestampSlice(b []byte, t time.Time) []byte {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	b = appendFourDigitsSlice(b, y)
	b = append(b, '-')
	b = appendTwoDigitsSlice(b, int(m))
	b = append(b, '-')
	b = appendTwoDigitsSlice(b, d)
	b = append(b, ' ')
	b = appendTwoDigitsSlice(b, hh)
	b = append(b, ':')
	b = appendTwoDigitsSlice(b, mm)
	b = append(b, ':')
	b = appendTwoDigitsSlice(b, ss)
	return b
}

func (f *DefaultFormatter) formatBuffer(level LogLevel, message string, buf *bytes.Buffer) {
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

	appendTimestampBuf(buf, time.Now())
	if f.IncludeCaller {
		buf.WriteString(" - ")
		buf.WriteString(file)
		buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(line))
	}
	buf.WriteString(" - [")
	buf.WriteString(logLevelToString(level))
	buf.WriteString("] ")
	buf.WriteString(message)
	buf.WriteByte('\n')
}

func (f *DefaultFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *DefaultFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	var tmp [64]byte
	b := tmp[:0]
	b = appendTimestampSlice(b, time.Now())
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
		b = append(b, ' ', '-', ' ')
		b = append(b, file...)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(line), 10)
	}
	b = append(b, ' ', '-', ' ', '[')
	b = append(b, logLevelToString(level)...)
	b = append(b, ']', ' ')
	w.Write(b)
	io.WriteString(w, message)
	w.Write([]byte{'\n'})
}

// JSONFormatter formats log messages as JSON
// JSONFormatter formats log messages as JSON. The IncludeCaller flag controls
// whether caller information is included in the output.
type JSONFormatter struct {
	IncludeCaller bool
}

func (f *JSONFormatter) formatBuffer(level LogLevel, message string, buf *bytes.Buffer) {
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
	buf.WriteString(`{"timestamp":"`)
	buf.WriteString(time.Now().Format(time.RFC3339))
	buf.WriteString(`","level":"`)
	buf.WriteString(logLevelToString(level))
	buf.WriteString(`","message":`)
	buf.WriteString(strconv.Quote(message))
	if f.IncludeCaller {
		buf.WriteString(`,"file":`)
		buf.WriteString(strconv.Quote(file))
		buf.WriteString(`,"line":`)
		buf.WriteString(strconv.Itoa(line))
	}
	buf.WriteString("}\n")
}

func (f *JSONFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *JSONFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	var tmp bytes.Buffer
	f.formatBuffer(level, message, &tmp)
	w.Write(tmp.Bytes())
}

// log logs a message using the current formatter
func (l *Logger) log(level LogLevel, v ...interface{}) {
	if level < l.level {
		return
	}
	var message string
	if len(v) == 1 {
		switch t := v[0].(type) {
		case string:
			message = t
		case int:
			message = strconv.Itoa(t)
		case fmt.Stringer:
			message = t.String()
		default:
			message = fmt.Sprint(t)
		}
	} else {
		b := builderPool.Get().(*strings.Builder)
		b.Reset()
		for i, val := range v {
			if i > 0 {
				b.WriteByte(' ')
			}
			switch t := val.(type) {
			case string:
				b.WriteString(t)
			case int:
				b.WriteString(strconv.Itoa(t))
			case fmt.Stringer:
				b.WriteString(t.String())
			default:
				fmt.Fprint(b, val)
			}
		}
		message = b.String()
		builderPool.Put(b)
	}
	if wf, ok := l.formatter.(WriterFormatter); ok {
		wf.FormatTo(level, message, l.output)
	} else {
		formattedMessage := l.formatter.Format(level, message)
		io.WriteString(l.output, formattedMessage)
	}

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
