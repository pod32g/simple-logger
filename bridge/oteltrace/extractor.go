// Package oteltrace provides a simple-logger ContextExtractor that automatically
// adds the active OpenTelemetry trace_id and span_id to every context-aware log
// entry, so logs can be correlated with traces in observability backends.
package oteltrace

import (
	"context"

	log "github.com/pod32g/simple-logger"
	"go.opentelemetry.io/otel/trace"
)

// Extractor returns trace_id, span_id, and trace_flags fields for the active
// span in ctx, or nil when there is no valid span. Install it with
//
//	logger.SetContextExtractor(oteltrace.Extractor)
//
// so that DebugContext/InfoContext/... calls are automatically trace-correlated.
// The trace_id/span_id keys are also recognized by the OTLP bridge, which
// promotes them to first-class LogRecord fields.
func Extractor(ctx context.Context) []log.Field {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []log.Field{
		log.String("trace_id", sc.TraceID().String()),
		log.String("span_id", sc.SpanID().String()),
		log.String("trace_flags", sc.TraceFlags().String()),
	}
}

// Chain combines the OTel trace extractor with simple-logger's default context
// fields (those stored via log.WithFields), so both trace IDs and ad-hoc context
// fields are emitted. Install with:
//
//	logger.SetContextExtractor(oteltrace.Chain)
func Chain(ctx context.Context) []log.Field {
	fields := Extractor(ctx)
	if extra := log.ContextFields(ctx); len(extra) > 0 {
		fields = append(fields, extra...)
	}
	return fields
}
