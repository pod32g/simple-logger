package log

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
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

// ErrorVerbose captures the full error including its wrapped cause chain and, for
// errors that carry one (e.g. github.com/pkg/errors), the originating stack via
// the %+v verb. Use it instead of Error when you need the chain, not just the
// flattened message.
func ErrorVerbose(key string, err error) Field {
	if err == nil {
		return Field{Key: key, Value: nil}
	}
	return Field{Key: key, Value: fmt.Sprintf("%+v", err)}
}

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

// NewBurstSampler allows the first `first` matching entries within each `window`
// and then one of every `thereafter` thereafter, keyed by level+message. It
// keeps the first occurrences of an error storm visible while rate-limiting the
// flood. A thereafter of 0 suppresses everything after the burst.
func NewBurstSampler(window time.Duration, first, thereafter int) Sampler {
	return &burstSampler{
		window:     window,
		first:      int64(first),
		thereafter: int64(thereafter),
		buckets:    make(map[string]*burstBucket),
	}
}

// burstSweepThreshold is the bucket count above which Allow sweeps expired
// entries before adding another. Messages are routinely unique -- they carry
// request IDs, URLs, wrapped error text -- and a bucket holds the message as its
// key, so without a sweep the sampler installed to survive an error storm grows
// for as long as the storm lasts.
const burstSweepThreshold = 1024

type burstBucket struct {
	windowStart time.Time
	count       int64
}

type burstSampler struct {
	window     time.Duration
	first      int64
	thereafter int64
	mu         sync.Mutex
	buckets    map[string]*burstBucket
}

func (b *burstSampler) Allow(level LogLevel, message string, _ []Field) bool {
	key := level.String() + ":" + message
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	bk := b.buckets[key]
	if bk == nil || now.Sub(bk.windowStart) >= b.window {
		if bk == nil && len(b.buckets) >= burstSweepThreshold {
			b.sweepLocked(now)
		}
		bk = &burstBucket{windowStart: now}
		b.buckets[key] = bk
	}
	bk.count++
	if bk.count <= b.first {
		return true
	}
	if b.thereafter <= 0 {
		return false
	}
	return (bk.count-b.first)%b.thereafter == 0
}

// sweepLocked drops buckets whose window has closed; they can only ever be
// replaced by a fresh one, so keeping them holds a message string for nothing.
// If every bucket is still live the map is reset instead: the alternative is to
// keep growing, and rebuilding rate limits is cheaper than running out of
// memory. b.mu must be held.
func (b *burstSampler) sweepLocked(now time.Time) {
	for key, bucket := range b.buckets {
		if now.Sub(bucket.windowStart) >= b.window {
			delete(b.buckets, key)
		}
	}
	if len(b.buckets) >= burstSweepThreshold {
		b.buckets = make(map[string]*burstBucket)
	}
}

// NewLevelSampler applies a distinct sampler per level. Levels absent from the
// map are always allowed, so you can pass ERROR/FATAL through untouched while
// sampling chatty DEBUG/INFO.
func NewLevelSampler(samplers map[LogLevel]Sampler) Sampler {
	cp := make(map[LogLevel]Sampler, len(samplers))
	for k, v := range samplers {
		cp[k] = v
	}
	return &levelSampler{samplers: cp}
}

type levelSampler struct{ samplers map[LogLevel]Sampler }

func (l *levelSampler) Allow(level LogLevel, message string, fields []Field) bool {
	if s, ok := l.samplers[level]; ok && s != nil {
		return s.Allow(level, message, fields)
	}
	return true
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
	caller     *Caller       // call site resolved by the producer, nil to resolve at format time
	flush      chan struct{} // non-nil marks a Flush barrier rather than an entry
}

// Caller is a resolved source location.
type Caller struct {
	File string
	Line int
}

// CallerAwareFormatter is an optional Formatter extension for formatters that
// render caller information. The logger resolves the call site on the goroutine
// that logged (it has to: an async entry is formatted on the worker, whose stack
// says nothing about who logged) and passes it here instead of letting the
// formatter walk an unrelated stack. Formatters that do not implement it still
// work; they just resolve their own caller, which is only meaningful in
// synchronous mode.
type CallerAwareFormatter interface {
	// IncludeCallerInfo reports whether the formatter renders the call site, so
	// the logger can skip resolving one that would be thrown away.
	IncludeCallerInfo() bool
	// FormatWithCallerTo renders an entry using the supplied call site.
	FormatWithCallerTo(level LogLevel, message string, fields []Field, caller Caller, w io.Writer)
}

// Redactor transforms fields before they are formatted or dispatched to hooks,
// e.g. to mask sensitive values. Implementations must not mutate the input
// slice in place; return the original slice unchanged when nothing is redacted.
type Redactor interface {
	Redact(fields []Field) []Field
}

// MessageRedactor is an optional Redactor extension that also scrubs the
// resolved message string.
type MessageRedactor interface {
	Redactor
	RedactMessage(message string) string
}

type redactorHolder struct{ r Redactor }
type errHandlerHolder struct{ fn func(error) }

// errCapturingWriter remembers the first error returned by the underlying
// writer so the logging path can report a failed sink write.
type errCapturingWriter struct {
	w   io.Writer
	err error
}

func (e *errCapturingWriter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil && e.err == nil {
		e.err = err
	}
	return n, err
}

