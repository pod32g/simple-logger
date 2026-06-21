package oteltrace_test

import (
	"context"
	"testing"

	"github.com/pod32g/simple-logger/bridge/oteltrace"
	"go.opentelemetry.io/otel/trace"
)

func TestExtractorReturnsTraceFields(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	fields := oteltrace.Extractor(ctx)
	found := map[string]string{}
	for _, f := range fields {
		if s, ok := f.Value.(string); ok {
			found[f.Key] = s
		}
	}
	if found["trace_id"] != traceID.String() {
		t.Errorf("trace_id = %q, want %q", found["trace_id"], traceID.String())
	}
	if found["span_id"] != spanID.String() {
		t.Errorf("span_id = %q, want %q", found["span_id"], spanID.String())
	}
}

func TestExtractorNilWithoutSpan(t *testing.T) {
	if fields := oteltrace.Extractor(context.Background()); fields != nil {
		t.Errorf("expected nil fields without a valid span, got %v", fields)
	}
}
