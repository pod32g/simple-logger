package log

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	builderPool = sync.Pool{
		New: func() interface{} {
			return new(strings.Builder)
		},
	}

	bufferPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

// DropStrategy defines behavior when the async queue is full.
type DropStrategy int

const (
	DropNew DropStrategy = iota
	DropOldest
	BlockWhenFull
)

// AsyncStats reports async queue usage.
type AsyncStats struct {
	QueueSize   int
	QueueLength int
	Dropped     int64
}

// HookOption configures hook registration.
type HookOption interface {
	apply(*hookRegistration)
}

type hookOptionFunc func(*hookRegistration)

func (fn hookOptionFunc) apply(h *hookRegistration) { fn(h) }

// WithHookLevels limits a hook to specific levels.
func WithHookLevels(levels ...LogLevel) HookOption {
	set := make(map[LogLevel]struct{}, len(levels))
	for _, lvl := range levels {
		set[lvl] = struct{}{}
	}
	return hookOptionFunc(func(h *hookRegistration) { h.levels = set })
}

// WithHookFilter provides a predicate to decide whether to fire a hook.
func WithHookFilter(fn func(LogLevel, string, []Field) bool) HookOption {
	return hookOptionFunc(func(h *hookRegistration) { h.filter = fn })
}

// WithHookFieldsFilter filters based on fields.
func WithHookFieldsFilter(fn func([]Field) bool) HookOption {
	return hookOptionFunc(func(h *hookRegistration) { h.fieldsFilter = fn })
}

type hookRegistration struct {
	target       Hook
	levels       map[LogLevel]struct{}
	filter       func(LogLevel, string, []Field) bool
	fieldsFilter func([]Field) bool
}

// Field represents a structured logging key/value pair.
type Field struct {
	Key   string
	Value interface{}
}

// Field constructors for common types.
func String(key, value string) Field  { return Field{Key: key, Value: value} }
func Int(key string, value int) Field { return Field{Key: key, Value: value} }
func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}
func Uint(key string, value uint) Field {
	return Field{Key: key, Value: value}
}
func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }
func Error(key string, err error) Field {
	if err == nil {
		return Field{Key: key, Value: nil}
	}
	return Field{Key: key, Value: err.Error()}
}
func Any(key string, value interface{}) Field { return Field{Key: key, Value: value} }

// FieldEncoder allows custom text/JSON rendering for specific value types.
type FieldEncoder struct {
	EncodeText func(interface{}) (string, bool)
	EncodeJSON func(interface{}) (interface{}, bool)
}

type fieldEncoder struct {
	text func(interface{}) (string, bool)
	json func(interface{}) (interface{}, bool)
}

var (
	encoderMu     sync.RWMutex
	fieldEncoders = make(map[reflect.Type]fieldEncoder)
)

// RegisterFieldEncoder attaches custom text/JSON encoders for a type.
// Pass nil for either function to fall back to default behavior.
func RegisterFieldEncoder[T any](text func(T) (string, bool), json func(T) (interface{}, bool)) {
	encoderMu.Lock()
	defer encoderMu.Unlock()
	t := reflect.TypeOf((*T)(nil)).Elem()
	fieldEncoders[t] = fieldEncoder{
		text: wrapTextEncoder(text),
		json: wrapJSONEncoder(json),
	}
}

func wrapTextEncoder[T any](fn func(T) (string, bool)) func(interface{}) (string, bool) {
	if fn == nil {
		return nil
	}
	return func(v interface{}) (string, bool) {
		val, ok := v.(T)
		if !ok {
			return "", false
		}
		return fn(val)
	}
}

func wrapJSONEncoder[T any](fn func(T) (interface{}, bool)) func(interface{}) (interface{}, bool) {
	if fn == nil {
		return nil
	}
	return func(v interface{}) (interface{}, bool) {
		val, ok := v.(T)
		if !ok {
			return nil, false
		}
		return fn(val)
	}
}

func lookupFieldEncoder(val interface{}) (fieldEncoder, interface{}, bool) {
	if val == nil {
		return fieldEncoder{}, nil, false
	}
	t := reflect.TypeOf(val)
	encoderMu.RLock()
	enc, ok := fieldEncoders[t]
	if ok {
		encoderMu.RUnlock()
		return enc, val, true
	}
	if t.Kind() == reflect.Pointer {
		rv := reflect.ValueOf(val)
		if rv.IsNil() {
			encoderMu.RUnlock()
			return fieldEncoder{}, nil, false
		}
		t = t.Elem()
		enc, ok = fieldEncoders[t]
		if ok {
			encoderMu.RUnlock()
			return enc, rv.Elem().Interface(), true
		}
	}
	encoderMu.RUnlock()
	return fieldEncoder{}, nil, false
}

func encodeTextWithRegistry(val interface{}) (string, bool) {
	enc, v, ok := lookupFieldEncoder(val)
	if !ok || enc.text == nil {
		return "", false
	}
	return enc.text(v)
}

