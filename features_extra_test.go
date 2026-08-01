package log_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestBurstSampler(t *testing.T) {
	// first=2, thereafter=0: only the first two in the window pass.
	s := log.NewBurstSampler(time.Hour, 2, 0)
	allowed := 0
	for i := 0; i < 5; i++ {
		if s.Allow(log.ERROR, "boom", nil) {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("expected 2 allowed in burst, got %d", allowed)
	}

	// Distinct message gets its own bucket.
	if !s.Allow(log.ERROR, "different", nil) {
		t.Fatal("a new message key should pass the burst allowance")
	}

	// first=1, thereafter=2: positions 1,3,5 pass.
	s2 := log.NewBurstSampler(time.Hour, 1, 2)
	var pattern []bool
	for i := 0; i < 5; i++ {
		pattern = append(pattern, s2.Allow(log.INFO, "x", nil))
	}
	want := []bool{true, false, true, false, true}
	for i := range want {
		if pattern[i] != want[i] {
			t.Fatalf("burst thereafter pattern = %v, want %v", pattern, want)
		}
	}
}

func TestLevelSampler(t *testing.T) {
	blockAll := log.SamplerFunc(func(log.LogLevel, string, []log.Field) bool { return false })
	s := log.NewLevelSampler(map[log.LogLevel]log.Sampler{log.INFO: blockAll})

	if s.Allow(log.INFO, "m", nil) {
		t.Error("INFO should be sampled away by its sampler")
	}
	if !s.Allow(log.ERROR, "m", nil) {
		t.Error("ERROR has no sampler and must always pass")
	}
}

func TestInjectableClock(t *testing.T) {
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := func() time.Time { return fixed }

	var jb bytes.Buffer
	jl := log.Must(log.New(log.WithOutput(&jb), log.WithLevel(log.INFO), log.WithJSON(), log.WithClock(clock)))
	jl.Info("x")
	if !strings.Contains(jb.String(), "2020-01-02T03:04:05Z") {
		t.Fatalf("JSON clock not injected: %q", jb.String())
	}

	var db bytes.Buffer
	dl := log.Must(log.New(log.WithOutput(&db), log.WithLevel(log.INFO), log.WithClock(clock), log.WithTimeFormat("2006-01-02")))
	dl.Info("x")
	if !strings.Contains(db.String(), "2020-01-02") {
		t.Fatalf("Default clock not injected: %q", db.String())
	}
}

func TestErrorVerbose(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	wrapped := fmt.Errorf("outer: %w", errors.New("inner cause"))
	logger.Info("failed", log.ErrorVerbose("error", wrapped))
	out := buf.String()
	if !strings.Contains(out, "outer") || !strings.Contains(out, "inner cause") {
		t.Fatalf("expected full error chain, got %q", out)
	}
}

func TestConsoleFormatter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithConsole()))
	logger.Info("hello", log.String("k", "v"), log.String("phrase", "two words"))
	out := buf.String()
	for _, want := range []string{"INFO", "hello", "k=v", `phrase="two words"`} {
		if !strings.Contains(out, want) {
			t.Errorf("console output missing %q, got %q", want, out)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Errorf("NoColor output should have no ANSI codes: %q", out)
	}
}

func TestPerLevelOutputs(t *testing.T) {
	var main, errSink bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&main), log.WithLevel(log.DEBUG), log.WithLevelOutput(log.ERROR, &errSink)))

	logger.Info("just info")
	if errSink.Len() != 0 {
		t.Fatalf("INFO should not reach the ERROR sink, got %q", errSink.String())
	}

	logger.Error("a problem")
	if !strings.Contains(errSink.String(), "a problem") {
		t.Fatalf("ERROR should reach the ERROR sink, got %q", errSink.String())
	}
	if !strings.Contains(main.String(), "a problem") || !strings.Contains(main.String(), "just info") {
		t.Fatalf("primary output should receive everything, got %q", main.String())
	}
}

func TestRequestIDHelpers(t *testing.T) {
	ctx := log.WithRequestID(context.Background(), "req-42")
	if log.RequestID(ctx) != "req-42" {
		t.Fatalf("RequestID round-trip failed, got %q", log.RequestID(ctx))
	}

	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	logger.InfoContext(ctx, "handling")
	if !strings.Contains(buf.String(), "request_id=req-42") {
		t.Fatalf("expected request_id emitted on context log, got %q", buf.String())
	}
}

// --- merged from text_escaping_test.go ---

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
		{"field value", func(l *log.Logger) { l.Info("login", log.String("user", forged)) }},
		{"message", func(l *log.Logger) { l.Info(forged) }},
		{"formatted message", func(l *log.Logger) { l.Infof("login %s", forged) }},
		{"error value", func(l *log.Logger) { l.Info("login", log.Err("err", errString(forged))) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
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
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithConsole()))
	l.Info("login\nsecond line", log.String("user", "eve\nadmin"))
	if n := lineCount(buf.String()); n != 1 {
		t.Errorf("entry spans %d lines, want 1:\n%s", n, buf.String())
	}
}

// Ordinary values must not start getting quoted just because escaping exists.
func TestTextFormatterLeavesPlainValuesAlone(t *testing.T) {
	var buf bytes.Buffer
	l := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	l.Info("started", log.String("addr", "127.0.0.1:8080"), log.String("note", "with spaces"), log.Int("n", 3))

	got := buf.String()
	for _, want := range []string{"started", "addr=127.0.0.1:8080", "note=with spaces", "n=3"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %s", want, got)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// --- merged from sampler_memory_test.go ---

// High-cardinality messages must not grow the sampler without bound.
func TestBurstSamplerBoundsMemory(t *testing.T) {
	s := log.NewBurstSampler(10*time.Millisecond, 1, 0)

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	for i := 0; i < 200_000; i++ {
		s.Allow(log.ERROR, fmt.Sprintf("request %d failed with a reasonably long message", i), nil)
		if i%5_000 == 0 {
			time.Sleep(time.Millisecond) // let windows expire so sweeps have work
		}
	}

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)

	// Retaining every key costs ~25 MiB here; a bounded sampler settles in the
	// kilobytes. 4 MiB sits well clear of both.
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 4<<20 {
		t.Errorf("sampler retained %d KiB across 200k distinct messages", grew/1024)
	}
}

// Sweeping must not change the decisions the sampler makes.
func TestBurstSamplerStillRateLimits(t *testing.T) {
	s := log.NewBurstSampler(time.Hour, 2, 3)

	allowed := 0
	for i := 0; i < 10; i++ {
		if s.Allow(log.ERROR, "same message", nil) {
			allowed++
		}
	}
	// first 2, then every 3rd of the remaining 8: entries 5 and 8.
	if allowed != 4 {
		t.Errorf("allowed %d of 10, want 4", allowed)
	}

	// A distinct message keeps its own budget.
	if !s.Allow(log.ERROR, "other message", nil) {
		t.Error("first occurrence of a new message should be allowed")
	}
}

func TestBurstSamplerReopensAfterWindow(t *testing.T) {
	s := log.NewBurstSampler(20*time.Millisecond, 1, 0)
	if !s.Allow(log.WARN, "msg", nil) {
		t.Fatal("first entry should be allowed")
	}
	if s.Allow(log.WARN, "msg", nil) {
		t.Fatal("second entry within the window should be suppressed")
	}
	time.Sleep(30 * time.Millisecond)
	if !s.Allow(log.WARN, "msg", nil) {
		t.Error("entry after the window should be allowed again")
	}
}
