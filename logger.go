package log

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var (
	encodeBufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 512)
			return &b
		},
	}
)

// dropStrategy defines behavior when the async queue is full. It is internal:
// callers choose it through WithAsyncDropOldest / WithAsyncBlocking.
type dropStrategy int

const (
	dropNew dropStrategy = iota
	dropOldest
	blockWhenFull
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

func Any(key string, value interface{}) Field { return Field{Key: key, Value: value} }

// Group nests fields under a key: a JSON object, and a dotted prefix in text
// output. It is what the slog bridge maps slog.Group onto.
func Group(key string, fields ...Field) Field {
	return Field{Key: key, Value: append([]Field(nil), fields...)}
}

// Lazy defers computing a value until the entry is known to be emitted, so an
// expensive field costs nothing on an entry that a level or sampler drops.
func Lazy(key string, fn func() interface{}) Field {
	return Field{Key: key, Value: lazyValue{fn: fn}}
}

// lazyValue is resolved in normalizeFields, before redaction and hooks, so
// everything downstream sees a plain value.
type lazyValue struct{ fn func() interface{} }

// Err records an error. It is named Err rather than Error so that the package
// can offer Error as a logging function, which every comparable library does;
// zerolog uses the same name for the same reason.
func Err(key string, err error) Field {
	if err == nil {
		return Field{Key: key, Value: nil}
	}
	return Field{Key: key, Value: err.Error()}
}

func Int32(key string, value int32) Field     { return Field{Key: key, Value: int64(value)} }
func Uint32(key string, value uint32) Field   { return Field{Key: key, Value: uint64(value)} }
func Uint64(key string, value uint64) Field   { return Field{Key: key, Value: value} }
func Float32(key string, value float32) Field { return Field{Key: key, Value: value} }

// Duration records a duration, rendered as "1.5s" rather than a nanosecond
// count.
func Duration(key string, value time.Duration) Field { return Field{Key: key, Value: value} }

// Time records a timestamp, rendered in RFC 3339.
func Time(key string, value time.Time) Field { return Field{Key: key, Value: value} }

// Stringer records any value with a String method, calling it at encode time so
// a filtered-out entry never pays for it.
func Stringer(key string, value fmt.Stringer) Field { return Field{Key: key, Value: value} }

// Binary records bytes as a hex string. Without it a []byte renders through
// fmt as a list of decimal numbers, which is unreadable and enormous.
func Binary(key string, value []byte) Field {
	return Field{Key: key, Value: hex.EncodeToString(value)}
}

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

// EntryOption adjusts a single entry. It exists for bridges that already know
// something the logger would otherwise derive -- slog hands over a record
// carrying its own timestamp and program counter, and discarding those was a
// conformance failure.
type EntryOption func(*entryOverrides)

type entryOverrides struct {
	time   time.Time
	caller *Caller
}

// EntryTime records the entry at t rather than at the moment it is encoded.
func EntryTime(t time.Time) EntryOption {
	return func(o *entryOverrides) { o.time = t }
}

// EntryCaller records an already-resolved call site.
func EntryCaller(file string, line int) EntryOption {
	return func(o *entryOverrides) { o.caller = &Caller{File: file, Line: line} }
}

type logRequest struct {
	level   LogLevel
	entryAt time.Time
	message string
	fields  []Field
	caller  *Caller       // call site resolved by the producer, nil to resolve at format time
	flush   chan struct{} // non-nil marks a Flush barrier rather than an entry
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
	// TRACE is the most verbose level, below DEBUG.
	TRACE LogLevel = iota
	DEBUG
	INFO
	WARN
	ERROR
	// PANIC logs the entry and then panics, the way FATAL logs and then exits.
	PANIC
	FATAL
)

// String returns the upper-case name of the level (e.g. "INFO"), or "UNKNOWN".
func (l LogLevel) String() string {
	return logLevelToString(l)
}

// ParseLevel converts a level name (case-insensitive: trace, debug, info, warn,
// error, panic, fatal) to a LogLevel. It returns an error for an unrecognized
// name.
func ParseLevel(s string) (LogLevel, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE":
		return TRACE, nil
	case "DEBUG":
		return DEBUG, nil
	case "INFO":
		return INFO, nil
	case "WARN", "WARNING":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	case "PANIC":
		return PANIC, nil
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

// UnmarshalJSON accepts a level name ("debug", "WARN", "warning"), matching
// every other entry point: LOG_LEVEL, the HTTP level endpoint, ParseLevel.
//
// Numbers are deliberately rejected. A numeric level in a config file means
// whatever position that level happens to occupy, so adding TRACE below DEBUG
// silently turned every existing 3 from ERROR into WARN. A name cannot drift
// that way.
func (l *LogLevel) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("log level must be a name such as \"debug\" or \"error\": %w", err)
	}
	lvl, err := ParseLevel(name)
	if err != nil {
		return err
	}
	*l = lvl
	return nil
}

