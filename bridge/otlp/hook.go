package otlp

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
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

// Defaults for the batching worker. They mirror the OTLP SDK's batch processor:
// export in bulk, on an interval, and never wait forever on a collector.
const (
	defaultQueueSize     = 2048
	defaultBatchSize     = 512
	defaultFlushInterval = time.Second
	defaultExportTimeout = 10 * time.Second
)

// Hook exports log entries to an OTLP exporter.
//
// Records are queued and exported in batches by a background goroutine, so a
// log call never waits on the collector. Call Close (or Flush) before the
// process exits, or records still in the queue are lost. WithSynchronousExport
// restores in-line exporting for callers who would rather block.
type Hook struct {
	exporter Exporter
	resource *resourcepb.Resource
	scope    *commonpb.InstrumentationScope
	onError  func(error)
	failed   atomic.Int64
	dropped  atomic.Int64

	timeout       time.Duration
	synchronous   bool
	queueSize     int
	batchSize     int
	flushInterval time.Duration

	queue    chan *logspb.LogRecord
	flushReq chan chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	closed   atomic.Bool
	wg       sync.WaitGroup
}

// HookStats reports health counters for the OTLP hook.
type HookStats struct {
	// Failed is the number of exports that returned an error.
	Failed int64
	// Dropped is the number of records discarded because the queue was full.
	Dropped int64
	// Queued is the number of records waiting to be exported.
	Queued int
}

// Option configures the OTLP hook.
type Option func(*Hook)

// WithExportTimeout bounds how long a single export may take. Zero disables the
// deadline. Defaults to 10s: without one, a collector that accepts the
// connection and then stops responding blocks the export indefinitely.
func WithExportTimeout(d time.Duration) Option {
	return func(h *Hook) { h.timeout = d }
}

// WithBatchSize sets how many records are exported together.
func WithBatchSize(n int) Option {
	return func(h *Hook) { h.batchSize = n }
}

// WithFlushInterval sets how long a partial batch waits before being exported
// anyway. Zero means a batch is only exported once it fills, or on Flush/Close.
func WithFlushInterval(d time.Duration) Option {
	return func(h *Hook) { h.flushInterval = d }
}

// WithQueueSize sets how many records may await export. Records arriving at a
// full queue are dropped and counted in Stats, so a slow collector costs
// telemetry rather than application latency.
func WithQueueSize(n int) Option {
	return func(h *Hook) { h.queueSize = n }
}

// WithSynchronousExport exports each record inline, on the goroutine that
// logged, instead of batching. It couples log calls to collector latency; use it
// only where delivery before the call returns matters more than that.
func WithSynchronousExport() Option {
	return func(h *Hook) { h.synchronous = true }
}

// WithErrorHandler registers a callback invoked whenever an export fails.
// Without one, export failures are still counted (see Stats) but are otherwise
// silent — telemetry delivery to a remote backend can fail, so callers wiring
// this into production should observe it.
func WithErrorHandler(fn func(error)) Option {
	return func(h *Hook) {
		h.onError = fn
	}
}

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