// redactedPlaceholder is substituted for redacted field values.
const redactedPlaceholder = "[REDACTED]"

// NewKeyRedactor returns a Redactor that replaces the value of any field whose
// key matches (case-insensitively) one of keys with "[REDACTED]". Use it to
// keep credentials, tokens, and PII out of logs and downstream sinks.
func NewKeyRedactor(keys ...string) Redactor {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[strings.ToLower(k)] = struct{}{}
	}
	return &keyRedactor{keys: set}
}

type keyRedactor struct{ keys map[string]struct{} }

func (k *keyRedactor) Redact(fields []Field) []Field {
	var out []Field
	for i := range fields {
		if _, ok := k.keys[strings.ToLower(fields[i].Key)]; ok {
			if out == nil {
				out = append(make([]Field, 0, len(fields)), fields[:i]...)
			}
			out = append(out, Field{Key: fields[i].Key, Value: redactedPlaceholder})
		} else if out != nil {
			out = append(out, fields[i])
		}
	}
	if out == nil {
		return fields
	}
	return out
}

// NewPatternScrubber returns a Redactor that replaces every substring matching
// any of patterns with "[REDACTED]", in string-valued fields and (via
// RedactMessage) in the resolved message. Use it as defense-in-depth against
// secrets embedded in free-form text. It implements MessageRedactor.
func NewPatternScrubber(patterns ...*regexp.Regexp) MessageRedactor {
	return &patternScrubber{patterns: patterns}
}

type patternScrubber struct{ patterns []*regexp.Regexp }

func (p *patternScrubber) scrub(s string) (string, bool) {
	out := s
	for _, re := range p.patterns {
		if re != nil {
			out = re.ReplaceAllString(out, redactedPlaceholder)
		}
	}
	return out, out != s
}

func (p *patternScrubber) Redact(fields []Field) []Field {
	var out []Field
	for i := range fields {
		if sv, ok := fields[i].Value.(string); ok {
			if ns, changed := p.scrub(sv); changed {
				if out == nil {
					out = append(make([]Field, 0, len(fields)), fields[:i]...)
				}
				out = append(out, Field{Key: fields[i].Key, Value: ns})
				continue
			}
		}
		if out != nil {
			out = append(out, fields[i])
		}
	}
	if out == nil {
		return fields
	}
	return out
}

func (p *patternScrubber) RedactMessage(message string) string {
	s, _ := p.scrub(message)
	return s
}

func dedupeFieldsLastWins(fields []Field) []Field {
	if len(fields) < 2 {
		return fields
	}
	// Detect duplicates cheaply before allocating.
	seen := make(map[string]int, len(fields))
	dup := false
	for _, f := range fields {
		if _, ok := seen[f.Key]; ok {
			dup = true
		}
		seen[f.Key] = 0
	}
	if !dup {
		return fields
	}
	order := make([]string, 0, len(seen))
	idx := make(map[string]int, len(seen))
	for i, f := range fields {
		if _, ok := idx[f.Key]; !ok {
			order = append(order, f.Key)
		}
		idx[f.Key] = i
	}
	out := make([]Field, 0, len(order))
	for _, key := range order {
		out = append(out, fields[idx[key]])
	}
	return out
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	// Back the cut off to a rune boundary. Slicing mid-rune leaves a partial
	// UTF-8 sequence, which renders as mojibake in text output and has to be
	// escaped as a replacement character in JSON.
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("...[+%d bytes]", len(s)-cut)
}

func truncateFieldValues(fields []Field, max int) []Field {
	var out []Field
	for i := range fields {
		if sv, ok := fields[i].Value.(string); ok && len(sv) > max {
			if out == nil {
				out = append(make([]Field, 0, len(fields)), fields[:i]...)
			}
			out = append(out, Field{Key: fields[i].Key, Value: truncateString(sv, max)})
			continue
		}
		if out != nil {
			out = append(out, fields[i])
		}
	}
	if out == nil {
		return fields
	}
	return out
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

// RequestIDKey is the field key used for request/correlation IDs.
const RequestIDKey = "request_id"

// WithRequestID attaches a request/correlation ID to the context. It is emitted
// as the "request_id" field on every *Context log call, and is read back with
// RequestID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return WithField(ctx, String(RequestIDKey, id))
}

// RequestID returns the request ID attached via WithRequestID, or "".
func RequestID(ctx context.Context) string {
	for _, f := range ContextFields(ctx) {
		if f.Key == RequestIDKey {
			if s, ok := f.Value.(string); ok {
				return s
			}
		}
	}
	return ""
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

// String returns the upper-case name of the level (e.g. "INFO"), or "UNKNOWN".
func (l LogLevel) String() string {
	return logLevelToString(l)
}

// ParseLevel converts a level name (case-insensitive: debug, info, warn, error,
// fatal) to a LogLevel. It returns an error for an unrecognized name.
func ParseLevel(s string) (LogLevel, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return DEBUG, nil
	case "INFO":
		return INFO, nil
	case "WARN", "WARNING":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	case "FATAL":
		return FATAL, nil
	default:
		return INFO, fmt.Errorf("unknown log level %q", s)
	}
}

// MarshalJSON encodes the level as its lower-case name, so a config written by
// this package reads the way an operator would write one by hand.
func (l LogLevel) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strings.ToLower(l.String()) + `"`), nil
}

