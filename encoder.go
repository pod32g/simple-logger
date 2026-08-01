package log

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"
)

// Entry is one log record, handed to an Encoder.
type Entry struct {
	Level   LogLevel
	Message string
	Fields  []Field
	Time    time.Time
	// Caller is the resolved call site. It is only populated when the logger was
	// built WithCaller; File is empty otherwise.
	Caller Caller

	// codecs are the logger's own type encoders. It is unexported because a
	// custom encoder renders values itself and has no use for them.
	codecs *fieldCodecs
}

// Encoder renders an entry. It appends to buf and returns the extended slice,
// in the manner of the append builtin, so the logger can reuse one buffer
// across entries rather than allocating per call.
//
// One interface, one method: everything an entry carries is on Entry. Earlier
// versions of this package split the job across eight interfaces and picked
// between them with a chain of type assertions, which meant the contract was
// whatever the dispatch order happened to be.
type Encoder interface {
	Encode(buf []byte, e Entry) []byte
}

// Caller is a resolved source location.
type Caller struct {
	File string
	Line int
}

// Zero reports whether no call site was captured.
func (c Caller) Zero() bool { return c.File == "" }

// --- text -------------------------------------------------------------------

// TextEncoder renders "2006-01-02 15:04:05 - [INFO] message key=value", the
// default. Colorize adds ANSI color to the level; TimeLayout overrides the
// timestamp format.
type TextEncoder struct {
	Colorize   bool
	TimeLayout string
}

func (e *TextEncoder) Encode(buf []byte, entry Entry) []byte {
	if e.TimeLayout != "" {
		buf = entry.Time.AppendFormat(buf, e.TimeLayout)
	} else {
		buf = appendTimestamp(buf, entry.Time)
	}
	if !entry.Caller.Zero() {
		buf = append(buf, ' ', '-', ' ')
		buf = append(buf, entry.Caller.File...)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(entry.Caller.Line), 10)
	}
	buf = append(buf, ' ', '-', ' ', '[')
	color := ""
	if e.Colorize {
		color = levelColors[entry.Level]
		buf = append(buf, color...)
	}
	buf = appendLevel(buf, entry.Level)
	if color != "" {
		buf = append(buf, colorReset...)
	}
	buf = append(buf, ']', ' ')
	buf = appendTextSafe(buf, entry.Message)
	buf = appendFieldsText(buf, entry.Fields, entry.Message != "", "", entry.codecs)
	return append(buf, '\n')
}

// --- JSON -------------------------------------------------------------------

// JSONEncoder renders entries as one JSON object per line.
type JSONEncoder struct {
	TimeLayout string
}

func (e *JSONEncoder) Encode(buf []byte, entry Entry) []byte {
	buf = append(buf, `{"timestamp":"`...)
	if e.TimeLayout != "" {
		buf = entry.Time.AppendFormat(buf, e.TimeLayout)
	} else {
		buf = entry.Time.AppendFormat(buf, time.RFC3339)
	}
	buf = append(buf, `","level":"`...)
	buf = appendLevel(buf, entry.Level)
	buf = append(buf, `","message":`...)
	buf = appendJSONStringTo(buf, entry.Message)
	if !entry.Caller.Zero() {
		buf = append(buf, `,"file":`...)
		buf = appendJSONStringTo(buf, entry.Caller.File)
		buf = append(buf, `,"line":`...)
		buf = strconv.AppendInt(buf, int64(entry.Caller.Line), 10)
	}
	buf = appendJSONFieldsTo(buf, entry.Fields, entry.codecs)
	return append(buf, '}', '\n')
}

// --- console ----------------------------------------------------------------

// ConsoleEncoder renders human-friendly output for a terminal: a dimmed
// timestamp, a padded and colored level, then aligned key=value pairs.
type ConsoleEncoder struct {
	TimeLayout string
	NoColor    bool
}