// Logger represents a logging instance
type writerHolder struct {
	w io.Writer
}

type encoderHolder struct {
	e Encoder
}

type clockHolder struct {
	fn func() time.Time
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
	level      atomic.Int32
	output     atomic.Pointer[writerHolder]
	encoder    atomic.Pointer[encoderHolder]
	clock      atomic.Pointer[clockHolder]
	wantCaller atomic.Bool
	sampler    atomic.Pointer[samplerHolder]
	hooksMu    sync.RWMutex
	hooks      []hookRegistration
	// writeMu guards the output. Synchronized writes take it exclusively;
	// unsynchronized writes take it for reading, which still lets them run
	// concurrently with each other but lets a writer swap or a Close wait for
	// them to finish.
	writeMu           sync.RWMutex
	closer            io.Closer
	syncWrites        atomic.Bool
	extractor         atomic.Pointer[extractorHolder]
	includeStacktrace atomic.Bool
	callerSkip        atomic.Int32

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
// created by enableAsync and retired by disableAsyncLocked; only dropStrategy is
// mutable at runtime (via SetDropStrategy) and is therefore stored atomically.
type asyncState struct {
	ch            chan logRequest
	strategy      atomic.Int32 // dropStrategy
	batchSize     int
	flushInterval time.Duration
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
	l, _ := New()
	if defaultLogger.CompareAndSwap(nil, l) {
		return l
	}
	return defaultLogger.Load()
}

// Package-level logging goes through Default(). These exist because
// log.Info("ready") is the first line of nearly every getting-started guide in
// the ecosystem, and needing log.Default().Info is friction with no upside.

// Log emits an entry at the given level. The level methods are better for
// ordinary code; this is for bridges that receive the level as data.
func (l *Logger) Log(level LogLevel, message string, fields []Field, opts ...EntryOption) {
	var ov *entryOverrides
	if len(opts) > 0 {
		ov = &entryOverrides{}
		for _, opt := range opts {
			opt(ov)
		}
	}
	l.logEntry(level, message, fields, ov)
}

// Trace logs a message at TRACE on the default logger.
func Trace(message string, fields ...Field) { Default().Trace(message, fields...) }

// Tracef logs a formatted message at TRACE on the default logger.
func Tracef(format string, args ...interface{}) { Default().Tracef(format, args...) }

// Debug logs a message at DEBUG on the default logger.
func Debug(message string, fields ...Field) { Default().Debug(message, fields...) }

// Debugf logs a formatted message at DEBUG on the default logger.
func Debugf(format string, args ...interface{}) { Default().Debugf(format, args...) }

// Info logs a message at INFO on the default logger.
func Info(message string, fields ...Field) { Default().Info(message, fields...) }

// Infof logs a formatted message at INFO on the default logger.
func Infof(format string, args ...interface{}) { Default().Infof(format, args...) }

// Warn logs a message at WARN on the default logger.
func Warn(message string, fields ...Field) { Default().Warn(message, fields...) }

// Warnf logs a formatted message at WARN on the default logger.
func Warnf(format string, args ...interface{}) { Default().Warnf(format, args...) }

// Error logs a message at ERROR on the default logger.
func Error(message string, fields ...Field) { Default().Error(message, fields...) }

// Errorf logs a formatted message at ERROR on the default logger.
func Errorf(format string, args ...interface{}) { Default().Errorf(format, args...) }

// Panic logs a message at PANIC on the default logger.
func Panic(message string, fields ...Field) { Default().Panic(message, fields...) }

// Panicf logs a formatted message at PANIC on the default logger.
func Panicf(format string, args ...interface{}) { Default().Panicf(format, args...) }

// Fatal logs a message at FATAL on the default logger.
func Fatal(message string, fields ...Field) { Default().Fatal(message, fields...) }

// Fatalf logs a formatted message at FATAL on the default logger.
func Fatalf(format string, args ...interface{}) { Default().Fatalf(format, args...) }

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

// SetOutputWithCloser changes the output and hands the logger ownership of the
// closer, which it invokes on Close or when a later SetOutput* call replaces the
// writer. Use SetOutput when the caller keeps ownership.
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
//		logger.Debug("state", expensiveFields()...)
//	}
func (l *Logger) Enabled(level LogLevel) bool {
	return level >= LogLevel(l.level.Load())
}

// With returns a derived logger that prepends the given fields to every entry it
// emits. The derived logger shares the parent's output, level, hooks, and async
// state, so runtime changes (SetLevel, SetOutput, addHook, enableAsync, ...)
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

// WithError returns a derived logger that carries err on every entry, which is
// the shape logrus users reach for most.
func (l *Logger) WithError(err error) *Logger {
	return l.With(Err("error", err))
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
		l.Error("recovered panic", Any("panic", r), String("stacktrace", string(debug.Stack())))
		panic(r)
	}
}

