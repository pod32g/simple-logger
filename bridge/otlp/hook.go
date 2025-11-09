package otlp

import (
	"context"
	"fmt"
	"time"

	log "github.com/pod32g/simple-logger"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// Exporter represents anything that can receive OTLP log payloads.
type Exporter interface {
	Export(context.Context, *logspb.ResourceLogs) error
	Shutdown(context.Context) error
}

// Hook exports log entries to an OTLP exporter.
type Hook struct {
	exporter Exporter
	resource *resourcepb.Resource
	scope    *commonpb.InstrumentationScope
}

// Option configures the OTLP hook.
type Option func(*Hook)

// WithServiceName sets the service.name resource attribute.
func WithServiceName(name string) Option {
	return func(h *Hook) {
		h.resource.Attributes = append(h.resource.Attributes, &commonpb.KeyValue{
			Key:   "service.name",
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: name}},
		})
	}
}

// WithResourceAttribute appends a custom resource attribute.
func WithResourceAttribute(key string, value string) Option {
	return func(h *Hook) {
		h.resource.Attributes = append(h.resource.Attributes, &commonpb.KeyValue{
			Key:   key,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
		})
	}
}

// NewHook constructs a OTLP hook using the provided exporter.
func NewHook(exporter Exporter, opts ...Option) *Hook {
	h := &Hook{
		exporter: exporter,
		resource: &resourcepb.Resource{},
		scope:    &commonpb.InstrumentationScope{Name: "github.com/pod32g/simple-logger"},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Fire satisfies the Hook interface for simple-logger.
func (h *Hook) Fire(level log.LogLevel, message string, fields []log.Field) {
	record := &logspb.LogRecord{
		TimeUnixNano:         uint64(time.Now().UnixNano()),
		ObservedTimeUnixNano: uint64(time.Now().UnixNano()),
		SeverityText:         logLevelString(level),
		SeverityNumber:       severityNumber(level),
		Body:                 &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: message}},
	}

	for _, field := range fields {
		record.Attributes = append(record.Attributes, &commonpb.KeyValue{
			Key:   field.Key,
			Value: anyValue(field.Value),
		})
	}

	resourceLogs := &logspb.ResourceLogs{
		Resource: h.resource,
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope:      h.scope,
			LogRecords: []*logspb.LogRecord{record},
		}},
	}

	_ = h.exporter.Export(context.Background(), resourceLogs)
}

// Close shuts down the exporter.
func (h *Hook) Close(ctx context.Context) error {
	return h.exporter.Shutdown(ctx)
}

func severityNumber(level log.LogLevel) logspb.SeverityNumber {
	switch level {
	case log.DEBUG:
		return logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG
	case log.INFO:
		return logspb.SeverityNumber_SEVERITY_NUMBER_INFO
	case log.WARN:
		return logspb.SeverityNumber_SEVERITY_NUMBER_WARN
	case log.ERROR:
		return logspb.SeverityNumber_SEVERITY_NUMBER_ERROR
	case log.FATAL:
		return logspb.SeverityNumber_SEVERITY_NUMBER_FATAL
	default:
		return logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED
	}
}

func logLevelString(level log.LogLevel) string {
	switch level {
	case log.DEBUG:
		return "DEBUG"
	case log.INFO:
		return "INFO"
	case log.WARN:
		return "WARN"
	case log.ERROR:
		return "ERROR"
	case log.FATAL:
		return "FATAL"
	default:
		return "INFO"
	}
}

func anyValue(val interface{}) *commonpb.AnyValue {
	switch v := val.(type) {
	case string:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}
	case bool:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v}}
	case int:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: int64(v)}}
	case int64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v}}
	case uint64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: int64(v)}}
	case float32:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: float64(v)}}
	case float64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v}}
	case time.Duration:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v.String()}}
	case time.Time:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v.Format(time.RFC3339)}}
	default:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprint(val)}}
	}
}