func (e *ConsoleEncoder) Encode(buf []byte, entry Entry) []byte {
	layout := e.TimeLayout
	if layout == "" {
		layout = "15:04:05.000"
	}
	if e.NoColor {
		buf = entry.Time.AppendFormat(buf, layout)
	} else {
		buf = append(buf, dimColor...)
		buf = entry.Time.AppendFormat(buf, layout)
		buf = append(buf, colorReset...)
	}
	buf = append(buf, ' ')

	name := entry.Level.String()
	if e.NoColor {
		buf = append(buf, name...)
	} else {
		buf = append(buf, levelColors[entry.Level]...)
		buf = append(buf, name...)
		buf = append(buf, colorReset...)
	}
	for i := len(name); i < 5; i++ {
		buf = append(buf, ' ')
	}

	if !entry.Caller.Zero() {
		buf = append(buf, ' ')
		buf = append(buf, entry.Caller.File...)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(entry.Caller.Line), 10)
	}
	if entry.Message != "" {
		buf = append(buf, ' ')
		buf = appendTextSafe(buf, entry.Message)
	}
	buf = appendFieldsConsole(buf, entry.Fields, e.NoColor, "", entry.codecs)
	return append(buf, '\n')
}

// --- shared helpers ---------------------------------------------------------

func appendLevel(buf []byte, level LogLevel) []byte {
	if level >= 0 && int(level) < len(levelBytes) {
		return append(buf, levelBytes[level]...)
	}
	return append(buf, "UNKNOWN"...)
}

func appendTimestamp(buf []byte, t time.Time) []byte {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	buf = appendFourDigits(buf, y)
	buf = append(buf, '-')
	buf = appendTwoDigits(buf, int(m))
	buf = append(buf, '-')
	buf = appendTwoDigits(buf, d)
	buf = append(buf, ' ')
	buf = appendTwoDigits(buf, hh)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, mm)
	buf = append(buf, ':')
	return appendTwoDigits(buf, ss)
}

func appendTwoDigits(b []byte, val int) []byte {
	return append(b, byte('0'+val/10), byte('0'+val%10))
}

func appendFourDigits(b []byte, val int) []byte {
	return append(b, byte('0'+val/1000), byte('0'+val/100%10), byte('0'+val/10%10), byte('0'+val%10))
}

// appendTextSafe writes s, quoting it when it holds a control character. A raw
// newline would end the record and let whatever follows read as a genuine entry.
func appendTextSafe(buf []byte, s string) []byte {
	if !containsControl(s) {
		return append(buf, s...)
	}
	return strconv.AppendQuote(buf, s)
}

// appendFieldsText renders fields as key=value, flattening groups with a dotted
// prefix so line-oriented output stays one line.
func appendFieldsText(buf []byte, fields []Field, spaceFirst bool, prefix string, codecs *fieldCodecs) []byte {
	for i, f := range fields {
		if group, ok := f.Value.([]Field); ok {
			buf = appendFieldsText(buf, group, spaceFirst || i > 0, joinKey(prefix, f.Key), codecs)
			continue
		}
		if spaceFirst || i > 0 {
			buf = append(buf, ' ')
		}
		buf = append(buf, joinKey(prefix, f.Key)...)
		buf = append(buf, '=')
		buf = appendValueText(buf, f.Value, codecs)
	}
	return buf
}

func appendFieldsConsole(buf []byte, fields []Field, noColor bool, prefix string, codecs *fieldCodecs) []byte {
	for _, f := range fields {
		if group, ok := f.Value.([]Field); ok {
			buf = appendFieldsConsole(buf, group, noColor, joinKey(prefix, f.Key), codecs)
			continue
		}
		buf = append(buf, ' ')
		if !noColor {
			buf = append(buf, dimColor...)
		}
		buf = append(buf, joinKey(prefix, f.Key)...)
		buf = append(buf, '=')
		if !noColor {
			buf = append(buf, colorReset...)
		}
		buf = appendConsoleValue(buf, f.Value, codecs)
	}
	return buf
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	if key == "" {
		return prefix
	}
	return prefix + "." + key
}

