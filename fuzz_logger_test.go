package log_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode/utf8"

	log "github.com/pod32g/simple-logger"
)

func FuzzJSONFormatterFormat(f *testing.F) {
	f.Add("hello", "blob", []byte("world"))
	f.Add("", "", []byte{})
	f.Add("m", `ke"y`, []byte("a\xffb"))
	f.Add("line\nbreak", "k", []byte("\U0001D173"))

	f.Fuzz(func(t *testing.T, msg, key string, blob []byte) {
		formatter := &log.JSONFormatter{}
		var buf bytes.Buffer
		formatter.FormatWithFieldsTo(log.INFO, msg, []log.Field{
			log.String(key, string(blob)),
		}, &buf)

		// The point of a JSON encoder: whatever the bytes, the result parses.
		var out map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("invalid JSON: %v\n  message: %q\n  key: %q\n  value: %q\n  output: %s",
				err, msg, key, blob, buf.String())
		}
		// A key made of valid UTF-8 must survive escaping. One that is not
		// cannot: JSON strings are UTF-8, so invalid bytes necessarily become
		// the replacement rune, exactly as encoding/json does.
		if _, ok := out[key]; !ok && utf8.ValidString(key) {
			switch key {
			case "timestamp", "level", "message": // collides with a field we write
			default:
				t.Fatalf("key %q missing from %v", key, out)
			}
		}

		if s := formatter.FormatWithFields(log.ERROR, msg, nil); !json.Valid([]byte(s)) {
			t.Fatalf("invalid JSON from FormatWithFields: %q", s)
		}
	})
}
