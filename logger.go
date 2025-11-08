package log

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// ArgsFormatter allows formatting log arguments directly without building an intermediate string
type ArgsFormatter interface {
	FormatArgs(level LogLevel, w io.Writer, v ...interface{})
}

// Logger represents a logging instance
type writerHolder struct {
	w io.Writer
}

type formatterHolder struct {
	f Formatter
}

type Logger struct {
	level      atomic.Int32
	output     atomic.Pointer[writerHolder]
	formatter  atomic.Pointer[formatterHolder]
	writeMu    sync.Mutex
	closer     io.Closer
	syncWrites atomic.Bool
}

// NewLogger creates a new Logger instance
func NewLogger(output io.Writer, level LogLevel, formatter Formatter) *Logger {
	if formatter == nil {
		panic("logger: formatter cannot be nil")
	}
	if output == nil {
		output = io.Discard
	}
	logger := &Logger{}
	logger.level.Store(int32(level))
	logger.output.Store(&writerHolder{w: output})
	logger.formatter.Store(&formatterHolder{f: formatter})
	logger.syncWrites.Store(true)
	return logger
}

// SetOutput changes the output destination for the logger
func (l *Logger) SetOutput(output io.Writer) {
	l.SetOutputWithCloser(output, nil)
}

// SetOutputWithCloser changes the output and associates a closer that will be
// invoked when the logger closes or when a subsequent SetOutput* call replaces
// the writer.
func (l *Logger) SetOutputWithCloser(output io.Writer, closer io.Closer) {
	if output == nil {
		output = io.Discard
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.closer != nil && l.closer != closer {
		l.closer.Close()
	}
	l.output.Store(&writerHolder{w: output})
	l.closer = closer
}

// SetLevel changes the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level.Store(int32(level))
}

// SetFormatter allows changing the log message format
func (l *Logger) SetFormatter(formatter Formatter) {
	if formatter == nil {
		panic("logger: formatter cannot be nil")
	}
	l.formatter.Store(&formatterHolder{f: formatter})
}

// SetSynchronized toggles serialized writes. When disabled, callers must ensure the
// writer they provide is safe for concurrent use.
func (l *Logger) SetSynchronized(enabled bool) {
	l.syncWrites.Store(enabled)
}

// Synchronized reports whether the logger currently serializes writes.
func (l *Logger) Synchronized() bool {
	return l.syncWrites.Load()
}

var levelStrings = [...]string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
var levelBytes = [...][]byte{
	[]byte("DEBUG"),
	[]byte("INFO"),
	[]byte("WARN"),
	[]byte("ERROR"),
	[]byte("FATAL"),
}

const packagePrefix = "github.com/pod32g/simple-logger."

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

