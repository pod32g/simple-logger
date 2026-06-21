package log_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	jl := log.NewLogger(&jb, log.INFO, &log.JSONFormatter{Now: clock})
	jl.Info("x")
	if !strings.Contains(jb.String(), "2020-01-02T03:04:05Z") {
		t.Fatalf("JSON clock not injected: %q", jb.String())
	}

	var db bytes.Buffer
	dl := log.NewLogger(&db, log.INFO, &log.DefaultFormatter{Now: clock, TimeLayout: "2006-01-02"})
	dl.Info("x")
	if !strings.Contains(db.String(), "2020-01-02") {
		t.Fatalf("Default clock not injected: %q", db.String())
	}
}

func TestErrorVerbose(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	wrapped := fmt.Errorf("outer: %w", errors.New("inner cause"))
	logger.InfoFields("failed", log.ErrorVerbose("error", wrapped))
	out := buf.String()
	if !strings.Contains(out, "outer") || !strings.Contains(out, "inner cause") {
		t.Fatalf("expected full error chain, got %q", out)
	}
}

func TestConsoleFormatter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.INFO, &log.ConsoleFormatter{NoColor: true})
	logger.InfoFields("hello", log.String("k", "v"), log.String("phrase", "two words"))
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
	logger := log.NewLogger(&main, log.DEBUG, &log.DefaultFormatter{IncludeCaller: false})
	logger.AddLevelOutput(log.ERROR, &errSink)

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
	logger := log.NewLogger(&buf, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.InfoContext(ctx, "handling")
	if !strings.Contains(buf.String(), "request_id=req-42") {
		t.Fatalf("expected request_id emitted on context log, got %q", buf.String())
	}
}
