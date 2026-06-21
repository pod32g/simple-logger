package log

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ConsoleFormatter renders human-friendly, optionally colorized output for local
// development: a dimmed timestamp, a colorized and padded level token, the
// message, and aligned key=value fields with quoting for values that contain
// spaces. For machine ingestion prefer JSONFormatter.
type ConsoleFormatter struct {
	// TimeLayout is the timestamp layout; defaults to "15:04:05.000".
	TimeLayout string
	// NoColor disables ANSI color codes (e.g. when the output is not a TTY).
	NoColor bool
	// Now overrides the timestamp source; defaults to time.Now.
	Now func() time.Time
}

const dimColor = "\033[2m"

func (f *ConsoleFormatter) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *ConsoleFormatter) layout() string {
	if f.TimeLayout != "" {
		return f.TimeLayout
	}
	return "15:04:05.000"
}

func (f *ConsoleFormatter) color(code, s string) string {
	if f.NoColor {
		return s
	}
	return code + s + colorReset
}

// Format implements Formatter.
func (f *ConsoleFormatter) Format(level LogLevel, message string) string {
	var sb strings.Builder
	f.FormatWithFieldsTo(level, message, nil, &sb)
	return sb.String()
}

// FormatTo implements WriterFormatter.
func (f *ConsoleFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	f.FormatWithFieldsTo(level, message, nil, w)
}

// FormatWithFieldsTo implements StructuredWriterFormatter.
func (f *ConsoleFormatter) FormatWithFieldsTo(level LogLevel, message string, fields []Field, w io.Writer) {
	ts := f.now().Format(f.layout())
	lvl := fmt.Sprintf("%-5s", logLevelToString(level))

	io.WriteString(w, f.color(dimColor, ts))
	io.WriteString(w, " ")
	io.WriteString(w, f.color(levelColors[level], lvl))
	if message != "" {
		io.WriteString(w, " ")
		io.WriteString(w, message)
	}
	for _, field := range fields {
		io.WriteString(w, " ")
		io.WriteString(w, f.color(dimColor, field.Key+"="))
		io.WriteString(w, consoleValue(field.Value))
	}
	io.WriteString(w, "\n")
}

func consoleValue(v interface{}) string {
	if s, ok := encodeTextWithRegistry(v); ok {
		return maybeQuote(s)
	}
	switch val := v.(type) {
	case string:
		return maybeQuote(val)
	case nil:
		return "null"
	default:
		return maybeQuote(fmt.Sprint(val))
	}
}

func maybeQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"=") {
		return strconv.Quote(s)
	}
	return s
}
