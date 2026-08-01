package log_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

var updateGolden = flag.Bool("update", false, "rewrite the encoder golden files")

// The exact output of the encoders is a documented interface: the README shows
// it, and anything parsing these logs depends on it. These tests pin it.
//
// They exist because a change to timestamp handling silently dropped the
// separator between the call site and the level -- "09:41:22 - main.go:42
// [INFO]" instead of "09:41:22 - main.go:42 - [INFO]" -- and the whole suite
// stayed green, because every other test asserted that fields and levels
// appeared, never how the line was assembled.

func goldenTime() time.Time {
	return time.Date(2026, 8, 1, 9, 41, 22, 461_000_000, time.UTC)
}

type goldenCase struct {
	name  string
	entry log.Entry
}

func goldenCases() []goldenCase {
	ts := goldenTime()
	return []goldenCase{
		{"plain", log.Entry{Level: log.INFO, Message: "server started", Time: ts}},
		{"empty message", log.Entry{Level: log.INFO, Time: ts,
			Fields: []log.Field{log.String("only", "fields")}}},
		{"field types", log.Entry{Level: log.INFO, Message: "types", Time: ts, Fields: []log.Field{
			log.String("str", "value"),
			log.Int("int", -7),
			log.Uint64("uint", 18446744073709551615),
			log.Float64("float", 1.5),
			log.Bool("bool", true),
			log.Duration("dur", 1500*time.Millisecond),
			log.Time("time", ts),
			log.Binary("bin", []byte{0xde, 0xad}),
			log.Err("err", errors.New("connection refused")),
			log.Any("nil", nil),
		}}},
		{"groups", log.Entry{Level: log.INFO, Message: "request", Time: ts, Fields: []log.Field{
			log.String("id", "req-42"),
			log.Group("http", log.String("method", "GET"), log.Int("status", 200),
				log.Group("client", log.String("ip", "10.0.0.1"))),
		}}},
		{"empty group", log.Entry{Level: log.INFO, Message: "m", Time: ts,
			Fields: []log.Field{log.Group("empty")}}},
		{"caller", log.Entry{Level: log.INFO, Message: "with call site", Time: ts,
			Caller: log.Caller{File: "main.go", Line: 42}}},
		{"caller and fields", log.Entry{Level: log.WARN, Message: "slow", Time: ts,
			Caller: log.Caller{File: "handler.go", Line: 118},
			Fields: []log.Field{log.Duration("took", 2*time.Second)}}},
		{"no timestamp", log.Entry{Level: log.INFO, Message: "a record with no time"}},
		{"no timestamp with caller", log.Entry{Level: log.INFO, Message: "no time, has caller",
			Caller: log.Caller{File: "bridge.go", Line: 7}}},
		{"control characters", log.Entry{Level: log.ERROR, Time: ts,
			Message: "line one\nline two\ttabbed",
			Fields:  []log.Field{log.String("user", "eve\n2020-01-01 00:00:00 - [ERROR] forged")}}},
		{"quotes and backslash", log.Entry{Level: log.INFO, Message: `say "hi"\ok`, Time: ts,
			Fields: []log.Field{log.String("path", `C:\tmp\x`)}}},
		{"invalid utf8", log.Entry{Level: log.INFO, Message: "bad \xff byte", Time: ts,
			Fields: []log.Field{log.String("k", "a\xffb")}}},
		{"spaces in values", log.Entry{Level: log.INFO, Message: "note", Time: ts,
			Fields: []log.Field{log.String("text", "with spaces"), log.String("empty", "")}}},
	}
}

func levelCases() []goldenCase {
	ts := goldenTime()
	var out []goldenCase
	for _, lvl := range []log.LogLevel{log.TRACE, log.DEBUG, log.INFO, log.WARN, log.ERROR, log.PANIC, log.FATAL} {
		out = append(out, goldenCase{
			name:  "level " + lvl.String(),
			entry: log.Entry{Level: lvl, Message: "message", Time: ts},
		})
	}
	return out
}

func TestEncoderGolden(t *testing.T) {
	encoders := map[string]log.Encoder{
		"text":         &log.TextEncoder{},
		"text_color":   &log.TextEncoder{Colorize: true},
		"text_layout":  &log.TextEncoder{TimeLayout: time.RFC3339},
		"json":         &log.JSONEncoder{},
		"console":      &log.ConsoleEncoder{NoColor: true},
		"console_time": &log.ConsoleEncoder{NoColor: true, TimeLayout: "15:04:05"},
	}

	cases := append(goldenCases(), levelCases()...)

	for name, enc := range encoders {
		t.Run(name, func(t *testing.T) {
			var got bytes.Buffer
			for _, tc := range cases {
				fmt.Fprintf(&got, "--- %s\n", tc.name)
				got.Write(enc.Encode(nil, tc.entry))
			}
			compareGolden(t, filepath.Join("testdata", "golden", name+".txt"), got.Bytes())
		})
	}
}

