package log_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestTraceLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.TRACE)))

	logger.Trace("finest detail", log.Int("step", 1))
	if !strings.Contains(buf.String(), "[TRACE]") || !strings.Contains(buf.String(), "finest detail") {
		t.Errorf("expected a TRACE entry, got %q", buf.String())
	}

	// TRACE sits below DEBUG, so a DEBUG logger hides it.
	buf.Reset()
	quiet := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	quiet.Trace("hidden")
	if buf.Len() != 0 {
		t.Errorf("TRACE should be below DEBUG, got %q", buf.String())
	}
}

func TestLevelOrdering(t *testing.T) {
	ordered := []log.LogLevel{log.TRACE, log.DEBUG, log.INFO, log.WARN, log.ERROR, log.PANIC, log.FATAL}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1] >= ordered[i] {
			t.Errorf("%v should sort below %v", ordered[i-1], ordered[i])
		}
	}
}

func TestParseLevelRoundTrip(t *testing.T) {
	for _, name := range []string{"trace", "debug", "info", "warn", "warning", "error", "panic", "fatal"} {
		lvl, err := log.ParseLevel(name)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", name, err)
			continue
		}
		if _, err := log.ParseLevel(lvl.String()); err != nil {
			t.Errorf("%q did not round trip through %q: %v", name, lvl.String(), err)
		}
	}
}

// Panic writes the entry and then panics, the way Fatal writes and then exits.
func TestPanicLogsThenPanics(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Panic to panic")
		}
		if !strings.Contains(buf.String(), "unrecoverable") {
			t.Errorf("expected the entry to be written before panicking, got %q", buf.String())
		}
	}()
	logger.Panic("unrecoverable", log.String("cause", "test"))
}

// Panic must drain the async queue first, like Fatal.
func TestPanicDrainsAsyncQueue(t *testing.T) {
	if os.Getenv("PANIC_DRAIN_CHILD") == "1" {
		logger := log.Must(log.New(log.WithOutput(os.Stdout), log.WithLevel(log.INFO),
			log.WithAsyncQueue(128), log.WithAsyncBatch(32, time.Minute)))
		for i := 0; i < 5; i++ {
			logger.Info("queued-entry")
		}
		logger.Panic("panic-entry")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPanicDrainsAsyncQueue")
	cmd.Env = append(os.Environ(), "PANIC_DRAIN_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected the child to exit non-zero after panicking")
	}
	got := string(out)
	if n := strings.Count(got, "queued-entry"); n != 5 {
		t.Errorf("Panic wrote %d of 5 queued entries before panicking:\n%s", n, got)
	}
}

func TestFieldConstructors(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	logger.Info("types",
		log.Duration("took", 1500*time.Millisecond),
		log.Time("at", time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)),
		log.Binary("payload", []byte{0xde, 0xad, 0xbe, 0xef}),
		log.Uint64("big", 18446744073709551615),
		log.Float32("ratio", 0.5),
		log.Int32("small", -7),
		log.Uint32("count", 9),
		log.Stringer("dur", 2*time.Second),
		log.Err("failure", errors.New("boom")))

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	for key, want := range map[string]interface{}{
		"took":    "1.5s",
		"at":      "2026-07-31T12:00:00Z",
		"payload": "deadbeef",
		"big":     "18446744073709551615",
		"ratio":   0.5,
		"small":   float64(-7),
		"count":   float64(9),
		"dur":     "2s",
		"failure": "boom",
	} {
		got, ok := out[key]
		if !ok {
			t.Errorf("missing field %q in %v", key, out)
			continue
		}
		if key == "big" {
			// JSON numbers lose precision at this size; compare textually.
			if s := buf.String(); !strings.Contains(s, `"big":18446744073709551615`) {
				t.Errorf("uint64 lost precision: %s", s)
			}
			continue
		}
		if got != want {
			t.Errorf("field %q = %v (%T), want %v", key, got, got, want)
		}
	}
}

// Binary must not render as the decimal-number list fmt produces for []byte.
func TestBinaryIsReadable(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	logger.Info("blob", log.Binary("b", []byte{0x01, 0x02, 0xff}))

	if !strings.Contains(buf.String(), "b=0102ff") {
		t.Errorf("expected hex rendering, got %q", buf.String())
	}
}

func TestWithError(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	sentinel := errors.New("upstream refused")
	logger.WithError(sentinel).Error("request failed")

	if !strings.Contains(buf.String(), "error=upstream refused") {
		t.Errorf("expected the bound error, got %q", buf.String())
	}
}

// The package-level functions are the hello-world path.
func TestPackageLevelFunctions(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Default()
	t.Cleanup(func() { log.SetDefault(previous) })

	log.SetDefault(log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.TRACE))))

	log.Trace("t")
	log.Debug("d")
	log.Info("i", log.String("k", "v"))
	log.Warn("w")
	log.Error("e")
	log.Infof("formatted %d", 42)

	out := buf.String()
	for _, want := range []string{"[TRACE] t", "[DEBUG] d", "[INFO] i k=v", "[WARN] w", "[ERROR] e", "formatted 42"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