func encodeJSONWithRegistry(val interface{}) (interface{}, bool) {
	enc, v, ok := lookupFieldEncoder(val)
	if !ok || enc.json == nil {
		return nil, false
	}
	return enc.json(v)
}

// Hook observes log events.
type Hook interface {
	Fire(level LogLevel, message string, fields []Field)
}

type HookFunc func(level LogLevel, message string, fields []Field)

func (f HookFunc) Fire(level LogLevel, message string, fields []Field) {
	f(level, message, fields)
}

// StructuredFormatter allows formatters to customize field rendering.
type StructuredFormatter interface {
	FormatWithFields(level LogLevel, message string, fields []Field) string
}

// StructuredWriterFormatter allows direct writer formatting with fields.
type StructuredWriterFormatter interface {
	FormatWithFieldsTo(level LogLevel, message string, fields []Field, w io.Writer)
}

// StructuredArgsFormatter extends ArgsFormatter to handle structured fields.
type StructuredArgsFormatter interface {
	FormatArgsWithFields(level LogLevel, fields []Field, w io.Writer, v ...interface{})
}

type contextFieldsKey struct{}

// Sampler decides whether a log entry at the given level should be emitted.
type Sampler interface {
	Allow(level LogLevel, message string, fields []Field) bool
}

// SamplerFunc is an adapter to allow use of ordinary functions as samplers.
type SamplerFunc func(level LogLevel, message string, fields []Field) bool

// Allow calls f(level, message, fields).
func (f SamplerFunc) Allow(level LogLevel, message string, fields []Field) bool {
	return f(level, message, fields)
}

// EveryNSampler permits one out of every N log entries.
type EveryNSampler struct {
	n       int64
	counter atomic.Int64
}

// NewEveryNSampler returns a sampler that allows one entry out of every n calls.
func NewEveryNSampler(n int) Sampler {
	if n <= 1 {
		return SamplerFunc(func(LogLevel, string, []Field) bool { return true })
	}
	return &EveryNSampler{n: int64(n)}
}

// Allow returns true for the first call and every Nth call thereafter.
func (s *EveryNSampler) Allow(level LogLevel, message string, fields []Field) bool {
	val := s.counter.Add(1)
	if s.n <= 1 {
		return true
	}
	return val%s.n == 1
}

// AsyncOptions configure the asynchronous logging mode.
type AsyncOptions struct {
	QueueSize     int
	DropStrategy  DropStrategy
	BatchSize     int
	FlushInterval time.Duration
}

type logRequest struct {
	level      LogLevel
	message    string
	hasMessage bool
	fields     []Field
	args       []interface{}
}

// HookFunc adapts a function into a Hook.
func cloneFields(fields []Field) []Field {
	if len(fields) == 0 {
		return nil
	}
	cloned := make([]Field, len(fields))
	copy(cloned, fields)
	return cloned
}

func cloneArgs(args []interface{}) []interface{} {
	if len(args) == 0 {
		return nil
	}
	cloned := make([]interface{}, len(args))
	copy(cloned, args)
	return cloned
}

// WithField returns a new context with the provided field appended.
func WithField(ctx context.Context, field Field) context.Context {
	return WithFields(ctx, field)
}

// WithFields returns a new context with the provided fields appended.
func WithFields(ctx context.Context, fields ...Field) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	existing := ContextFields(ctx)
	total := len(existing) + len(fields)
	combined := make([]Field, 0, total)
	combined = append(combined, existing...)
	combined = append(combined, fields...)
	return context.WithValue(ctx, contextFieldsKey{}, combined)
}

// ContextFields retrieves any logging fields stored on the context.
func ContextFields(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}
	if fields, ok := ctx.Value(contextFieldsKey{}).([]Field); ok {
		cp := make([]Field, len(fields))
		copy(cp, fields)
		return cp
	}
	return nil
}

func defaultContextExtractor(ctx context.Context) []Field {
	return ContextFields(ctx)
}

// LogLevel represents the severity of the log message
type LogLevel int

// Log levels
const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Formatter defines an interface for formatting log messages
type Formatter interface {
	Format(level LogLevel, message string) string
}

// WriterFormatter allows writing formatted output directly to an io.Writer
type WriterFormatter interface {
	FormatTo(level LogLevel, message string, w io.Writer)
}

// ArgsFormatter allows formatting log arguments directly without building an intermediate string
type ArgsFormatter interface {
	FormatArgs(level LogLevel, w io.Writer, v ...interface{})
}

// Logger represents a logging instance
type writerHolder struct {
	w io.Writer
}

type formatterHolder struct {
	f Formatter
}

type extractorHolder struct {
	fn ContextExtractorFunc
}

type samplerHolder struct {
	fn Sampler
}

type ContextExtractorFunc func(context.Context) []Field