// Whatever the input, every line the JSON encoder produces must parse.
func TestJSONGoldenIsValid(t *testing.T) {
	enc := &log.JSONEncoder{}
	for _, tc := range append(goldenCases(), levelCases()...) {
		out := enc.Encode(nil, tc.entry)
		var into map[string]any
		if err := json.Unmarshal(out, &into); err != nil {
			t.Errorf("%s: invalid JSON: %v\n  %s", tc.name, err, out)
		}
	}
}

// Text and console output is line-oriented: one entry is always one line.
func TestTextEncodersEmitOneLinePerEntry(t *testing.T) {
	encoders := map[string]log.Encoder{
		"text":    &log.TextEncoder{},
		"console": &log.ConsoleEncoder{NoColor: true},
	}
	for name, enc := range encoders {
		for _, tc := range append(goldenCases(), levelCases()...) {
			out := enc.Encode(nil, tc.entry)
			if n := bytes.Count(bytes.TrimRight(out, "\n"), []byte("\n")); n != 0 {
				t.Errorf("%s/%s: entry spans %d extra lines:\n%s", name, tc.name, n, out)
			}
		}
	}
}

// The same entries again, this time through a real logger, so the assembly
// around the encoder is pinned too and not just the encoder in isolation.
func TestLoggerOutputGolden(t *testing.T) {
	var buf bytes.Buffer
	clock := log.WithClock(goldenTime)

	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.TRACE), clock))
	logger.Info("server started", log.String("addr", ":8080"))
	logger.Warnf("retrying in %s", 250*time.Millisecond)
	logger.Trace("entering handler")
	logger.Error("upstream failed", log.Err("error", errors.New("connection refused")))
	logger.With(log.String("request_id", "req-42")).Named("http").Info("handling")
	logger.Info("request", log.Group("http", log.String("method", "GET"), log.Int("status", 200)))

	redacting := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), clock,
		log.WithRedactor(log.NewKeyRedactor("password"))))
	redacting.Info("login", log.String("user", "alice"), log.String("password", "hunter2"))

	jsonLogger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON(), clock))
	jsonLogger.Info("server started", log.String("addr", ":8080"), log.Bool("tls", true))

	compareGolden(t, filepath.Join("testdata", "golden", "logger.txt"), buf.Bytes())
}

func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\n(run `go test ./ -update` to create it)", err)
	}
	if bytes.Equal(want, got) {
		return
	}

	// Report the first differing line rather than two blobs.
	wantLines, gotLines := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			t.Fatalf("%s differs at line %d\n  want: %q\n  got:  %q\n(run `go test ./ -update` if the change is intended)",
				path, i+1, w, g)
		}
	}
}

// The reason values are quoted: key=value output is only useful if a consumer
// can split it back apart. This asserts the property rather than the bytes, so
// it keeps holding if the format is adjusted again.
func TestTextValuesSplitBackApart(t *testing.T) {
	entry := log.Entry{Level: log.INFO, Message: "note", Time: goldenTime(), Fields: []log.Field{
		log.String("err", "connection refused"),
		log.String("empty", ""),
		log.String("quoted", `say "hi"`),
		log.String("equals", "a=b"),
		log.String("addr", ":8080"),
		log.Int("n", 3),
	}}

	for name, enc := range map[string]log.Encoder{
		"text":    &log.TextEncoder{},
		"console": &log.ConsoleEncoder{NoColor: true},
	} {
		line := strings.TrimRight(string(enc.Encode(nil, entry)), "\n")
		got := map[string]string{}
		for _, tok := range splitOutsideQuotes(line) {
			k, v, ok := strings.Cut(tok, "=")
			if !ok {
				continue
			}
			if unquoted, err := strconv.Unquote(v); err == nil {
				v = unquoted
			}
			got[k] = v
		}
		for _, want := range []struct{ k, v string }{
			{"err", "connection refused"},
			{"empty", ""},
			{"quoted", `say "hi"`},
			{"equals", "a=b"},
			{"addr", ":8080"},
			{"n", "3"},
		} {
			if got[want.k] != want.v {
				t.Errorf("%s: %s = %q, want %q (line: %s)", name, want.k, got[want.k], want.v, line)
			}
		}
	}
}

// splitOutsideQuotes splits on spaces that are not inside a quoted value.
func splitOutsideQuotes(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
			cur.WriteByte(c)
		case c == '\\' && inQuote:
			escaped = true
			cur.WriteByte(c)
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