// UnmarshalJSON accepts either a level name ("debug", "WARN", "warning") or the
// numeric value. Names are what every other entry point takes -- LOG_LEVEL, the
// HTTP level endpoint -- so a JSON config that spells the level out should not
// be the one place that rejects it.
func (l *LogLevel) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		lvl, err := ParseLevel(name)
		if err != nil {
			return err
		}
		*l = lvl
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("log level must be a name or a number: %w", err)
	}
	if n < int(DEBUG) || n > int(FATAL) {
		return fmt.Errorf("log level %d out of range", n)
	}
	*l = LogLevel(n)
	return nil
}

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

// Logger writes log entries. Derived loggers created with With share the same
// underlying core (output, level, hooks, async state) and add bound fields.
type Logger struct {
	*loggerCore
	boundFields []Field // immutable fields prepended to every entry
}

// loggerCore holds the mutable, shared logging machinery. It is referenced (not
// copied) by every logger derived via With, so a single SetOutput/SetLevel/etc.
// is observed by the whole family of derived loggers.
type loggerCore struct {
	level     atomic.Int32
	output    atomic.Pointer[writerHolder]
	formatter atomic.Pointer[formatterHolder]
	sampler   atomic.Pointer[samplerHolder]
	hooksMu   sync.RWMutex
	hooks     []hookRegistration
	// writeMu guards the output. Synchronized writes take it exclusively;
	// unsynchronized writes take it for reading, which still lets them run
	// concurrently with each other but lets a writer swap or a Close wait for
	// them to finish.
	writeMu           sync.RWMutex
	closer            io.Closer
	syncWrites        atomic.Bool
	extractor         atomic.Pointer[extractorHolder]
	includeStacktrace atomic.Bool

	redactor        atomic.Pointer[redactorHolder]
	errHandler      atomic.Pointer[errHandlerHolder]
	levelOutputs    atomic.Pointer[[]levelOutput]
	dedupeFields    atomic.Bool
	maxFieldBytes   atomic.Int64
	maxMessageBytes atomic.Int64
	writeErrors     atomic.Int64

	asyncMu     sync.Mutex                 // serializes async lifecycle (enable/disable)
	asyncSendMu sync.RWMutex               // producers RLock to send; closer Locks to retire
	asyncWG     sync.WaitGroup             // tracks the worker goroutine
	async       atomic.Pointer[asyncState] // non-nil while async logging is active
	asyncDrops  atomic.Int64
}

// asyncState is the immutable-per-generation async configuration. A new value is
// created by EnableAsync and retired by disableAsyncLocked; only DropStrategy is
// mutable at runtime (via SetDropStrategy) and is therefore stored atomically.
type asyncState struct {
	ch            chan logRequest
	strategy      atomic.Int32 // DropStrategy
	batchSize     int
	flushInterval time.Duration
}

// NewLogger creates a new Logger instance. A nil output is replaced with
// io.Discard. It panics if formatter is nil, since a logger cannot format
// without one.
func NewLogger(output io.Writer, level LogLevel, formatter Formatter) *Logger {
	if formatter == nil {
		panic("logger: formatter cannot be nil")
	}
	if output == nil {
		output = io.Discard
	}
	logger := &Logger{loggerCore: &loggerCore{}}
	logger.level.Store(int32(level))
	logger.output.Store(&writerHolder{w: output})
	logger.formatter.Store(&formatterHolder{f: formatter})
	logger.syncWrites.Store(true)
	logger.extractor.Store(&extractorHolder{fn: defaultContextExtractor})
	logger.sampler.Store((*samplerHolder)(nil))
	logger.includeStacktrace.Store(false)
	return logger
}

var defaultLogger atomic.Pointer[Logger]

// Default returns the process-wide default logger. On first use it lazily
// creates one that writes to os.Stdout at INFO with the default text formatter.
// Use it for zero-config logging, e.g. log.Default().Info("ready").
//
// (There are no package-level Info/Error/Fatal functions because the package
// already exposes Error as a Field constructor; go through Default() instead.)
func Default() *Logger {
	if l := defaultLogger.Load(); l != nil {
		return l
	}
	l := NewLogger(os.Stdout, INFO, &DefaultFormatter{})
	if defaultLogger.CompareAndSwap(nil, l) {
		return l
	}
	return defaultLogger.Load()
}