type Logger struct {
	level             atomic.Int32
	output            atomic.Pointer[writerHolder]
	formatter         atomic.Pointer[formatterHolder]
	sampler           atomic.Pointer[samplerHolder]
	hooksMu           sync.RWMutex
	hooks             []hookRegistration
	writeMu           sync.Mutex
	closer            io.Closer
	syncWrites        atomic.Bool
	extractor         atomic.Pointer[extractorHolder]
	includeStacktrace atomic.Bool

	asyncMu    sync.Mutex
	asyncWG    sync.WaitGroup
	asyncQueue atomic.Value // chan logRequest
	asyncOpts  AsyncOptions
	asyncDrops atomic.Int64
}

// NewLogger creates a new Logger instance
func NewLogger(output io.Writer, level LogLevel, formatter Formatter) *Logger {
	if formatter == nil {
		panic("logger: formatter cannot be nil")
	}
	if output == nil {
		output = io.Discard
	}
	logger := &Logger{}
	logger.level.Store(int32(level))
	logger.output.Store(&writerHolder{w: output})
	logger.formatter.Store(&formatterHolder{f: formatter})
	logger.syncWrites.Store(true)
	logger.extractor.Store(&extractorHolder{fn: defaultContextExtractor})
	logger.sampler.Store((*samplerHolder)(nil))
	logger.includeStacktrace.Store(false)
	logger.asyncQueue.Store((chan logRequest)(nil))
	return logger
}

// SetOutput changes the output destination for the logger
func (l *Logger) SetOutput(output io.Writer) {
	l.SetOutputWithCloser(output, nil)
}

// SetOutputWithCloser changes the output and associates a closer that will be
// invoked when the logger closes or when a subsequent SetOutput* call replaces
// the writer.
func (l *Logger) SetOutputWithCloser(output io.Writer, closer io.Closer) {
	if output == nil {
		output = io.Discard
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.closer != nil && l.closer != closer {
		l.closer.Close()
	}
	l.output.Store(&writerHolder{w: output})
	l.closer = closer
}

// SetOutputs configures the logger to write to multiple destinations.
func (l *Logger) SetOutputs(writers ...io.Writer) {
	switch len(writers) {
	case 0:
		l.SetOutput(io.Discard)
	case 1:
		l.SetOutput(writers[0])
	default:
		l.SetOutput(io.MultiWriter(writers...))
	}
}

// SetLevel changes the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level.Store(int32(level))
}

// SetFormatter allows changing the log message format
func (l *Logger) SetFormatter(formatter Formatter) {
	if formatter == nil {
		panic("logger: formatter cannot be nil")
	}
	l.formatter.Store(&formatterHolder{f: formatter})
}

// SetContextExtractor configures how context.Context values are converted into fields.
func (l *Logger) SetContextExtractor(fn ContextExtractorFunc) {
	if fn == nil {
		l.extractor.Store((*extractorHolder)(nil))
		return
	}
	l.extractor.Store(&extractorHolder{fn: fn})
}

// SetSampler installs a sampler that can drop log entries before formatting.
func (l *Logger) SetSampler(fn Sampler) {
	if fn == nil {
		l.sampler.Store((*samplerHolder)(nil))
		return
	}
	l.sampler.Store(&samplerHolder{fn: fn})
}

// SetIncludeStacktrace toggles automatic stacktrace capture for error/fatal logs.
func (l *Logger) SetIncludeStacktrace(enabled bool) {
	l.includeStacktrace.Store(enabled)
}

// EnableAsync activates asynchronous logging with the provided options.
func (l *Logger) EnableAsync(opts AsyncOptions) {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1024
	}
	if opts.DropStrategy != DropNew && opts.DropStrategy != DropOldest && opts.DropStrategy != BlockWhenFull {
		opts.DropStrategy = DropNew
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	if opts.FlushInterval < 0 {
		opts.FlushInterval = 0
	}

	l.asyncMu.Lock()
	defer l.asyncMu.Unlock()
	l.disableAsyncLocked()

	ch := make(chan logRequest, opts.QueueSize)
	l.asyncOpts = opts
	l.asyncQueue.Store(ch)
	l.asyncWG.Add(1)
	go l.asyncWorker(ch)
}

// SetDropStrategy changes the behaviour when the async queue is full.
func (l *Logger) SetDropStrategy(strategy DropStrategy) {
	l.asyncMu.Lock()
	l.asyncOpts.DropStrategy = strategy
	l.asyncMu.Unlock()
}

// DisableAsync stops asynchronous logging and flushes pending entries.
func (l *Logger) DisableAsync() {
	l.asyncMu.Lock()
	l.disableAsyncLocked()
	l.asyncMu.Unlock()
}

func (l *Logger) disableAsyncLocked() {
	value := l.asyncQueue.Load()
	if value == nil {
		return
	}
	ch := value.(chan logRequest)
	if ch == nil {
		return
	}
	l.asyncQueue.Store((chan logRequest)(nil))
	close(ch)
	l.asyncWG.Wait()
}

