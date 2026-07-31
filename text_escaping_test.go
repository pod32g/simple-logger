package log_test

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func lineCount(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestTextFormatterDoesNotForgeLines(t *testing.T) {
	forged := "eve\n2020-01-01 00:00:00 - [ERROR] fake entry"

	cases := []struct {
		name string
		emit func(*log.Logger)
	}{
		{"field value", func(l *log.Logger) { l.InfoFields("login", log.String("user", forged)) }},
		{"message", func(l *log.Logger) { l.InfoString(forged) }},
		{"formatted args", func(l *log.Logger) { l.Info("login", forged) }},
		{"error value", func(l *log.Logger) { l.InfoFields("login", log.Error("err", errString(forged))) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})
			tc.emit(l)
			if n := lineCount(buf.String()); n != 1 {
				t.Errorf("entry spans %d lines, want 1:\n%s", n, buf.String())
			}
			if !strings.Contains(buf.String(), `\n`) {
				t.Errorf("newline was not escaped:\n%s", buf.String())
			}
		})
	}
}

func TestConsoleFormatterDoesNotForgeLines(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.INFO, &log.ConsoleFormatter{NoColor: true})
	l.InfoFields("login\nsecond line", log.String("user", "eve\nadmin"))
	if n := lineCount(buf.String()); n != 1 {
		t.Errorf("entry spans %d lines, want 1:\n%s", n, buf.String())
	}
}

// Ordinary values must not start getting quoted just because escaping exists.
func TestTextFormatterLeavesPlainValuesAlone(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{})
	l.InfoFields("started", log.String("addr", "127.0.0.1:8080"), log.String("note", "with spaces"), log.Int("n", 3))

	got := buf.String()
	for _, want := range []string{"started", "addr=127.0.0.1:8080", "note=with spaces", "n=3"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %s", want, got)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
