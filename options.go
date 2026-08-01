package log

import (
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Option configures a Logger at construction. Options are applied in order, so
// a later one wins where two set the same thing.
type Option func(*builder) error

// builder accumulates option state so that construction can validate before
// anything is opened, and so options stay ordinary values rather than mutations
// of a half-built logger.
type builder struct {
	level   LogLevel
	output  io.Writer
	closer  io.Closer
	encoder Encoder

	caller     bool
	callerSkip int
	stacktrace bool
	colorize   bool
	timeLayout string
	clock      func() time.Time

	unsynchronized  bool
	dedupeFields    bool
	maxFieldBytes   int
	maxMessageBytes int

	sampler      Sampler
	redactor     Redactor
	extractor    ContextExtractorFunc
	errHandler   func(error)
	hooks        []hookSpec
	levelOutputs []levelOutput

	async     bool
	asyncOpts asyncConfig
}

type hookSpec struct {
	hook Hook
	opts []HookOption
}

// asyncConfig mirrors the async worker's knobs. It is unexported: queue
// mechanics are tuning, not vocabulary the caller should have to learn.
type asyncConfig struct {
	queueSize     int
	strategy      dropStrategy
	batchSize     int
	flushInterval time.Duration
}

// New builds a Logger. With no options it writes text to stdout at INFO.
//
//	logger, err := log.New(log.Production(), log.WithFile("app.log"))
//
// It returns an error only for options that can fail, such as opening a file.
func New(opts ...Option) (*Logger, error) {
	b := &builder{
		level:     INFO,
		output:    os.Stdout,
		extractor: defaultContextExtractor,
		asyncOpts: asyncConfig{queueSize: 1024, batchSize: 1},
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(b); err != nil {
			return nil, err
		}
	}
	return b.build(), nil
}

// Must wraps New for cases where a failure should stop the program, such as
// package-level initialisation.
//
//	var logger = log.Must(log.New(log.Production()))
func Must(l *Logger, err error) *Logger {
	if err != nil {
		panic("logger: " + err.Error())
	}
	return l
}

func (b *builder) build() *Logger {
	if b.encoder == nil {
		b.encoder = &TextEncoder{Colorize: b.colorize, TimeLayout: b.timeLayout}
	}

	l := &Logger{loggerCore: &loggerCore{}}
	l.level.Store(int32(b.level))
	l.output.Store(&writerHolder{w: b.output})
	l.encoder.Store(&encoderHolder{e: b.encoder})
	l.wantCaller.Store(b.caller)
	if b.clock != nil {
		l.clock.Store(&clockHolder{fn: b.clock})
	}
	l.syncWrites.Store(!b.unsynchronized)
	l.extractor.Store(&extractorHolder{fn: b.extractor})
	l.sampler.Store((*samplerHolder)(nil))
	l.includeStacktrace.Store(b.stacktrace)
	l.dedupeFields.Store(b.dedupeFields)
	l.maxFieldBytes.Store(int64(b.maxFieldBytes))
	l.maxMessageBytes.Store(int64(b.maxMessageBytes))
	l.callerSkip.Store(int32(b.callerSkip))
	l.closer = b.closer

	if b.sampler != nil {
		l.sampler.Store(&samplerHolder{fn: b.sampler})
	}
	if b.redactor != nil {
		l.redactor.Store(&redactorHolder{r: b.redactor})
	}
	if b.errHandler != nil {
		l.errHandler.Store(&errHandlerHolder{fn: b.errHandler})
	}
	for _, h := range b.hooks {
		l.AddHook(h.hook, h.opts...)
	}
	if len(b.levelOutputs) > 0 {
		outs := append([]levelOutput(nil), b.levelOutputs...)
		l.levelOutputs.Store(&outs)
	}
	if b.async {
		l.enableAsync(b.asyncOpts)
	}
	return l
}

// --- destination ------------------------------------------------------------

// WithOutput writes entries to w.
func WithOutput(w io.Writer) Option {
	return func(b *builder) error {
		if w == nil {
			w = io.Discard
		}
		b.output = w
		return nil
	}
}

// WithOutputs writes every entry to all of the given writers.
func WithOutputs(writers ...io.Writer) Option {
	return func(b *builder) error {
		switch len(writers) {
		case 0:
			b.output = io.Discard
		case 1:
			b.output = writers[0]
		default:
			b.output = io.MultiWriter(writers...)
		}
		return nil
	}
}

// WithFile appends to the named file, creating it if needed. The logger closes
// it on Close.
func WithFile(path string) Option {
	return func(b *builder) error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		b.output = f
		b.closer = f
		return nil
	}
}