// SetDefault replaces the process-wide default logger returned by Default.
func SetDefault(l *Logger) {
	if l != nil {
		defaultLogger.Store(l)
	}
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
		// Holding writeMu exclusively excludes in-flight writers in both modes,
		// so nothing can still be holding the previous writer and it can be
		// closed here. The close error is not actionable.
		_ = l.closer.Close()
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

// Level returns the current logging level.
func (l *Logger) Level() LogLevel {
	return LogLevel(l.level.Load())
}

// Enabled reports whether an entry at the given level would be emitted. Use it
// to guard expensive field construction on hot paths:
//
//	if logger.Enabled(log.DEBUG) {
//		logger.DebugFields("state", expensiveFields()...)
//	}
func (l *Logger) Enabled(level LogLevel) bool {
	return level >= LogLevel(l.level.Load())
}

// With returns a derived logger that prepends the given fields to every entry it
// emits. The derived logger shares the parent's output, level, hooks, and async
// state, so runtime changes (SetLevel, SetOutput, AddHook, EnableAsync, ...)
// made through any logger in the family are observed by all of them. Bound
// fields appear ahead of per-call and context-extracted fields.
func (l *Logger) With(fields ...Field) *Logger {
	if len(fields) == 0 {
		return l
	}
	bound := make([]Field, 0, len(l.boundFields)+len(fields))
	bound = append(bound, l.boundFields...)
	bound = append(bound, fields...)
	return &Logger{loggerCore: l.loggerCore, boundFields: bound}
}

const loggerNameKey = "logger"

// Named returns a derived logger that tags every entry with a "logger" field
// naming the component. Nested Named calls join with dots, e.g.
// logger.Named("http").Named("auth") emits logger=http.auth.
func (l *Logger) Named(name string) *Logger {
	if name == "" {
		return l
	}
	prefix := ""
	rest := make([]Field, 0, len(l.boundFields))
	for _, f := range l.boundFields {
		if f.Key == loggerNameKey {
			if s, ok := f.Value.(string); ok {
				prefix = s
			}
			continue // drop the old name; re-add the dotted value below
		}
		rest = append(rest, f)
	}
	full := name
	if prefix != "" {
		full = prefix + "." + name
	}
	bound := make([]Field, 0, len(rest)+1)
	bound = append(bound, String(loggerNameKey, full))
	bound = append(bound, rest...)
	return &Logger{loggerCore: l.loggerCore, boundFields: bound}
}

// Recover logs a recovered panic at ERROR level (with the stack) and re-panics,
// so an otherwise-unhandled panic still crashes the process. Use it at goroutine
// or handler boundaries: defer logger.Recover().
func (l *Logger) Recover() {
	if r := recover(); r != nil {
		l.ErrorFields("recovered panic", Any("panic", r), String("stacktrace", string(debug.Stack())))
		panic(r)
	}
}

// RecoverAndContinue logs a recovered panic at ERROR level (with the stack) but
// does not re-panic, letting the goroutine continue. Use it for worker
// goroutines that should survive a single bad task.
func (l *Logger) RecoverAndContinue() {
	if r := recover(); r != nil {
		l.ErrorFields("recovered panic", Any("panic", r), String("stacktrace", string(debug.Stack())))
	}
}

// SetFormatter allows changing the log message format. It panics if formatter
// is nil.
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

// SetRedactor installs a Redactor that masks fields (and, if it implements
// MessageRedactor, the message) before formatting and hook dispatch. Pass nil
// to remove it. See NewKeyRedactor and NewPatternScrubber.
func (l *Logger) SetRedactor(r Redactor) {
	if r == nil {
		l.redactor.Store((*redactorHolder)(nil))
		return
	}
	l.redactor.Store(&redactorHolder{r: r})
}

// SetDeduplicateFields enables last-wins de-duplication of fields sharing a key
// (e.g. a context field overridden at the call site) before rendering. It adds
// a small per-entry cost, so it is off by default.
func (l *Logger) SetDeduplicateFields(enabled bool) {
	l.dedupeFields.Store(enabled)
}

// SetMaxFieldBytes caps the length of string field values; longer values are
// truncated with a "...[+N bytes]" marker. Zero (the default) means unlimited.
func (l *Logger) SetMaxFieldBytes(n int) {
	l.maxFieldBytes.Store(int64(n))
}

// SetMaxMessageBytes caps the length of the rendered message; longer messages
// are truncated with a "...[+N bytes]" marker. Zero (the default) means unlimited.
func (l *Logger) SetMaxMessageBytes(n int) {
	l.maxMessageBytes.Store(int64(n))
}

// SetErrorHandler registers a callback invoked when a write to the output sink
// returns an error. Without one, sink write failures are counted (WriteErrors)
// but otherwise silent. Pass nil to remove it.
func (l *Logger) SetErrorHandler(fn func(error)) {
	if fn == nil {
		l.errHandler.Store((*errHandlerHolder)(nil))
		return
	}
	l.errHandler.Store(&errHandlerHolder{fn: fn})
}

// WriteErrors returns the number of sink write errors observed so far.
func (l *Logger) WriteErrors() int64 {
	return l.writeErrors.Load()
}

type levelOutput struct {
	min LogLevel
	w   io.Writer
}

// AddLevelOutput registers an additional writer that receives the same formatted
// entries as the primary output, but only for entries at or above minLevel. For
// example, AddLevelOutput(ERROR, alertSink) mirrors errors and fatals to an
// alerting sink while the primary output keeps everything. Outputs added here are
// not closed by Close; manage their lifecycle yourself.
func (l *Logger) AddLevelOutput(minLevel LogLevel, w io.Writer) {
	if w == nil {
		return
	}
	for {
		cur := l.levelOutputs.Load()
		var next []levelOutput
		if cur != nil {
			next = append(next, *cur...)
		}
		next = append(next, levelOutput{min: minLevel, w: w})
		if l.levelOutputs.CompareAndSwap(cur, &next) {
			return
		}
	}
}

// ClearLevelOutputs removes all writers registered with AddLevelOutput.
func (l *Logger) ClearLevelOutputs() {
	l.levelOutputs.Store(nil)
}

// normalizeFields applies redaction, de-duplication, and value truncation. It
// returns fields unchanged (no allocation) when none are configured.
func (l *Logger) normalizeFields(fields []Field) []Field {
	if rh := l.redactor.Load(); rh != nil && rh.r != nil {
		fields = rh.r.Redact(fields)
	}
	if l.dedupeFields.Load() {
		fields = dedupeFieldsLastWins(fields)
	}
	if max := l.maxFieldBytes.Load(); max > 0 {
		fields = truncateFieldValues(fields, int(max))
	}
	return fields
}

// redactMessage scrubs and truncates the resolved message per configuration.
func (l *Logger) redactMessage(message string) string {
	if rh := l.redactor.Load(); rh != nil && rh.r != nil {
		if mr, ok := rh.r.(MessageRedactor); ok {
			message = mr.RedactMessage(message)
		}
	}
	if max := l.maxMessageBytes.Load(); max > 0 {
		message = truncateString(message, int(max))
	}
	return message
}

func (l *Logger) reportWriteError(err error) {
	if err == nil {
		return
	}
	l.writeErrors.Add(1)
	if h := l.errHandler.Load(); h != nil && h.fn != nil {
		h.fn(err)
	}
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

	st := &asyncState{
		ch:            make(chan logRequest, opts.QueueSize),
		batchSize:     opts.BatchSize,
		flushInterval: opts.FlushInterval,
	}
	st.strategy.Store(int32(opts.DropStrategy))
	l.async.Store(st)
	l.asyncWG.Add(1)
	go l.asyncWorker(st)
}

// SetDropStrategy changes the behavior when the async queue is full. It takes
// effect immediately on the active async logger; it is a no-op when async
// logging is disabled.
func (l *Logger) SetDropStrategy(strategy DropStrategy) {
	l.asyncMu.Lock()
	defer l.asyncMu.Unlock()
	if st := l.async.Load(); st != nil {
		st.strategy.Store(int32(strategy))
	}
}

// DisableAsync stops asynchronous logging and flushes pending entries.
func (l *Logger) DisableAsync() {
	l.asyncMu.Lock()
	l.disableAsyncLocked()
	l.asyncMu.Unlock()
}

// Flush blocks until every entry enqueued before the call has been written by
// the async worker, without stopping the worker. It is a no-op in synchronous
// mode (where writes are not buffered).
func (l *Logger) Flush() {
	st := l.async.Load()
	if st == nil {
		return
	}
	done := make(chan struct{})
	l.asyncSendMu.RLock()
	if l.async.Load() != st {
		// Async was retired; anything queued has already been drained.
		l.asyncSendMu.RUnlock()
		return
	}
	// Block-send the barrier so it is never dropped. The worker cannot be retired
	// while we hold the read lock, so the send cannot hit a closed channel.
	st.ch <- logRequest{flush: done}
	l.asyncSendMu.RUnlock()
	<-done
}

// Sync flushes the async queue (see Flush) and, if the current output supports
// it (e.g. *os.File), calls its Sync method to fsync buffered data to disk.
func (l *Logger) Sync() error {
	l.Flush()
	wh := l.output.Load()
	if wh == nil || wh.w == nil {
		return nil
	}
	if s, ok := wh.w.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

// disableAsyncLocked retires the active async generation. It must be called with
// asyncMu held. The asyncSendMu write lock excludes all in-flight producers, and
// clearing l.async makes subsequent producers fall back to the sync path, so the
// channel can be closed with no risk of a "send on closed channel" panic.
func (l *Logger) disableAsyncLocked() {
	st := l.async.Load()
	if st == nil {
		return
	}
	l.asyncSendMu.Lock()
	l.async.Store(nil)
	l.asyncSendMu.Unlock()
	close(st.ch)
	l.asyncWG.Wait()
}

func (l *Logger) asyncWorker(st *asyncState) {
	defer l.asyncWG.Done()

	ch := st.ch
	batchSize := st.batchSize
	if batchSize <= 1 && st.flushInterval <= 0 {
		for req := range ch {
			if req.flush != nil {
				close(req.flush)
				continue
			}
			l.logEntrySync(req.level, req.message, req.hasMessage, req.fields, req.args, req.caller)
		}
		return
	}

	if batchSize <= 0 {
		batchSize = 1
	}

	flushInterval := st.flushInterval
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
			l.logEntrySync(req.level, req.message, req.hasMessage, req.fields, req.args, req.caller)
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
			if req.flush != nil {
				// Drain everything queued before this barrier, then signal.
				if len(batch) > 0 {
					flush()
				}
				close(req.flush)
				continue
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

// fireHook invokes a hook with panic isolation. A hook is arbitrary
// integration code (metrics, alerting, exporters); a panic there must never
// crash the caller's goroutine or kill the async worker.
func (l *Logger) fireHook(h Hook, level LogLevel, message string, fields []Field) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "logger: hook panicked: %v\n", r)
		}
	}()
	h.Fire(level, message, fields)
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
	// Now overrides the timestamp source; defaults to time.Now. Set it to a
	// fixed clock in tests for deterministic, golden-file-friendly output.
	Now func() time.Time
}