func appendValueText(buf []byte, val interface{}, codecs *fieldCodecs) []byte {
	if s, ok := codecs.text(val); ok {
		return appendTextSafe(buf, s)
	}
	switch v := val.(type) {
	case string:
		return appendTextSafe(buf, v)
	case int:
		return strconv.AppendInt(buf, int64(v), 10)
	case int64:
		return strconv.AppendInt(buf, v, 10)
	case uint:
		return strconv.AppendUint(buf, uint64(v), 10)
	case uint64:
		return strconv.AppendUint(buf, v, 10)
	case float64:
		return strconv.AppendFloat(buf, v, 'f', -1, 64)
	case float32:
		return strconv.AppendFloat(buf, float64(v), 'f', -1, 32)
	case bool:
		return strconv.AppendBool(buf, v)
	case nil:
		return append(buf, "null"...)
	case fmt.Stringer:
		return appendTextSafe(buf, v.String())
	case error:
		return appendTextSafe(buf, v.Error())
	default:
		return appendTextSafe(buf, fmt.Sprint(v))
	}
}

func appendConsoleValue(buf []byte, val interface{}, codecs *fieldCodecs) []byte {
	if s, ok := codecs.text(val); ok {
		return appendMaybeQuoted(buf, s)
	}
	switch v := val.(type) {
	case string:
		return appendMaybeQuoted(buf, v)
	case nil:
		return append(buf, "null"...)
	default:
		return appendMaybeQuoted(buf, fmt.Sprint(val))
	}
}

func appendMaybeQuoted(buf []byte, s string) []byte {
	if s == "" || containsAny(s, " \t\n\"=") {
		return strconv.AppendQuote(buf, s)
	}
	return append(buf, s...)
}

func containsAny(s, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

func appendJSONFieldsTo(buf []byte, fields []Field, codecs *fieldCodecs) []byte {
	for _, f := range fields {
		buf = append(buf, ',')
		buf = appendJSONStringTo(buf, f.Key)
		buf = append(buf, ':')
		buf = appendJSONValueTo(buf, f.Value, codecs)
	}
	return buf
}

func appendJSONValueTo(buf []byte, val interface{}, codecs *fieldCodecs) []byte {
	if group, ok := val.([]Field); ok {
		buf = append(buf, '{')
		for i, f := range group {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSONStringTo(buf, f.Key)
			buf = append(buf, ':')
			buf = appendJSONValueTo(buf, f.Value, codecs)
		}
		return append(buf, '}')
	}
	if encoded, ok := codecs.json(val); ok {
		val = encoded
	}
	switch v := val.(type) {
	case string:
		return appendJSONStringTo(buf, v)
	case int:
		return strconv.AppendInt(buf, int64(v), 10)
	case int64:
		return strconv.AppendInt(buf, v, 10)
	case uint:
		return strconv.AppendUint(buf, uint64(v), 10)
	case uint64:
		return strconv.AppendUint(buf, v, 10)
	case float64:
		return strconv.AppendFloat(buf, v, 'f', -1, 64)
	case float32:
		return strconv.AppendFloat(buf, float64(v), 'f', -1, 32)
	case bool:
		return strconv.AppendBool(buf, v)
	case nil:
		return append(buf, "null"...)
	case []byte:
		return appendJSONStringTo(buf, string(v))
	case fmt.Stringer:
		return appendJSONStringTo(buf, v.String())
	case error:
		return appendJSONStringTo(buf, v.Error())
	}
	if data, err := json.Marshal(val); err == nil {
		return append(buf, data...)
	}
	return appendJSONStringTo(buf, fmt.Sprint(val))
}

// appendJSONStringTo writes s as a JSON string, quotes included.
//
// strconv.Quote is not usable here: it produces Go literal syntax, so invalid
// UTF-8 comes out as \xNN and non-printable runes above the BMP as \U0001d173,
// neither of which is a JSON escape.
func appendJSONStringTo(buf []byte, s string) []byte {
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= ' ' && b != '"' && b != '\\' {
				i++
				continue
			}
			buf = append(buf, s[start:i]...)
			switch b {
			case '"':
				buf = append(buf, '\\', '"')
			case '\\':
				buf = append(buf, '\\', '\\')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		if r, size := utf8.DecodeRuneInString(s[i:]); r == utf8.RuneError && size == 1 {
			buf = append(buf, s[start:i]...)
			buf = append(buf, "�"...)
			i++
			start = i
		} else {
			i += size
		}
	}
	buf = append(buf, s[start:]...)
	return append(buf, '"')
}
