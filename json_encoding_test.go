package log_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

// Every entry must parse as JSON no matter what bytes reach it, and the values
// must survive the round trip intact.
func TestJSONFormatterAlwaysEmitsValidJSON(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(*log.Logger)
		message   string
		fields    []log.Field
		wantKey   string
		wantValue string
	}{
		{
			name:      "quote in key",
			fields:    []log.Field{log.String(`ke"y`, "v")},
			wantKey:   `ke"y`,
			wantValue: "v",
		},
		{
			name:      "newline in key",
			fields:    []log.Field{log.String("a\nb", "v")},
			wantKey:   "a\nb",
			wantValue: "v",
		},
		{
			name:      "key that would close the object and forge a field",
			fields:    []log.Field{log.String(`x":"y","injected":"1`, "v")},
			wantKey:   `x":"y","injected":"1`,
			wantValue: "v",
		},
		{
			name:      "control characters in value",
			fields:    []log.Field{log.String("k", "a\x00b\x1fc")},
			wantKey:   "k",
			wantValue: "a\x00b\x1fc",
		},
		{
			name:      "non-printable astral rune",
			fields:    []log.Field{log.String("k", "\U0001D173")},
			wantKey:   "k",
			wantValue: "\U0001D173",
		},
		{
			name:      "emoji survives unescaped",
			fields:    []log.Field{log.String("k", "ok 🎉")},
			wantKey:   "k",
			wantValue: "ok 🎉",
		},
		{
			name:      "invalid utf8 becomes the replacement rune",
			fields:    []log.Field{log.String("k", "a\xffb")},
			wantKey:   "k",
			wantValue: "a�b",
		},
		{
			name:      "truncation that lands mid-rune",
			setup:     func(l *log.Logger) { l.SetMaxFieldBytes(2) },
			fields:    []log.Field{log.String("k", "aé")},
			wantKey:   "k",
			wantValue: "a...[+2 bytes]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{})
			if tc.setup != nil {
				tc.setup(l)
			}
			msg := tc.message
			if msg == "" {
				msg = "m"
			}
			l.InfoFields(msg, tc.fields...)

			var out map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
			}
			got, ok := out[tc.wantKey]
			if !ok {
				t.Fatalf("key %q missing from %v", tc.wantKey, out)
			}
			if got != tc.wantValue {
				t.Errorf("value = %q, want %q", got, tc.wantValue)
			}
			if len(out) != 4 { // timestamp, level, message, the one field
				t.Errorf("unexpected field count %d in %v -- a field may have been forged", len(out), out)
			}
		})
	}
}

func TestJSONFormatterEscapesMessage(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{})
	l.InfoString("line one\nline two\ttabbed \"quoted\"")

	if n := bytes.Count(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")); n != 0 {
		t.Errorf("message newline was not escaped, entry spans %d extra lines: %s", n, buf.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
	}
	if out["message"] != "line one\nline two\ttabbed \"quoted\"" {
		t.Errorf("message did not round trip: %q", out["message"])
	}
}

func TestTruncationStopsOnRuneBoundary(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{})
	l.SetMaxMessageBytes(4)
	l.InfoString("héllo")

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n  output: %s", err, buf.String())
	}
	msg, _ := out["message"].(string)
	if !strings.HasPrefix(msg, "hé") {
		t.Errorf("truncated message = %q, want it to keep the whole é", msg)
	}
	if strings.ContainsRune(msg, '�') {
		t.Errorf("truncation split a rune: %q", msg)
	}
}