func (f *DefaultFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.FormatTo(level, message, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func basename(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func (f *DefaultFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	var tmp [128]byte
	b := tmp[:0]
	b = appendTimestampSlice(b, time.Now())
	if f.IncludeCaller {
		file, line := resolveCaller(0)
		b = append(b, ' ', '-', ' ')
		b = append(b, file...)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(line), 10)
	}
	b = append(b, ' ', '-', ' ', '[')
	if level >= 0 && int(level) < len(levelBytes) {
		b = append(b, levelBytes[level]...)
	} else {
		b = append(b, "UNKNOWN"...)
	}
	b = append(b, ']', ' ')
	w.Write(b)
	io.WriteString(w, message)
	w.Write([]byte{'\n'})
}

// FormatArgs implements ArgsFormatter for DefaultFormatter to avoid intermediate string allocations
func (f *DefaultFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	var tmp [128]byte
	b := tmp[:0]
	b = appendTimestampSlice(b, time.Now())
	if f.IncludeCaller {
		file, line := resolveCaller(0)
		b = append(b, ' ', '-', ' ')
		b = append(b, file...)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(line), 10)
	}
	b = append(b, ' ', '-', ' ', '[')
	if level >= 0 && int(level) < len(levelBytes) {
		b = append(b, levelBytes[level]...)
	} else {
		b = append(b, "UNKNOWN"...)
	}
	b = append(b, ']', ' ')
	w.Write(b)
	for i, val := range v {
		if i > 0 {
			w.Write([]byte{' '})
		}
		switch t := val.(type) {
		case string:
			io.WriteString(w, t)
		case int:
			var ibuf [20]byte
			w.Write(strconv.AppendInt(ibuf[:0], int64(t), 10))
		case fmt.Stringer:
			io.WriteString(w, t.String())
		default:
			fmt.Fprint(w, val)
		}
	}
	w.Write([]byte{'\n'})
}

// JSONFormatter formats log messages as JSON
// JSONFormatter formats log messages as JSON. The IncludeCaller flag controls
// whether caller information is included in the output.
type JSONFormatter struct {
	IncludeCaller bool
}

func (f *JSONFormatter) formatBuffer(level LogLevel, message string, buf *bytes.Buffer) {
	buf.WriteString(`{"timestamp":"`)
	buf.WriteString(time.Now().Format(time.RFC3339))
	buf.WriteString(`","level":"`)
	buf.WriteString(logLevelToString(level))
	buf.WriteString(`","message":`)
	buf.WriteString(strconv.Quote(message))
	if f.IncludeCaller {
		file, line := resolveCaller(0)
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
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// FormatArgs implements ArgsFormatter for JSONFormatter. It joins the arguments
// into a message before delegating to FormatTo.
func (f *JSONFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	msg := buildMessage(v...)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, msg, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// log logs a message using the current formatter
func (l *Logger) log(level LogLevel, v ...interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}

	fh := l.formatter.Load()
	var formatter Formatter
	if fh != nil {
		formatter = fh.f
	}
	if formatter == nil {
		return
	}

	wh := l.output.Load()
	var writer io.Writer
	if wh != nil {
		writer = wh.w
	}
	if writer == nil {
		if level == FATAL {
			os.Exit(1)
		}
		return
	}

	write := func(fn func(io.Writer)) {
		if l.syncWrites.Load() {
			l.writeMu.Lock()
			fn(writer)
			if level == FATAL {
				l.writeMu.Unlock()
				os.Exit(1)
			}
			l.writeMu.Unlock()
		} else {
			fn(writer)
			if level == FATAL {
				os.Exit(1)
			}
		}
	}

	if af, ok := formatter.(ArgsFormatter); ok {
		write(func(w io.Writer) {
			af.FormatArgs(level, w, v...)
		})
		return
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	if wf, ok := formatter.(WriterFormatter); ok {
		message := buildMessage(v...)
		wf.FormatTo(level, message, buf)
	} else {
		message := buildMessage(v...)
		buf.WriteString(formatter.Format(level, message))
	}

	write(func(w io.Writer) {
		buf.WriteTo(w)
	})
	bufferPool.Put(buf)
}

// Close releases any resources owned by the logger, such as open files.
func (l *Logger) Close() error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.closer != nil {
		err := l.closer.Close()
		l.closer = nil
		return err
	}
	return nil
}

func resolveCaller(extraSkip int) (string, int) {
	const depth = 12
	var pcs [depth]uintptr
	n := runtime.Callers(3+extraSkip, pcs[:])
	if n == 0 {
		return "unknown", 0
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if !strings.HasPrefix(frame.Function, packagePrefix) {
			return basename(frame.File), frame.Line
		}
		if !more {
			break
		}
	}
	return "unknown", 0
}

func buildMessage(v ...interface{}) string {
	if len(v) == 1 {
		switch t := v[0].(type) {
		case string:
			return t
		case int:
			return strconv.Itoa(t)
		case fmt.Stringer:
			return t.String()
		default:
			return fmt.Sprint(t)
		}
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	for i, val := range v {
		if i > 0 {
			b.WriteByte(' ')
		}
		writeValue(b, val)
	}
	s := b.String()
	builderPool.Put(b)
	return s
}

func writeValue(b *strings.Builder, val interface{}) {
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

// Debug logs a debug message
func (l *Logger) Debug(v ...interface{}) {
	l.log(DEBUG, v...)
}

// DebugString logs a preformatted debug string without variadic allocation.
func (l *Logger) DebugString(message string) {
	l.log(DEBUG, message)
}

// Debug1 logs a debug message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Debug1(message string, value interface{}) {
	l.log(DEBUG, message, value)
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	l.log(INFO, v...)
}

// InfoString logs a preformatted info string without variadic allocation.
func (l *Logger) InfoString(message string) {
	l.log(INFO, message)
}

// Info1 logs an info message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Info1(message string, value interface{}) {
	l.log(INFO, message, value)
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	l.log(WARN, v...)
}

// WarnString logs a preformatted warning string without variadic allocation.
func (l *Logger) WarnString(message string) {
	l.log(WARN, message)
}

// Warn1 logs a warning message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Warn1(message string, value interface{}) {
	l.log(WARN, message, value)
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	l.log(ERROR, v...)
}

// ErrorString logs a preformatted error string without variadic allocation.
func (l *Logger) ErrorString(message string) {
	l.log(ERROR, message)
}

// Error1 logs an error message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Error1(message string, value interface{}) {
	l.log(ERROR, message, value)
}

// Fatal logs a fatal message and exits the application
func (l *Logger) Fatal(v ...interface{}) {
	l.log(FATAL, v...)
}

// FatalString logs a preformatted fatal string without variadic allocation.
func (l *Logger) FatalString(message string) {
	l.log(FATAL, message)
}

// Fatal1 logs a fatal message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Fatal1(message string, value interface{}) {
	l.log(FATAL, message, value)
}