// Rotation configures size-based log rotation. A zero field takes its default:
// 100 MB, 30 days, 7 backups, compressed.
type Rotation struct {
	MaxSizeMB  int
	MaxAgeDays int
	MaxBackups int
	NoCompress bool
}

// WithRotatingFile appends to the named file, rotating it as configured.
func WithRotatingFile(path string, r Rotation) Option {
	return func(b *builder) error {
		if r.MaxSizeMB <= 0 {
			r.MaxSizeMB = 100
		}
		if r.MaxAgeDays <= 0 {
			r.MaxAgeDays = 30
		}
		if r.MaxBackups <= 0 {
			r.MaxBackups = 7
		}
		lj := &lumberjack.Logger{
			Filename:   path,
			MaxSize:    r.MaxSizeMB,
			MaxAge:     r.MaxAgeDays,
			MaxBackups: r.MaxBackups,
			Compress:   !r.NoCompress,
		}
		b.output = lj
		b.closer = lj
		return nil
	}
}

// WithLevelOutput mirrors entries at or above minLevel to an additional writer,
// for example errors to an alerting sink. Writers added here are not closed by
// Close.
func WithLevelOutput(minLevel LogLevel, w io.Writer) Option {
	return func(b *builder) error {
		if w != nil {
			b.levelOutputs = append(b.levelOutputs, levelOutput{min: minLevel, w: w})
		}
		return nil
	}
}

// --- encoding ---------------------------------------------------------------

// WithLevel sets the minimum level that will be emitted.
func WithLevel(level LogLevel) Option {
	return func(b *builder) error {
		b.level = level
		return nil
	}
}

// WithJSON encodes entries as JSON, the usual choice for machine ingestion.
func WithJSON() Option {
	return func(b *builder) error {
		b.encoder = &JSONEncoder{TimeLayout: b.timeLayout}
		return nil
	}
}

// WithConsole encodes entries for a terminal: dimmed timestamps, colored
// levels, aligned fields.
func WithConsole() Option {
	return func(b *builder) error {
		b.encoder = &ConsoleEncoder{TimeLayout: b.timeLayout, NoColor: !b.colorize}
		return nil
	}
}

// WithEncoder installs a custom encoder.
func WithEncoder(f Encoder) Option {
	return func(b *builder) error {
		if f == nil {
			return fmt.Errorf("logger: encoder cannot be nil")
		}
		b.encoder = f
		return nil
	}
}

// WithColor enables ANSI color in the text and console encoders.
func WithColor() Option {
	return func(b *builder) error {
		b.colorize = true
		return retrofit(b)
	}
}

// WithTimeFormat sets the timestamp layout, in the usual Go reference-time form.
func WithTimeFormat(layout string) Option {
	return func(b *builder) error {
		b.timeLayout = layout
		return retrofit(b)
	}
}

// WithClock replaces the timestamp source, which makes output deterministic in
// tests.
func WithClock(now func() time.Time) Option {
	return func(b *builder) error {
		b.clock = now
		return retrofit(b)
	}
}

// retrofit re-applies presentation settings to an encoder already chosen, so
// option order does not matter for the built-in encoders.
func retrofit(b *builder) error {
	switch f := b.encoder.(type) {
	case *TextEncoder:
		f.Colorize, f.TimeLayout = b.colorize, b.timeLayout
	case *JSONEncoder:
		f.TimeLayout = b.timeLayout
	case *ConsoleEncoder:
		f.TimeLayout, f.NoColor = b.timeLayout, !b.colorize
	}
	return nil
}

// --- call sites -------------------------------------------------------------

// WithCaller records the file and line that logged each entry.
func WithCaller() Option {
	return func(b *builder) error {
		b.caller = true
		return nil
	}
}

// WithCallerSkip ignores skip additional stack frames when resolving the call
// site. Use it when the logger is reached through a wrapper, so entries point at
// the code that called the wrapper rather than at the wrapper itself.
func WithCallerSkip(skip int) Option {
	return func(b *builder) error {
		if skip < 0 {
			return fmt.Errorf("logger: caller skip cannot be negative")
		}
		b.callerSkip = skip
		return nil
	}
}

// WithStacktrace attaches the caller's stack to entries at ERROR and above.
func WithStacktrace() Option {
	return func(b *builder) error {
		b.stacktrace = true
		return nil
	}
}

// --- behavior --------------------------------------------------------------

// WithSampler installs a sampler that drops entries before they are formatted.
func WithSampler(s Sampler) Option {
	return func(b *builder) error {
		b.sampler = s
		return nil
	}
}