func (l *Logger) asyncWorker(ch chan logRequest) {
	defer l.asyncWG.Done()

	opts := l.asyncOpts
	batchSize := opts.BatchSize
	if batchSize <= 1 && opts.FlushInterval <= 0 {
		for req := range ch {
			l.logEntrySync(req.level, req.message, req.hasMessage, req.fields, req.args)
		}
		return
	}

	if batchSize <= 0 {
		batchSize = 1
	}

	flushInterval := opts.FlushInterval
	batch := make([]logRequest, 0, batchSize)

	var timer *time.Timer
	startTimer := func() {
		if flushInterval <= 0 {
			return
		}
		if timer == nil {
			timer = time.NewTimer(flushInterval)
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(flushInterval)
	}

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}

	flush := func() {
		for _, req := range batch {
			l.logEntrySync(req.level, req.message, req.hasMessage, req.fields, req.args)
		}
		batch = batch[:0]
		if flushInterval > 0 {
			stopTimer()
		}
	}

	for {
		var timeout <-chan time.Time
		if flushInterval > 0 && len(batch) > 0 && timer != nil {
			timeout = timer.C
		}

		select {
		case req, ok := <-ch:
			if !ok {
				if len(batch) > 0 {
					flush()
				}
				if timer != nil {
					stopTimer()
				}
				return
			}
			batch = append(batch, req)
			if len(batch) >= batchSize {
				flush()
				continue
			}
			if flushInterval > 0 && len(batch) == 1 {
				startTimer()
			}
		case <-timeout:
			if len(batch) > 0 {
				flush()
			}
		}
	}
}

func (l *Logger) hooksSnapshot() []hookRegistration {
	l.hooksMu.RLock()
	defer l.hooksMu.RUnlock()
	if len(l.hooks) == 0 {
		return nil
	}
	snapshot := make([]hookRegistration, len(l.hooks))
	copy(snapshot, l.hooks)
	return snapshot
}

// AddHook registers a hook that will be fired for every emitted log entry.
func (l *Logger) AddHook(h Hook, opts ...HookOption) {
	if h == nil {
		return
	}
	reg := hookRegistration{target: h}
	for _, opt := range opts {
		opt.apply(&reg)
	}
	l.hooksMu.Lock()
	l.hooks = append(l.hooks, reg)
	l.hooksMu.Unlock()
}

// ClearHooks removes all registered hooks.
func (l *Logger) ClearHooks() {
	l.hooksMu.Lock()
	l.hooks = nil
	l.hooksMu.Unlock()
}

// SetSynchronized toggles serialized writes. When disabled, callers must ensure the
// writer they provide is safe for concurrent use.
func (l *Logger) SetSynchronized(enabled bool) {
	l.syncWrites.Store(enabled)
}

// Synchronized reports whether the logger currently serializes writes.
func (l *Logger) Synchronized() bool {
	return l.syncWrites.Load()
}

func (l *Logger) contextFields(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}
	holder := l.extractor.Load()
	if holder == nil || holder.fn == nil {
		return nil
	}
	fields := holder.fn(ctx)
	if len(fields) == 0 {
		return nil
	}
	cp := make([]Field, len(fields))
	copy(cp, fields)
	return cp
}

func (l *Logger) mergeContextFields(ctx context.Context, fields []Field) []Field {
	ctxFields := l.contextFields(ctx)
	if len(ctxFields) == 0 {
		return fields
	}
	if len(fields) == 0 {
		return ctxFields
	}
	combined := make([]Field, 0, len(ctxFields)+len(fields))
	combined = append(combined, ctxFields...)
	combined = append(combined, fields...)
	return combined
}

var levelStrings = [...]string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
var levelBytes = [...][]byte{
	[]byte("DEBUG"),
	[]byte("INFO"),
	[]byte("WARN"),
	[]byte("ERROR"),
	[]byte("FATAL"),
}