func (f *DefaultFormatter) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
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

// IncludeCallerInfo implements CallerAwareFormatter.
func (f *DefaultFormatter) IncludeCallerInfo() bool { return f.IncludeCaller }

// FormatWithCallerTo implements CallerAwareFormatter.
func (f *DefaultFormatter) FormatWithCallerTo(level LogLevel, message string, fields []Field, caller Caller, w io.Writer) {
	f.writeFrame(level, w, &caller, func(writer io.Writer) {
		writeTextSafe(writer, message)
		writeFieldsText(writer, fields, message != "")
	})
}

// writeFrame renders the timestamp/caller/level prefix. A non-nil caller was
// resolved by the logger on the goroutine that logged; otherwise the call site
// is resolved here, from this goroutine's stack.
func (f *DefaultFormatter) writeFrame(level LogLevel, w io.Writer, caller *Caller, messageWriter func(io.Writer)) {
	var tmp [128]byte
	b := tmp[:0]
	now := f.now()
	if f.TimeLayout != "" {
		b = append(b, now.Format(f.TimeLayout)...)
	} else {
		b = appendTimestampSlice(b, now)
	}
	if f.IncludeCaller {
		file, line := "", 0
		if caller != nil {
			file, line = caller.File, caller.Line
		} else {
			file, line = resolveCaller(0)
		}
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
	f.writeFrame(level, w, nil, func(writer io.Writer) {
		writeTextSafe(writer, message)
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
	f.writeFrame(level, w, nil, func(writer io.Writer) {
		writeTextSafe(writer, message)
		writeFieldsText(writer, fields, message != "")
	})
}

// FormatArgs implements ArgsFormatter for DefaultFormatter to avoid intermediate string allocations
func (f *DefaultFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	f.writeFrame(level, w, nil, func(writer io.Writer) {
		for i, val := range v {
			if i > 0 {
				writer.Write([]byte{' '})
			}
			writeFieldValueText(writer, val)
		}
	})
}

func (f *DefaultFormatter) FormatArgsWithFields(level LogLevel, fields []Field, w io.Writer, v ...interface{}) {
	f.writeFrame(level, w, nil, func(writer io.Writer) {
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
	// Now overrides the timestamp source; defaults to time.Now. Set it to a
	// fixed clock in tests for deterministic, golden-file-friendly output.
	Now func() time.Time
}

func (f *JSONFormatter) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// IncludeCallerInfo implements CallerAwareFormatter.
func (f *JSONFormatter) IncludeCallerInfo() bool { return f.IncludeCaller }

// FormatWithCallerTo implements CallerAwareFormatter.
func (f *JSONFormatter) FormatWithCallerTo(level LogLevel, message string, fields []Field, caller Caller, w io.Writer) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, fields, &caller, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// formatBuffer renders one entry. A non-nil caller was resolved by the logger on
// the goroutine that logged; otherwise the call site is resolved here.
func (f *JSONFormatter) formatBuffer(level LogLevel, message string, fields []Field, caller *Caller, buf *bytes.Buffer) {
	now := f.now()
	buf.WriteString(`{"timestamp":"`)
	if f.TimeLayout != "" {
		buf.WriteString(now.Format(f.TimeLayout))
	} else {
		buf.WriteString(now.Format(time.RFC3339))
	}
	buf.WriteString(`","level":"`)
	buf.WriteString(logLevelToString(level))
	buf.WriteString(`","message":`)
	appendJSONString(buf, message)
	if f.IncludeCaller {
		file, line := "", 0
		if caller != nil {
			file, line = caller.File, caller.Line
		} else {
			file, line = resolveCaller(0)
		}
		buf.WriteString(`,"file":`)
		appendJSONString(buf, file)
		buf.WriteString(`,"line":`)
		buf.WriteString(strconv.Itoa(line))
	}
	appendJSONFields(buf, fields)
	buf.WriteString("}\n")
}

func (f *JSONFormatter) Format(level LogLevel, message string) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, nil, nil, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *JSONFormatter) FormatTo(level LogLevel, message string, w io.Writer) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, nil, nil, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

// FormatArgs implements ArgsFormatter for JSONFormatter. It joins the arguments
// into a message before delegating to FormatTo.
func (f *JSONFormatter) FormatArgs(level LogLevel, w io.Writer, v ...interface{}) {
	msg := buildMessage(v...)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, msg, nil, nil, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

func (f *JSONFormatter) FormatWithFields(level LogLevel, message string, fields []Field) string {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, fields, nil, buf)
	s := buf.String()
	bufferPool.Put(buf)
	return s
}

func (f *JSONFormatter) FormatWithFieldsTo(level LogLevel, message string, fields []Field, w io.Writer) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, message, fields, nil, buf)
	buf.WriteTo(w)
	bufferPool.Put(buf)
}

func (f *JSONFormatter) FormatArgsWithFields(level LogLevel, fields []Field, w io.Writer, v ...interface{}) {
	msg := buildMessage(v...)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	f.formatBuffer(level, msg, fields, nil, buf)
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

	// Prepend fields bound via With so they appear on every entry, ahead of
	// per-call and context-extracted fields, and are visible to samplers/hooks.
	if len(l.boundFields) > 0 {
		merged := make([]Field, 0, len(l.boundFields)+len(fields))
		merged = append(merged, l.boundFields...)
		merged = append(merged, fields...)
		fields = merged
	}

	// FATAL must terminate the process. It is never sampled away and never
	// deferred to the async worker (where it could be dropped or race the
	// caller); it always runs synchronously on the calling goroutine so os.Exit
	// fires before logEntry returns.
	if level != FATAL {
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
			// Capture anything that describes the caller *here*, on the
			// goroutine that logged. Resolved on the worker it would describe
			// the worker instead.
			fields = l.withStacktrace(level, fields)
			req := logRequest{
				level:      level,
				message:    message,
				hasMessage: hasMessage,
				fields:     cloneFields(fields),
				caller:     l.resolveCallerForFormatter(),
			}
			if len(args) > 0 {
				req.args = cloneArgs(args)
			}
			if l.enqueueAsync(req) {
				return
			}
			l.logEntrySync(level, message, hasMessage, fields, args, req.caller)
			return
		}
	} else if l.async.Load() != nil {
		// Everything already queued explains why we are dying. Write it out
		// before os.Exit takes the queue with it.
		l.Flush()
	}

	fields = l.withStacktrace(level, fields)
	l.logEntrySync(level, message, hasMessage, fields, args, nil)
}

// withStacktrace appends the caller's stack for error-level entries when
// automatic capture is enabled. It copies rather than appending in place, so a
// caller's fields array is never written through.
func (l *Logger) withStacktrace(level LogLevel, fields []Field) []Field {
	if !l.includeStacktrace.Load() || level < ERROR {
		return fields
	}
	for _, f := range fields {
		if f.Key == "stacktrace" {
			return fields
		}
	}
	out := make([]Field, 0, len(fields)+1)
	out = append(out, fields...)
	return append(out, String("stacktrace", string(debug.Stack())))
}

// resolveCallerForFormatter resolves the call site when the active formatter
// renders one, and returns nil otherwise so the cost is only paid when the
// result is used.
func (l *Logger) resolveCallerForFormatter() *Caller {
	fh := l.formatter.Load()
	if fh == nil || fh.f == nil {
		return nil
	}
	caf, ok := fh.f.(CallerAwareFormatter)
	if !ok || !caf.IncludeCallerInfo() {
		return nil
	}
	file, line := resolveCaller(0)
	return &Caller{File: file, Line: line}
}

func (l *Logger) logEntrySync(level LogLevel, message string, hasMessage bool, fields []Field, args []interface{}, caller *Caller) {
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

	// Tee to any additional level-scoped outputs whose threshold this entry meets
	// (e.g. mirror ERROR/FATAL to an alerting sink while everything goes to a file).
	if lop := l.levelOutputs.Load(); lop != nil && len(*lop) > 0 {
		writers := make([]io.Writer, 1, len(*lop)+1)
		writers[0] = writer
		for _, lo := range *lop {
			if level >= lo.min {
				writers = append(writers, lo.w)
			}
		}
		if len(writers) > 1 {
			writer = io.MultiWriter(writers...)
		}
	}

	// Mask/dedup/truncate fields once, before both hooks and the formatter see them.
	fields = l.normalizeFields(fields)

	hooks := l.hooksSnapshot()

	write := func(fn func(io.Writer)) {
		ec := &errCapturingWriter{w: writer}
		if l.syncWrites.Load() {
			l.writeMu.Lock()
			fn(ec)
			l.writeMu.Unlock()
		} else {
			// Shared rather than unguarded: concurrent writes still proceed in
			// parallel, but a writer swap or Close can wait them out instead of
			// closing a writer somebody is still using.
			l.writeMu.RLock()
			fn(ec)
			l.writeMu.RUnlock()
		}
		l.reportWriteError(ec.err)
		if level == FATAL {
			os.Exit(1)
		}
	}

	// The args fast path cannot carry a pre-resolved call site, so it is skipped
	// when one was captured (async mode with a caller-rendering formatter).
	if caller == nil && len(fields) == 0 && len(args) > 0 {
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
	resolvedMessage = l.redactMessage(resolvedMessage)

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
			l.fireHook(reg.target, level, resolvedMessage, fields)
		}
	}

	if caller != nil {
		if caf, ok := formatter.(CallerAwareFormatter); ok {
			write(func(w io.Writer) {
				caf.FormatWithCallerTo(level, resolvedMessage, fields, *caller, w)
			})
			return
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

// Close releases any resources owned by the logger, such as open files. Writers
// superseded by a later SetOutputWithCloser are closed at the point they are
// replaced, so only the current one is left to close here. The close error, if
// any, is returned.
func (l *Logger) Close() error {
	l.DisableAsync()
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	var firstErr error
	if l.closer != nil {
		if err := l.closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		l.closer = nil
	}
	return firstErr
}

// resolveCaller reports the first frame outside this package, i.e. the code that
// called the logger.
//
// Frames are expanded with runtime.CallersFrames rather than read straight off
// the PC with FuncForPC: a PC can stand for several logical frames once the
// compiler inlines, and FuncForPC reports only the outermost one, which puts the
// file and line somewhere unrelated to the call site. Each PC's own result is
// cacheable (its inline chain is fixed), so the walk stays cheap.
func resolveCaller(extraSkip int) (string, int) {
	const depth = 16
	var pcs [depth]uintptr
	n := runtime.Callers(2+extraSkip, pcs[:])
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
			return ce.file, ce.line
		}

		entry := callerEntry{skip: true}
		frames := runtime.CallersFrames([]uintptr{pc})
		for {
			frame, more := frames.Next()
			if frame.Function != "" && !strings.HasPrefix(frame.Function, packagePrefix) {
				entry = callerEntry{file: basename(frame.File), line: frame.Line}
				break
			}
			if !more {
				break
			}
		}
		callerCache.Store(pc, entry)
		if !entry.skip {
			return entry.file, entry.line
		}
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
		writeTextSafe(b, message)
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

// containsControl reports whether s holds a character that would break a
// line-oriented log record.
func containsControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// writeTextSafe writes s, quoting it if it contains control characters. Without
// this a newline inside a message or a field value ends the record, and whatever
// follows reads as a genuine entry of its own -- log forging, using data that
// often comes straight from a request.
func writeTextSafe(w io.Writer, s string) {
	if !containsControl(s) {
		io.WriteString(w, s)
		return
	}
	io.WriteString(w, strconv.Quote(s))
}

func writeFieldValueText(w io.Writer, val interface{}) {
	if s, ok := encodeTextWithRegistry(val); ok {
		writeTextSafe(w, s)
		return
	}
	switch v := val.(type) {
	case string:
		writeTextSafe(w, v)
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
		writeTextSafe(w, v.String())
	case error:
		writeTextSafe(w, v.Error())
	default:
		writeTextSafe(w, fmt.Sprint(v))
	}
}

const hexDigits = "0123456789abcdef"

// appendJSONString writes s as a JSON string, quotes included.
//
// strconv.Quote is not usable here: it produces Go literal syntax, so invalid
// UTF-8 comes out as \xNN and non-printable runes outside the BMP as \U0001d173,
// neither of which is a JSON escape. A single such byte -- which truncation can
// manufacture by cutting mid-rune -- makes the whole entry unparseable.
func appendJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= ' ' && b != '"' && b != '\\' {
				i++
				continue
			}
			buf.WriteString(s[start:i])
			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				buf.WriteString(`\u00`)
				buf.WriteByte(hexDigits[b>>4])
				buf.WriteByte(hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		if r, size := utf8.DecodeRuneInString(s[i:]); r == utf8.RuneError && size == 1 {
			buf.WriteString(s[start:i])
			buf.WriteString(`�`)
			i++
			start = i
		} else {
			i += size
		}
	}
	buf.WriteString(s[start:])
	buf.WriteByte('"')
}

func appendJSONFields(buf *bytes.Buffer, fields []Field) {
	for _, field := range fields {
		buf.WriteByte(',')
		appendJSONString(buf, field.Key)
		buf.WriteByte(':')
		appendJSONValue(buf, field.Value)
	}
}

func appendJSONValue(buf *bytes.Buffer, val interface{}) {
	if encoded, ok := encodeJSONWithRegistry(val); ok {
		switch v := encoded.(type) {
		case string:
			appendJSONString(buf, v)
		case []byte:
			appendJSONString(buf, string(v))
		default:
			if data, err := json.Marshal(v); err == nil {
				buf.Write(data)
			} else {
				appendJSONString(buf, fmt.Sprint(v))
			}
		}
		return
	}
	switch v := val.(type) {
	case string:
		appendJSONString(buf, v)
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
		appendJSONString(buf, v.String())
		return
	case error:
		appendJSONString(buf, v.Error())
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
	appendJSONString(buf, fmt.Sprint(val))
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

// logf formats and logs a message, but only once the level has been checked --
// the point of the *f methods over Info(fmt.Sprintf(...)), which formats whether
// or not the entry survives.
func (l *Logger) logf(level LogLevel, format string, args ...interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}
	l.logEntry(level, fmt.Sprintf(format, args...), true, nil, nil)
}

// Debugf logs a formatted debug message. The message is only formatted if DEBUG
// is enabled.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// Infof logs a formatted info message. The message is only formatted if INFO is
// enabled.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// Warnf logs a formatted warning. The message is only formatted if WARN is
// enabled.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

// Errorf logs a formatted error. The message is only formatted if ERROR is
// enabled.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// Fatalf logs a formatted fatal message and exits the application.
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logf(FATAL, format, args...)
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
	if st := l.async.Load(); st != nil {
		return st.ch
	}
	return nil
}

func (l *Logger) enqueueAsync(req logRequest) bool {
	st := l.async.Load()
	if st == nil {
		return false
	}
	// Hold the read lock across the send so disableAsyncLocked (which takes the
	// write lock before closing the channel) cannot retire this generation while
	// we are sending. Re-check l.async after acquiring the lock: if it changed,
	// this generation was retired and we must not touch st.ch.
	l.asyncSendMu.RLock()
	defer l.asyncSendMu.RUnlock()
	if l.async.Load() != st {
		return false
	}

	ch := st.ch
	switch DropStrategy(st.strategy.Load()) {
	case DropOldest:
		select {
		case ch <- req:
			return true
		default:
			// Free a slot by discarding the oldest entry -- but never a Flush
			// barrier. A dropped barrier is never closed by the worker, which
			// would leave Flush (and Sync) blocked for the life of the process.
			// Barriers encountered while looking for something to drop are set
			// aside and re-queued behind it.
			var barriers []logRequest
			for done := false; !done; {
				select {
				case old := <-ch:
					if old.flush != nil {
						barriers = append(barriers, old)
						continue
					}
					l.asyncDrops.Add(1)
					done = true
				default:
					// Nothing droppable left; the queue drained under us.
					done = true
				}
			}
			for _, b := range barriers {
				select {
				case ch <- b:
				default:
					// Unreachable in practice (we only re-queue what we took).
					// Release the waiter rather than strand it.
					close(b.flush)
				}
			}
			select {
			case ch <- req:
			default:
				// A concurrent producer refilled the freed slot; the new entry
				// is dropped, so count it to keep AsyncStats().Dropped accurate.
				l.asyncDrops.Add(1)
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