// NewHook constructs a OTLP hook using the provided exporter. Unless
// WithSynchronousExport is passed it starts a background goroutine, so the hook
// must be closed with Close when it is no longer needed.
func NewHook(exporter Exporter, opts ...Option) *Hook {
	h := &Hook{
		exporter:      exporter,
		resource:      &resourcepb.Resource{},
		scope:         &commonpb.InstrumentationScope{Name: "github.com/pod32g/simple-logger"},
		timeout:       defaultExportTimeout,
		queueSize:     defaultQueueSize,
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.queueSize <= 0 {
		h.queueSize = defaultQueueSize
	}
	if h.batchSize <= 0 {
		h.batchSize = defaultBatchSize
	}
	if h.flushInterval < 0 {
		h.flushInterval = 0
	}
	if h.timeout < 0 {
		h.timeout = 0
	}
	if !h.synchronous && exporter != nil {
		h.queue = make(chan *logspb.LogRecord, h.queueSize)
		h.flushReq = make(chan chan struct{})
		h.stop = make(chan struct{})
		h.wg.Add(1)
		go h.worker()
	}
	return h
}

func (h *Hook) worker() {
	defer h.wg.Done()

	batch := make([]*logspb.LogRecord, 0, h.batchSize)
	var timeout <-chan time.Time
	var timer *time.Timer
	if h.flushInterval > 0 {
		timer = time.NewTimer(h.flushInterval)
		defer timer.Stop()
		timeout = timer.C
	}

	drain := func() {
		for {
			select {
			case rec := <-h.queue:
				batch = append(batch, rec)
			default:
				return
			}
		}
	}
	flush := func() {
		if len(batch) > 0 {
			h.export(batch)
			batch = batch[:0]
		}
	}

	for {
		select {
		case rec := <-h.queue:
			batch = append(batch, rec)
			if len(batch) >= h.batchSize {
				flush()
			}
		case done := <-h.flushReq:
			drain()
			flush()
			close(done)
		case <-timeout:
			flush()
			timer.Reset(h.flushInterval)
		case <-h.stop:
			drain()
			flush()
			return
		}
	}
}

// export ships one batch, bounded by the configured timeout.
func (h *Hook) export(records []*logspb.LogRecord) {
	payload := make([]*logspb.LogRecord, len(records))
	copy(payload, records) // the worker reuses its batch slice

	ctx := context.Background()
	if h.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.timeout)
		defer cancel()
	}

	resourceLogs := &logspb.ResourceLogs{
		Resource: h.resource,
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope:      h.scope,
			LogRecords: payload,
		}},
	}
	if err := h.exporter.Export(ctx, resourceLogs); err != nil {
		h.failed.Add(1)
		if h.onError != nil {
			h.onError(err)
		}
	}
}

// Flush blocks until every record queued before the call has been exported. It
// is a no-op for a hook exporting synchronously.
func (h *Hook) Flush(ctx context.Context) error {
	if h.queue == nil || h.closed.Load() {
		return nil
	}
	done := make(chan struct{})
	select {
	case h.flushReq <- done:
	case <-h.stop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Fire satisfies the Hook interface for simple-logger.
func (h *Hook) Fire(level log.LogLevel, message string, fields []log.Field) {
	if h.exporter == nil {
		return
	}
	record := &logspb.LogRecord{
		TimeUnixNano:         uint64(time.Now().UnixNano()),
		ObservedTimeUnixNano: uint64(time.Now().UnixNano()),
		SeverityText:         logLevelString(level),
		SeverityNumber:       severityNumber(level),
		Body:                 &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: message}},
	}

	for _, field := range fields {
		// Promote well-known trace-correlation keys to the first-class OTLP
		// LogRecord fields so backends can join logs to traces at the protocol
		// level, in addition to keeping them as attributes.
		if s, ok := field.Value.(string); ok {
			switch field.Key {
			case "trace_id":
				if b, err := hex.DecodeString(s); err == nil && len(b) == 16 {
					record.TraceId = b
				}
			case "span_id":
				if b, err := hex.DecodeString(s); err == nil && len(b) == 8 {
					record.SpanId = b
				}
			}
		}
		record.Attributes = append(record.Attributes, &commonpb.KeyValue{
			Key:   field.Key,
			Value: anyValue(field.Value),
		})
	}

	if h.queue == nil {
		h.export([]*logspb.LogRecord{record})
		return
	}
	if h.closed.Load() {
		h.dropped.Add(1)
		return
	}
	select {
	case h.queue <- record:
	default:
		// The collector is not keeping up. Losing telemetry is the right trade
		// against blocking the goroutine that logged.
		h.dropped.Add(1)
	}
}

// Stats returns the hook's export counters.
func (h *Hook) Stats() HookStats {
	stats := HookStats{
		Failed:  h.failed.Load(),
		Dropped: h.dropped.Load(),
	}
	if h.queue != nil {
		stats.Queued = len(h.queue)
	}
	return stats
}

// Close exports whatever is still queued, stops the background worker and shuts
// the exporter down.
func (h *Hook) Close(ctx context.Context) error {
	if h.exporter == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		h.closed.Store(true)
		if h.queue != nil {
			close(h.stop)
			h.wg.Wait()
		}
	})
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