var levelColors = map[LogLevel]string{
	DEBUG: "\033[36m", // Cyan
	INFO:  "\033[32m", // Green
	WARN:  "\033[33m", // Yellow
	ERROR: "\033[31m", // Red
	FATAL: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

const packagePrefix = "github.com/pod32g/simple-logger."

type callerEntry struct {
	file string
	line int
	skip bool
}

var callerCache sync.Map

// logLevelToString converts a LogLevel to its string representation
func logLevelToString(level LogLevel) string {
	if level >= 0 && int(level) < len(levelStrings) {
		return levelStrings[level]
	}
	return "UNKNOWN"
}

// DefaultFormatter is a simple text-based log message formatter
// DefaultFormatter is a simple text-based log message formatter.
// The IncludeCaller flag controls whether file and line information
// is added to each log entry. Including caller information is
// convenient for debugging but adds overhead, so it can be disabled
// for better performance.
type DefaultFormatter struct {
	IncludeCaller bool
	Colorize      bool
	TimeLayout    string
}

func appendTwoDigitsSlice(b []byte, val int) []byte {
	return append(b, byte('0'+val/10), byte('0'+val%10))
}

func appendFourDigitsSlice(b []byte, val int) []byte {
	return append(b,
		byte('0'+val/1000),
		byte('0'+val/100%10),
		byte('0'+val/10%10),
		byte('0'+val%10))
}

func appendTimestampSlice(b []byte, t time.Time) []byte {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	b = appendFourDigitsSlice(b, y)
	b = append(b, '-')
	b = appendTwoDigitsSlice(b, int(m))
	b = append(b, '-')
	b = appendTwoDigitsSlice(b, d)
	b = append(b, ' ')
	b = appendTwoDigitsSlice(b, hh)
	b = append(b, ':')
	b = appendTwoDigitsSlice(b, mm)
	b = append(b, ':')
	b = appendTwoDigitsSlice(b, ss)
	return b
}

func basename(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func (f *DefaultFormatter) writeFrame(level LogLevel, w io.Writer, messageWriter func(io.Writer)) {
	var tmp [128]byte
	b := tmp[:0]
	now := time.Now()
	if f.TimeLayout != "" {
		b = append(b, now.Format(f.TimeLayout)...)
	} else {
		b = appendTimestampSlice(b, now)
	}
	if f.IncludeCaller {
		file, line := resolveCaller(0)
		b = append(b, ' ', '-', ' ')
		b = append(b, file...)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(line), 10)
	}
	b = append(b, ' ', '-', ' ', '[')
	var color string
	if f.Colorize {
		color = levelColors[level]
		if color != "" {
			b = append(b, color...)
		}
	}
	if level >= 0 && int(level) < len(levelBytes) {
		b = append(b, levelBytes[level]...)
	} else {
		b = append(b, "UNKNOWN"...)
	}
	if color != "" {
		b = append(b, colorReset...)
	}
	b = append(b, ']', ' ')
	w.Write(b)
	messageWriter(w)
	w.Write([]byte{'\n'})
}

func (f *DefaultFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.FormatWithFieldsTo(level, message, nil, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *DefaultFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	f.writeFrame(level, w, func(writer io.Writer) {
		io.WriteString(writer, message)
	})
}

func (f *DefaultFormatter) FormatWithFields(level LogLevel, message string, fields []Field) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.FormatWithFieldsTo(level, message, fields, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *DefaultFormatter) FormatWithFieldsTo(level LogLevel, message string, fields []Field, w io.Writer) {
	f.writeFrame(level, w, func(writer io.Writer) {
		io.WriteString(writer, message)
		writeFieldsText(writer, fields, message != "")
	})
}

// FormatArgs implements ArgsFormatter for DefaultFormatter to avoid intermediate string allocations
func (f *DefaultFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	f.writeFrame(level, w, func(writer io.Writer) {
		for i, val := range v {
			if i > 0 {
				writer.Write([]byte{' '})
			}
			writeFieldValueText(writer, val)
		}
	})
}

func (f *DefaultFormatter) FormatArgsWithFields(level LogLevel, fields []Field, w io.Writer, v ...interface{}) {
	f.writeFrame(level, w, func(writer io.Writer) {
		wrote := false
		for i, val := range v {
			if i > 0 {
				writer.Write([]byte{' '})
			}
			writeFieldValueText(writer, val)
			wrote = true
		}
		writeFieldsText(writer, fields, wrote)
	})
}

// JSONFormatter formats log messages as JSON
// JSONFormatter formats log messages as JSON. The IncludeCaller flag controls
// whether caller information is included in the output.
type JSONFormatter struct {
	IncludeCaller bool
	TimeLayout    string
}

func (f *JSONFormatter) formatBuffer(level LogLevel, message string, fields []Field, buf *bytes.Buffer) {
	buf.WriteString(`{"timestamp":"`)
	if f.TimeLayout != "" {
		buf.WriteString(time.Now().Format(f.TimeLayout))
	} else {
		buf.WriteString(time.Now().Format(time.RFC3339))
	}
	buf.WriteString(`","level":"`)
	buf.WriteString(logLevelToString(level))
	buf.WriteString(`","message":`)
	buf.WriteString(strconv.Quote(message))
	if f.IncludeCaller {
		file, line := resolveCaller(0)
		buf.WriteString(`,"file":`)
		buf.WriteString(strconv.Quote(file))
		buf.WriteString(`,"line":`)
		buf.WriteString(strconv.Itoa(line))
	}
	appendJSONFields(buf, fields)
	buf.WriteString("}\n")
}

func (f *JSONFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, nil, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *JSONFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, nil, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// FormatArgs implements ArgsFormatter for JSONFormatter. It joins the arguments
// into a message before delegating to FormatTo.
func (f *JSONFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	msg := buildMessage(v...)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, msg, nil, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

func (f *JSONFormatter) FormatWithFields(level LogLevel, message string, fields []Field) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, fields, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *JSONFormatter) FormatWithFieldsTo(level LogLevel, message string, fields []Field, w io.Writer) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, fields, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

func (f *JSONFormatter) FormatArgsWithFields(level LogLevel, fields []Field, w io.Writer, v ...interface{}) {
	msg := buildMessage(v...)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, msg, fields, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// log logs a message using the current formatter
func (l *Logger) log(level LogLevel, v ...interface{}) {
	l.logEntry(level, "", false, nil, v)
}

func (l *Logger) logEntry(level LogLevel, message string, hasMessage bool, fields []Field, args []interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}

	if holder := l.sampler.Load(); holder != nil && holder.fn != nil {
		effective := message
		if !hasMessage && len(args) > 0 {
			effective = buildMessage(args...)
		}
		if !holder.fn.Allow(level, effective, fields) {
			return
		}
	}

	if ch := l.asyncChannel(); ch != nil {
		req := logRequest{
			level:      level,
			message:    message,
			hasMessage: hasMessage,
			fields:     cloneFields(fields),
		}
		if len(args) > 0 {
			req.args = cloneArgs(args)
		}
		if l.enqueueAsync(req) {
			return
		}
	}

	l.logEntrySync(level, message, hasMessage, fields, args)
}

func (l *Logger) logEntrySync(level LogLevel, message string, hasMessage bool, fields []Field, args []interface{}) {
	fh := l.formatter.Load()
	if fh == nil || fh.f == nil {
		return
	}
	formatter := fh.f

	wh := l.output.Load()
	var writer io.Writer
	if wh != nil {
		writer = wh.w
	}
	if writer == nil {
		if level == FATAL {
			os.Exit(1)
		}
		return
	}

	if l.includeStacktrace.Load() && level >= ERROR {
		hasStack := false
		for _, f := range fields {
			if f.Key == "stacktrace" {
				hasStack = true
				break
			}
		}
		if !hasStack {
			fields = append(fields, String("stacktrace", string(debug.Stack())))
		}
	}

	hooks := l.hooksSnapshot()

	write := func(fn func(io.Writer)) {
		if l.syncWrites.Load() {
			l.writeMu.Lock()
			fn(writer)
			if level == FATAL {
				l.writeMu.Unlock()
				os.Exit(1)
			}
			l.writeMu.Unlock()
		} else {
			fn(writer)
			if level == FATAL {
				os.Exit(1)
			}
		}
	}

	if len(fields) == 0 && len(args) > 0 {
		if af, ok := formatter.(ArgsFormatter); ok {
			write(func(w io.Writer) {
				af.FormatArgs(level, w, args...)
			})
			return
		}
	}

	resolvedMessage := message
	if !hasMessage {
		if len(args) > 0 {
			resolvedMessage = buildMessage(args...)
		} else {
			resolvedMessage = ""
		}
	}

	if len(hooks) > 0 {
		for _, reg := range hooks {
			if reg.levels != nil {
				if _, ok := reg.levels[level]; !ok {
					continue
				}
			}
			if reg.fieldsFilter != nil && !reg.fieldsFilter(fields) {
				continue
			}
			if reg.filter != nil && !reg.filter(level, resolvedMessage, fields) {
				continue
			}
			reg.target.Fire(level, resolvedMessage, fields)
		}
	}

	if len(fields) > 0 {
		if sfw, ok := formatter.(StructuredWriterFormatter); ok {
			write(func(w io.Writer) {
				sfw.FormatWithFieldsTo(level, resolvedMessage, fields, w)
			})
			return
		}
		if sf, ok := formatter.(StructuredFormatter); ok {
			formatted := sf.FormatWithFields(level, resolvedMessage, fields)
			write(func(w io.Writer) {
				io.WriteString(w, formatted)
			})
			return
		}

		msgWithFields := appendFieldsToMessage(resolvedMessage, fields)
		if wf, ok := formatter.(WriterFormatter); ok {
			write(func(w io.Writer) {
				wf.FormatTo(level, msgWithFields, w)
			})
			return
		}

		formatted := formatter.Format(level, msgWithFields)
		write(func(w io.Writer) {
			io.WriteString(w, formatted)
		})
		return
	}

	if wf, ok := formatter.(WriterFormatter); ok {
		write(func(w io.Writer) {
			wf.FormatTo(level, resolvedMessage, w)
		})
		return
	}

	formatted := formatter.Format(level, resolvedMessage)
	write(func(w io.Writer) {
		io.WriteString(w, formatted)
	})
}

// Close releases any resources owned by the logger, such as open files.
func (l *Logger) Close() error {
	l.DisableAsync()
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.closer != nil {
		err := l.closer.Close()
		l.closer = nil
		return err
	}
	return nil
}

func resolveCaller(extraSkip int) (string, int) {
	const depth = 12
	var pcs [depth]uintptr
	n := runtime.Callers(3+extraSkip, pcs[:])
	if n == 0 {
		return "unknown", 0
	}
	for _, pc := range pcs[:n] {
		if pc == 0 {
			continue
		}
		if entry, ok := callerCache.Load(pc); ok {
			ce := entry.(callerEntry)
			if ce.skip {
				continue
			}
			if ce.file != "" {
				return ce.file, ce.line
			}
		}

		fn := runtime.FuncForPC(pc)
		if fn == nil {
			callerCache.Store(pc, callerEntry{skip: true})
			continue
		}

		if strings.HasPrefix(fn.Name(), packagePrefix) {
			callerCache.Store(pc, callerEntry{skip: true})
			continue
		}

		file, line := fn.FileLine(pc - 1)
		entry := callerEntry{
			file: basename(file),
			line: line,
		}
		callerCache.Store(pc, entry)
		return entry.file, entry.line
	}
	return "unknown", 0
}

func buildMessage(v ...interface{}) string {
	if len(v) == 1 {
		switch t := v[0].(type) {
		case string:
			return t
		case int:
			return strconv.Itoa(t)
		case fmt.Stringer:
			return t.String()
		default:
			return fmt.Sprint(t)
		}
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	for i, val := range v {
		if i > 0 {
			b.WriteByte(' ')
		}
		writeValue(b, val)
	}
	s := b.String()
	builderPool.Put(b)
	return s
}

func appendFieldsToMessage(message string, fields []Field) string {
	if len(fields) == 0 {
		return message
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	if message != "" {
		b.WriteString(message)
		writeFieldsText(b, fields, true)
	} else {
		writeFieldsText(b, fields, false)
	}
	result := b.String()
	builderPool.Put(b)
	return result
}

func writeFieldsText(w io.Writer, fields []Field, prefixSpace bool) {
	for i, field := range fields {
		if prefixSpace || i > 0 {
			w.Write([]byte{' '})
		}
		io.WriteString(w, field.Key)
		w.Write([]byte{'='})
		writeFieldValueText(w, field.Value)
	}
}

func writeFieldValueText(w io.Writer, val interface{}) {
	if s, ok := encodeTextWithRegistry(val); ok {
		io.WriteString(w, s)
		return
	}
	switch v := val.(type) {
	case string:
		io.WriteString(w, v)
	case int:
		var buf [20]byte
		w.Write(strconv.AppendInt(buf[:0], int64(v), 10))
	case int64:
		var buf [20]byte
		w.Write(strconv.AppendInt(buf[:0], v, 10))
	case uint:
		var buf [20]byte
		w.Write(strconv.AppendUint(buf[:0], uint64(v), 10))
	case uint64:
		var buf [20]byte
		w.Write(strconv.AppendUint(buf[:0], v, 10))
	case float64:
		var buf [64]byte
		w.Write(strconv.AppendFloat(buf[:0], v, 'f', -1, 64))
	case float32:
		var buf [64]byte
		w.Write(strconv.AppendFloat(buf[:0], float64(v), 'f', -1, 32))
	case bool:
		if v {
			w.Write([]byte("true"))
		} else {
			w.Write([]byte("false"))
		}
	case fmt.Stringer:
		io.WriteString(w, v.String())
	case error:
		io.WriteString(w, v.Error())
	default:
		fmt.Fprint(w, v)
	}
}

func appendJSONFields(buf *bytes.Buffer, fields []Field) {
	for _, field := range fields {
		buf.WriteString(`,"`)
		buf.WriteString(field.Key)
		buf.WriteString(`":`)
		appendJSONValue(buf, field.Value)
	}
}

func appendJSONValue(buf *bytes.Buffer, val interface{}) {
	if encoded, ok := encodeJSONWithRegistry(val); ok {
		switch v := encoded.(type) {
		case string:
			buf.WriteString(strconv.Quote(v))
		case []byte:
			buf.WriteString(strconv.Quote(string(v)))
		default:
			if data, err := json.Marshal(v); err == nil {
				buf.Write(data)
			} else {
				buf.WriteString(strconv.Quote(fmt.Sprint(v)))
			}
		}
		return
	}
	switch v := val.(type) {
	case string:
		buf.WriteString(strconv.Quote(v))
		return
	case int:
		buf.WriteString(strconv.Itoa(v))
		return
	case int64:
		buf.WriteString(strconv.FormatInt(v, 10))
		return
	case uint:
		buf.WriteString(strconv.FormatUint(uint64(v), 10))
		return
	case uint64:
		buf.WriteString(strconv.FormatUint(v, 10))
		return
	case float64:
		buf.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		return
	case float32:
		buf.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
		return
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return
	case fmt.Stringer:
		buf.WriteString(strconv.Quote(v.String()))
		return
	case error:
		buf.WriteString(strconv.Quote(v.Error()))
		return
	case json.Marshaler:
		if data, err := v.MarshalJSON(); err == nil {
			buf.Write(data)
			return
		}
	}
	if data, err := json.Marshal(val); err == nil {
		buf.Write(data)
		return
	}
	buf.WriteString(strconv.Quote(fmt.Sprint(val)))
}

func writeValue(b *strings.Builder, val interface{}) {
	switch t := val.(type) {
	case string:
		b.WriteString(t)
	case int:
		b.WriteString(strconv.Itoa(t))
	case fmt.Stringer:
		b.WriteString(t.String())
	default:
		fmt.Fprint(b, val)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(v ...interface{}) {
	l.log(DEBUG, v...)
}

// DebugString logs a preformatted debug string without variadic allocation.
func (l *Logger) DebugString(message string) {
	l.log(DEBUG, message)
}

// DebugFields logs a debug message with structured fields.
func (l *Logger) DebugFields(message string, fields ...Field) {
	l.logEntry(DEBUG, message, true, fields, nil)
}

// Debug1 logs a debug message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Debug1(message string, value interface{}) {
	l.log(DEBUG, message, value)
}

// DebugContext logs a debug message while enriching it with context-derived fields.
func (l *Logger) DebugContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(DEBUG, message, true, l.mergeContextFields(ctx, fields), nil)
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	l.log(INFO, v...)
}

// InfoString logs a preformatted info string without variadic allocation.
func (l *Logger) InfoString(message string) {
	l.log(INFO, message)
}

// Info1 logs an info message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Info1(message string, value interface{}) {
	l.log(INFO, message, value)
}

// InfoFields logs an info message with structured fields.
func (l *Logger) InfoFields(message string, fields ...Field) {
	l.logEntry(INFO, message, true, fields, nil)
}

// InfoContext logs an info message and appends any fields extracted from context.
func (l *Logger) InfoContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(INFO, message, true, l.mergeContextFields(ctx, fields), nil)
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	l.log(WARN, v...)
}

// WarnString logs a preformatted warning string without variadic allocation.
func (l *Logger) WarnString(message string) {
	l.log(WARN, message)
}

// Warn1 logs a warning message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Warn1(message string, value interface{}) {
	l.log(WARN, message, value)
}

// WarnFields logs a warning message with structured fields.
func (l *Logger) WarnFields(message string, fields ...Field) {
	l.logEntry(WARN, message, true, fields, nil)
}

// WarnContext logs a warning and appends context-derived fields.
func (l *Logger) WarnContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(WARN, message, true, l.mergeContextFields(ctx, fields), nil)
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	l.log(ERROR, v...)
}

// ErrorString logs a preformatted error string without variadic allocation.
func (l *Logger) ErrorString(message string) {
	l.log(ERROR, message)
}

// Error1 logs an error message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Error1(message string, value interface{}) {
	l.log(ERROR, message, value)
}