// RecoverAndContinue logs a recovered panic at ERROR level (with the stack) but
// does not re-panic, letting the goroutine continue. Use it for worker
// goroutines that should survive a single bad task.
func (l *Logger) RecoverAndContinue() {
	if r := recover(); r != nil {
		l.Error("recovered panic", Any("panic", r), String("stacktrace", string(debug.Stack())))
	}
}

// setEncoder replaces the encoder. A nil encoder is ignored rather than
// panicking: this is reached from config reload, where a bad configuration
// should not take the process down.
func (l *Logger) setEncoder(enc Encoder) {
	if enc != nil {
		l.encoder.Store(&encoderHolder{e: enc})
	}
}

// setIncludeStacktrace toggles automatic stacktrace capture for error/fatal logs.
func (l *Logger) setIncludeStacktrace(enabled bool) {
	l.includeStacktrace.Store(enabled)
}

// WriteErrors returns the number of sink write errors observed so far.
func (l *Logger) WriteErrors() int64 {
	return l.writeErrors.Load()
}

type levelOutput struct {
	min LogLevel
	w   io.Writer
}

// normalizeFields applies redaction, de-duplication, and value truncation. It
// returns fields unchanged (no allocation) when none are configured.
func (l *Logger) normalizeFields(fields []Field) []Field {
	fields = resolveLazy(fields)
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

// resolveLazy replaces deferred values with their result, recursing into
// groups. It copies only when there is something to resolve.
func resolveLazy(fields []Field) []Field {
	if !hasLazy(fields) {
		return fields
	}
	out := make([]Field, len(fields))
	copy(out, fields)
	for i := range out {
		switch v := out[i].Value.(type) {
		case lazyValue:
			if v.fn != nil {
				out[i].Value = v.fn()
			} else {
				out[i].Value = nil
			}
		case []Field:
			out[i].Value = resolveLazy(v)
		}
	}
	return out
}

func hasLazy(fields []Field) bool {
	for _, f := range fields {
		switch v := f.Value.(type) {
		case lazyValue:
			return true
		case []Field:
			if hasLazy(v) {
				return true
			}
		}
	}
	return false
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

// enableAsync activates asynchronous logging with the provided options.
func (l *Logger) enableAsync(opts asyncConfig) {
	if opts.queueSize <= 0 {
		opts.queueSize = 1024
	}
	if opts.strategy != dropNew && opts.strategy != dropOldest && opts.strategy != blockWhenFull {
		opts.strategy = dropNew
	}
	if opts.batchSize <= 0 {
		opts.batchSize = 1
	}
	if opts.flushInterval < 0 {
		opts.flushInterval = 0
	}

	l.asyncMu.Lock()
	defer l.asyncMu.Unlock()
	l.disableAsyncLocked()

	st := &asyncState{
		ch:            make(chan logRequest, opts.queueSize),
		batchSize:     opts.batchSize,
		flushInterval: opts.flushInterval,
	}
	st.strategy.Store(int32(opts.strategy))
	l.async.Store(st)
	l.asyncWG.Add(1)
	go l.asyncWorker(st)
}

// disableAsync stops asynchronous logging and flushes pending entries.
func (l *Logger) disableAsync() {
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
			l.logEntrySync(req.level, req.message, req.fields, req.caller, req.entryAt)
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
			l.logEntrySync(req.level, req.message, req.fields, req.caller, req.entryAt)
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

// AddHook registers a hook fired for every emitted entry. Hooks are usually
// supplied with WithHook at construction; this exists because attaching one
// later is a real pattern -- an exporter that is only wired up once its own
// configuration has loaded, for instance.
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

// setSynchronized toggles serialized writes. When disabled, callers must ensure the
// writer they provide is safe for concurrent use.
func (l *Logger) setSynchronized(enabled bool) {
	l.syncWrites.Store(enabled)
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

const hexDigits = "0123456789abcdef"

var levelStrings = [...]string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "PANIC", "FATAL"}
var levelBytes = [...][]byte{
	[]byte("TRACE"),
	[]byte("DEBUG"),
	[]byte("INFO"),
	[]byte("WARN"),
	[]byte("ERROR"),
	[]byte("PANIC"),
	[]byte("FATAL"),
}

var levelColors = map[LogLevel]string{
	TRACE: "\033[37m", // Grey
	DEBUG: "\033[36m", // Cyan
	INFO:  "\033[32m", // Green
	WARN:  "\033[33m", // Yellow
	ERROR: "\033[31m", // Red
	PANIC: "\033[35m", // Magenta
	FATAL: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

const dimColor = "\033[2m"

const packagePrefix = "github.com/pod32g/simple-logger."

type callerEntry struct {
	file string
	line int
	skip bool
}

var callerCache sync.Map

// terminal reports whether the level ends the program's normal flow, which
// means the entry must be written before control leaves the logger.
func (l LogLevel) terminal() bool { return l == FATAL || l == PANIC }

// logLevelToString converts a LogLevel to its string representation
func logLevelToString(level LogLevel) string {
	if level >= 0 && int(level) < len(levelStrings) {
		return levelStrings[level]
	}
	return "UNKNOWN"
}

func basename(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func (l *Logger) logEntry(level LogLevel, message string, fields []Field, ov *entryOverrides) {
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

	// FATAL and PANIC end the program or unwind it, so they are never sampled
	// away and never deferred to the async worker (where they could be dropped
	// or race the caller). They run synchronously on the calling goroutine, so
	// the exit or panic happens before logEntry returns.
	if !level.terminal() {
		if holder := l.sampler.Load(); holder != nil && holder.fn != nil {
			if !holder.fn.Allow(level, message, fields) {
				return
			}
		}

		if ch := l.asyncChannel(); ch != nil {
			// Capture anything that describes the caller *here*, on the
			// goroutine that logged. Resolved on the worker it would describe
			// the worker instead.
			fields = l.withStacktrace(level, fields)
			req := logRequest{
				level:   level,
				message: message,
				fields:  cloneFields(fields),
				caller:  l.resolveCallerForEntry(),
			}
			if ov != nil {
				req.entryAt = ov.time
				if ov.caller != nil {
					req.caller = ov.caller
				}
			}
			if l.enqueueAsync(req) {
				return
			}
			l.logEntrySync(level, message, fields, req.caller, req.entryAt)
			return
		}
	} else if l.async.Load() != nil {
		// Everything already queued explains why we are stopping. Write it out
		// before the exit or panic takes the queue with it.
		l.Flush()
	}

	fields = l.withStacktrace(level, fields)
	var at time.Time
	var caller *Caller
	if ov != nil {
		at, caller = ov.time, ov.caller
	}
	if caller == nil {
		caller = l.resolveCallerForEntry()
	}
	l.logEntrySync(level, message, fields, caller, at)
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

// resolveCallerForEntry resolves the call site when the logger records one, and
// returns nil otherwise so the cost is only paid when the result is used. It
// must run on the goroutine that logged: resolved on the async worker it would
// describe the worker.
func (l *Logger) resolveCallerForEntry() *Caller {
	if !l.wantCaller.Load() {
		return nil
	}
	file, line := resolveCaller(int(l.callerSkip.Load()))
	return &Caller{File: file, Line: line}
}

func (l *Logger) logEntrySync(level LogLevel, message string, fields []Field, caller *Caller, at time.Time) {
	eh := l.encoder.Load()
	if eh == nil || eh.e == nil {
		return
	}

	wh := l.output.Load()
	var writer io.Writer
	if wh != nil {
		writer = wh.w
	}
	if writer == nil {
		l.finishTerminal(level, message)
		return
	}

	// Mask, de-duplicate and truncate once, before either the hooks or the
	// encoder see the fields.
	fields = l.normalizeFields(fields)
	resolvedMessage := l.redactMessage(message)

	for _, reg := range l.hooksSnapshot() {
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

	entry := Entry{
		Level:   level,
		Message: resolvedMessage,
		Fields:  fields,
		Time:    at,
	}
	if entry.Time.IsZero() {
		entry.Time = l.now()
	}
	if caller != nil {
		entry.Caller = *caller
	}

	// One pooled buffer per entry, encoded once and written once. The encoder
	// appends, so nothing here allocates per field.
	bufp := encodeBufPool.Get().(*[]byte)
	*bufp = eh.e.Encode((*bufp)[:0], entry)
	payload := *bufp

	err := l.writePayload(level, writer, payload)
	encodeBufPool.Put(bufp)

	l.reportWriteError(err)
	l.finishTerminal(level, resolvedMessage)
}

// writePayload writes one encoded entry, taking the output lock as configured
// and mirroring to any level-scoped outputs.
func (l *Logger) writePayload(level LogLevel, writer io.Writer, payload []byte) error {
	var err error
	write := func() {
		if _, e := writer.Write(payload); e != nil && err == nil {
			err = e
		}
		if lop := l.levelOutputs.Load(); lop != nil {
			for _, lo := range *lop {
				if level >= lo.min {
					if _, e := lo.w.Write(payload); e != nil && err == nil {
						err = e
					}
				}
			}
		}
	}
	if l.syncWrites.Load() {
		l.writeMu.Lock()
		write()
		l.writeMu.Unlock()
	} else {
		// Shared rather than unguarded: concurrent writes still proceed in
		// parallel, but a writer swap or Close can wait them out instead of
		// closing a writer somebody is still using.
		l.writeMu.RLock()
		write()
		l.writeMu.RUnlock()
	}
	return err
}

// finishTerminal carries out what a terminal level promises, after its entry
// has been written.
func (l *Logger) finishTerminal(level LogLevel, message string) {
	switch level {
	case FATAL:
		os.Exit(1)
	case PANIC:
		panic(message)
	}
}

// now returns the entry timestamp, honoring a clock installed for tests.
func (l *Logger) now() time.Time {
	if h := l.clock.Load(); h != nil && h.fn != nil {
		return h.fn()
	}
	return time.Now()
}

// Close releases any resources owned by the logger, such as open files. Writers
// superseded by a later setOutputWithCloser are closed at the point they are
// replaced, so only the current one is left to close here. The close error, if
// any, is returned.
func (l *Logger) Close() error {
	l.disableAsync()
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

// logf formats and logs a message, but only once the level has been checked --
// the point of the *f methods over Info(fmt.Sprintf(...)), which formats whether
// or not the entry survives.
func (l *Logger) logf(level LogLevel, format string, args ...interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}
	l.logEntry(level, fmt.Sprintf(format, args...), nil, nil)
}

// Trace logs a message at TRACE with optional structured fields.
func (l *Logger) Trace(message string, fields ...Field) {
	l.logEntry(TRACE, message, fields, nil)
}

// Tracef logs a formatted message at TRACE. The message is formatted only
// if TRACE is enabled.
func (l *Logger) Tracef(format string, args ...interface{}) {
	l.logf(TRACE, format, args...)
}

// TraceContext logs at TRACE, adding any fields the context carries.
func (l *Logger) TraceContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(TRACE, message, l.mergeContextFields(ctx, fields), nil)
}

// Panic logs a message at PANIC with optional structured fields and then panics.
func (l *Logger) Panic(message string, fields ...Field) {
	l.logEntry(PANIC, message, fields, nil)
}

// Panicf logs a formatted message at PANIC and then panics. The message is formatted only
// if PANIC is enabled.
func (l *Logger) Panicf(format string, args ...interface{}) {
	l.logf(PANIC, format, args...)
}

// PanicContext logs at PANIC, adding any fields the context carries and then panics.
func (l *Logger) PanicContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(PANIC, message, l.mergeContextFields(ctx, fields), nil)
}

// Debug logs a message at DEBUG with optional structured fields.
func (l *Logger) Debug(message string, fields ...Field) {
	l.logEntry(DEBUG, message, fields, nil)
}

// Debugf logs a formatted message at DEBUG. The message is formatted only
// if DEBUG is enabled.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// DebugContext logs at DEBUG, adding any fields the context carries.
func (l *Logger) DebugContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(DEBUG, message, l.mergeContextFields(ctx, fields), nil)
}

// Info logs a message at INFO with optional structured fields.
func (l *Logger) Info(message string, fields ...Field) {
	l.logEntry(INFO, message, fields, nil)
}

// Infof logs a formatted message at INFO. The message is formatted only
// if INFO is enabled.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// InfoContext logs at INFO, adding any fields the context carries.
func (l *Logger) InfoContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(INFO, message, l.mergeContextFields(ctx, fields), nil)
}

// Warn logs a message at WARN with optional structured fields.
func (l *Logger) Warn(message string, fields ...Field) {
	l.logEntry(WARN, message, fields, nil)
}

// Warnf logs a formatted message at WARN. The message is formatted only
// if WARN is enabled.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

// WarnContext logs at WARN, adding any fields the context carries.
func (l *Logger) WarnContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(WARN, message, l.mergeContextFields(ctx, fields), nil)
}

// Error logs a message at ERROR with optional structured fields.
func (l *Logger) Error(message string, fields ...Field) {
	l.logEntry(ERROR, message, fields, nil)
}

// Errorf logs a formatted message at ERROR. The message is formatted only
// if ERROR is enabled.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// ErrorContext logs at ERROR, adding any fields the context carries.
func (l *Logger) ErrorContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(ERROR, message, l.mergeContextFields(ctx, fields), nil)
}

// Fatal logs a message at FATAL with optional structured fields and exits the application.
func (l *Logger) Fatal(message string, fields ...Field) {
	l.logEntry(FATAL, message, fields, nil)
}

// Fatalf logs a formatted message at FATAL and exits the application. The message is formatted only
// if FATAL is enabled.
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logf(FATAL, format, args...)
}

// FatalContext logs at FATAL, adding any fields the context carries and exits the application.
func (l *Logger) FatalContext(ctx context.Context, message string, fields ...Field) {
	l.logEntry(FATAL, message, l.mergeContextFields(ctx, fields), nil)
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
	switch dropStrategy(st.strategy.Load()) {
	case dropOldest:
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
	case blockWhenFull:
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