// WithRedactor masks fields, and the message when the redactor implements
// MessageRedactor, before formatting and hook dispatch.
func WithRedactor(r Redactor) Option {
	return func(b *builder) error {
		b.redactor = r
		return nil
	}
}

// WithHook registers a hook fired for every emitted entry.
func WithHook(h Hook, opts ...HookOption) Option {
	return func(b *builder) error {
		if h != nil {
			b.hooks = append(b.hooks, hookSpec{hook: h, opts: opts})
		}
		return nil
	}
}

// WithContextExtractor controls how context values become fields on the
// *Context logging methods.
func WithContextExtractor(fn ContextExtractorFunc) Option {
	return func(b *builder) error {
		b.extractor = fn
		return nil
	}
}

// WithErrorHandler is called when a write to the output fails. Without one such
// failures are counted (see WriteErrors) but otherwise silent.
func WithErrorHandler(fn func(error)) Option {
	return func(b *builder) error {
		b.errHandler = fn
		return nil
	}
}

// WithDeduplicateFields keeps only the last value for a repeated field key.
func WithDeduplicateFields() Option {
	return func(b *builder) error {
		b.dedupeFields = true
		return nil
	}
}

// WithMaxFieldBytes truncates string field values longer than n bytes.
func WithMaxFieldBytes(n int) Option {
	return func(b *builder) error {
		b.maxFieldBytes = n
		return nil
	}
}

// WithMaxMessageBytes truncates messages longer than n bytes.
func WithMaxMessageBytes(n int) Option {
	return func(b *builder) error {
		b.maxMessageBytes = n
		return nil
	}
}

// WithUnsynchronized stops the logger serializing writes. The writer must then
// be safe for concurrent use; in exchange, concurrent writes do not queue behind
// one another.
func WithUnsynchronized() Option {
	return func(b *builder) error {
		b.unsynchronized = true
		return nil
	}
}

// --- async ------------------------------------------------------------------

// WithAsync hands entries to a background writer, so a slow sink does not block
// the goroutine that logged. A full queue drops the newest entry; see
// WithAsyncDropOldest and WithAsyncBlocking. Close or Flush before exit.
func WithAsync() Option {
	return func(b *builder) error {
		b.async = true
		return nil
	}
}

// WithAsyncQueue sets how many entries may await writing.
func WithAsyncQueue(size int) Option {
	return func(b *builder) error {
		b.async = true
		b.asyncOpts.queueSize = size
		return nil
	}
}

// WithAsyncBatch writes entries in batches of size, flushing a partial batch
// after interval.
func WithAsyncBatch(size int, interval time.Duration) Option {
	return func(b *builder) error {
		b.async = true
		b.asyncOpts.batchSize = size
		b.asyncOpts.flushInterval = interval
		return nil
	}
}

// WithAsyncDropOldest discards the oldest queued entry when the queue is full,
// keeping the most recent history.
func WithAsyncDropOldest() Option {
	return func(b *builder) error {
		b.async = true
		b.asyncOpts.strategy = dropOldest
		return nil
	}
}

// WithAsyncBlocking blocks the caller when the queue is full, trading latency
// for never losing an entry.
func WithAsyncBlocking() Option {
	return func(b *builder) error {
		b.async = true
		b.asyncOpts.strategy = blockWhenFull
		return nil
	}
}

// --- presets ----------------------------------------------------------------

// Development configures a logger for a terminal: console encoding at DEBUG,
// with color, call sites and stacktraces. Compose it with other options, which
// take effect if they come after it.
//
//	logger, err := log.New(log.Development(), log.WithLevel(log.INFO))
func Development() Option {
	return func(b *builder) error {
		b.level = DEBUG
		b.colorize = true
		b.caller = true
		b.stacktrace = true
		b.encoder = &ConsoleEncoder{TimeLayout: b.timeLayout, NoColor: false}
		return nil
	}
}

// Production configures a logger for a running service: JSON at INFO, with call
// sites, stacktraces on errors, and asynchronous writing. Compose it with other
// options, which take effect if they come after it.
//
//	logger, err := log.New(log.Production(), log.WithFile("app.log"))
func Production() Option {
	return func(b *builder) error {
		b.level = INFO
		b.caller = true
		b.stacktrace = true
		b.encoder = &JSONEncoder{TimeLayout: b.timeLayout}
		b.async = true
		b.asyncOpts.queueSize = 4096
		b.asyncOpts.batchSize = 64
		b.asyncOpts.flushInterval = time.Second
		return nil
	}
}