// ErrorFields logs an error message with structured fields.
func (l *Logger) ErrorFields(message string, fields ...Field) {
	l.logEntry(ERROR, message, true, fields, nil)
}

// ErrorContext logs an error and appends context-derived fields.
func (l *Logger) ErrorContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(ERROR, message, true, l.mergeContextFields(ctx, fields), nil)
}

// Fatal logs a fatal message and exits the application
func (l *Logger) Fatal(v ...interface{}) {
	l.log(FATAL, v...)
}

// FatalString logs a preformatted fatal string without variadic allocation.
func (l *Logger) FatalString(message string) {
	l.log(FATAL, message)
}

// Fatal1 logs a fatal message composed of a string and one value without
// triggering variadic allocations.
func (l *Logger) Fatal1(message string, value interface{}) {
	l.log(FATAL, message, value)
}

// FatalFields logs a fatal message with structured fields and exits the application.
func (l *Logger) FatalFields(message string, fields ...Field) {
	l.logEntry(FATAL, message, true, fields, nil)
}

// FatalContext logs a fatal message with context-derived fields and exits the application.
func (l *Logger) FatalContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(FATAL, message, true, l.mergeContextFields(ctx, fields), nil)
}

func init() {
	RegisterFieldEncoder[time.Duration](
		func(d time.Duration) (string, bool) { return d.String(), true },
		func(d time.Duration) (interface{}, bool) { return d.String(), true },
	)
	RegisterFieldEncoder[time.Time](
		func(t time.Time) (string, bool) { return t.Format(time.RFC3339), true },
		func(t time.Time) (interface{}, bool) { return t.Format(time.RFC3339), true },
	)
	RegisterFieldEncoder[error](
		func(err error) (string, bool) { return err.Error(), true },
		func(err error) (interface{}, bool) { return err.Error(), true },
	)
}

func (l *Logger) asyncChannel() chan logRequest {
	if ch, _ := l.asyncQueue.Load().(chan logRequest); ch != nil {
		return ch
	}
	return nil
}

func (l *Logger) enqueueAsync(req logRequest) bool {
	ch := l.asyncChannel()
	if ch == nil {
		return false
	}
	switch l.asyncOpts.DropStrategy {
	case DropOldest:
		select {
		case ch <- req:
			return true
		default:
			select {
			case <-ch:
				l.asyncDrops.Add(1)
			default:
			}
			select {
			case ch <- req:
			default:
			}
			return true
		}
	case BlockWhenFull:
		ch <- req
		return true
	default:
		select {
		case ch <- req:
			return true
		default:
			l.asyncDrops.Add(1)
			return true
		}
	}
}

// AsyncStats returns counters for the async queue.
func (l *Logger) AsyncStats() AsyncStats {
	ch := l.asyncChannel()
	stats := AsyncStats{}
	if ch != nil {
		stats.QueueSize = cap(ch)
		stats.QueueLength = len(ch)
	}
	stats.Dropped = l.asyncDrops.Load()
	return stats
}
