package otlp

import (
	"context"
	"encoding/hex"
	"testing"

	log "github.com/pod32g/simple-logger"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

type recordOnlyExporter struct{ record *logspb.LogRecord }

func (r *recordOnlyExporter) Export(_ context.Context, rl *logspb.ResourceLogs) error {
	if len(rl.ScopeLogs) > 0 && len(rl.ScopeLogs[0].LogRecords) > 0 {
		r.record = rl.ScopeLogs[0].LogRecords[0]
	}
	return nil
}
func (r *recordOnlyExporter) Shutdown(context.Context) error { return nil }

func TestHookPromotesTraceAndSpanIDs(t *testing.T) {
	exp := &recordOnlyExporter{}
	hook := NewHook(exp)

	traceID := "0123456789abcdef0123456789abcdef" // 16 bytes
	spanID := "0123456789abcdef"                  // 8 bytes
	hook.Fire(log.INFO, "msg",
		[]log.Field{log.String("trace_id", traceID), log.String("span_id", spanID)})

	wantTrace, _ := hex.DecodeString(traceID)
	wantSpan, _ := hex.DecodeString(spanID)
	if string(exp.record.TraceId) != string(wantTrace) {
		t.Errorf("TraceId = %x, want %s", exp.record.TraceId, traceID)
	}
	if string(exp.record.SpanId) != string(wantSpan) {
		t.Errorf("SpanId = %x, want %s", exp.record.SpanId, spanID)
	}
	// They must also remain available as attributes.
	if len(exp.record.Attributes) != 2 {
		t.Errorf("expected trace fields retained as attributes, got %d", len(exp.record.Attributes))
	}
}

func TestHookIgnoresMalformedTraceID(t *testing.T) {
	exp := &recordOnlyExporter{}
	hook := NewHook(exp)
	hook.Fire(log.INFO, "msg", []log.Field{log.String("trace_id", "not-hex")})
	if exp.record.TraceId != nil {
		t.Errorf("malformed trace_id should be ignored, got %x", exp.record.TraceId)
	}
}
