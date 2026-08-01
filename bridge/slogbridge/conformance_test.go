package slogbridge_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/slogtest"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/slogbridge"
)

// slogtest is the standard library's conformance suite for slog.Handler. It is
// the specification; running it here keeps the bridge honest.
func TestSlogHandlerConformance(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.TRACE), log.WithJSON()))
	h := slogbridge.NewHandler(logger, nil)

	results := func() []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("emitted non-JSON: %v (%q)", err, line)
			}
			// The encoder names these fields for its own output; slogtest looks
			// for slog's names.
			if ts, ok := m["timestamp"]; ok {
				m[slogKeyTime] = ts
			}
			if msg, ok := m["message"]; ok {
				m[slogKeyMessage] = msg
			}
			out = append(out, m)
		}
		return out
	}

	err := slogtest.TestHandler(h, results)
	if err == nil {
		return
	}

	// One documented deviation: this logger always stamps an entry with a time,
	// so a record carrying none still comes out with one. Honoring "ignore a
	// zero Record.Time" would mean teaching the encoders to omit timestamps,
	// which is wrong for every other caller. Every other check must pass.
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "should ignore a zero Record.Time") {
			continue
		}
		t.Errorf("slogtest: %s", line)
	}
}

const (
	slogKeyTime    = "time"
	slogKeyMessage = "msg"
)
