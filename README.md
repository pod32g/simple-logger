
# Simple Logger

[![CI](https://github.com/pod32g/simple-logger/actions/workflows/ci.yml/badge.svg)](https://github.com/pod32g/simple-logger/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/pod32g/simple-logger.svg)](https://pkg.go.dev/github.com/pod32g/simple-logger)
[![Go Report Card](https://goreportcard.com/badge/github.com/pod32g/simple-logger)](https://goreportcard.com/report/github.com/pod32g/simple-logger)

A structured logging library for Go: fast, quiet by default, and small enough to
learn in one sitting.

```go
log.Info("ready")
```

```bash
go get github.com/pod32g/simple-logger
```

Requires Go 1.25 or later.

## Getting started

The package-level functions log through a default logger writing text to stdout
at `INFO`:

```go
import log "github.com/pod32g/simple-logger"

log.Info("server started", log.Int("port", 8080))
log.Warnf("retrying in %s", backoff)
```

```text
2026-08-01 09:41:22 - [INFO] server started port=8080
2026-08-01 09:41:22 - [WARN] retrying in 250ms
```

For anything beyond that, build your own with `New` and options:

```go
logger, err := log.New(
    log.WithJSON(),
    log.WithLevel(log.DEBUG),
    log.WithFile("app.log"),
)
if err != nil {
    return err
}
defer logger.Close()
```

`New` returns an error only for options that can fail, such as opening a file.
`log.Must(log.New(...))` panics instead, which suits package-level variables.

### Presets

Two presets cover the usual cases, and compose with any option after them:

```go
logger := log.Must(log.New(log.Development()))                     // console, color, DEBUG, call sites
logger := log.Must(log.New(log.Production(), log.WithFile("app.log")))  // JSON, async, stacktraces
```

## Logging

Each level has three forms, and no more:

```go
logger.Info("user login", log.String("user", "alice"), log.Int("attempt", 3))
logger.Infof("listening on %s", addr)
logger.InfoContext(ctx, "handled", log.Int("status", 200))
```

Levels are `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `PANIC` and `FATAL`:

```text
2026-08-01 09:41:22 - [TRACE] entering handler
2026-08-01 09:41:22 - [DEBUG] query planned rows=128
2026-08-01 09:41:22 - [INFO] request served
2026-08-01 09:41:22 - [WARN] slow response took=2s
2026-08-01 09:41:22 - [ERROR] write failed
```

`Panic` logs and then panics; `Fatal` logs and then exits. Both drain the async
queue first, so the entries explaining the failure are written before the
process stops.

### Fields

```go
log.String, log.Int, log.Int32, log.Int64, log.Uint, log.Uint32, log.Uint64
log.Float32, log.Float64, log.Bool, log.Duration, log.Time, log.Stringer
log.Binary, log.Err, log.ErrorVerbose, log.Any
```

```go
logger.Error("upstream failed",
    log.Err("error", err),
    log.Duration("elapsed", 1500*time.Millisecond))
```

```text
2026-08-01 09:41:22 - [ERROR] upstream failed error=connection refused elapsed=1.5s
```

`log.Group` nests fields — a JSON object, or a dotted prefix in text output, so
a line-oriented record stays one line:

```go
logger.Info("request", log.String("id", "req-42"),
    log.Group("http", log.String("method", "GET"), log.Int("status", 200),
        log.Group("client", log.String("ip", "10.0.0.1"))))
```

```text
2026-08-01 09:41:22 - [INFO] request id=req-42 http.method=GET http.status=200 http.client.ip=10.0.0.1
```

```text
{"timestamp":"2026-08-01T09:41:22Z","level":"INFO","message":"request","id":"req-42","http":{"method":"GET","status":200,"client":{"ip":"10.0.0.1"}}}
```

`log.Lazy` defers work until the entry is known to be emitted, so a dropped
entry costs nothing:

```go
logger.Debug("state", log.Lazy("dump", func() any { return expensive() }))
```

### Derived loggers

```go
requestLogger := logger.With(log.String("request_id", "req-42")).Named("http")
requestLogger.Info("handling")
requestLogger.Info("complete", log.Int("status", 200))

logger.WithError(err).Error("upload failed")
```

```text
2026-08-01 09:41:22 - [INFO] handling logger=http request_id=req-42
2026-08-01 09:41:22 - [INFO] complete logger=http request_id=req-42 status=200
```

Derived loggers share the parent's output, level, hooks and async state, so a
runtime `SetLevel` reaches all of them.

## Options

| Option | Effect |
|---|---|
| `WithLevel(level)` | Minimum level to emit |
| `WithOutput(w)` / `WithOutputs(w...)` | Where entries go |
| `WithFile(path)` / `WithRotatingFile(path, Rotation{})` | File output, optionally rotated |
| `WithLevelOutput(min, w)` | Mirror entries at or above a level to another sink |
| `WithJSON()` / `WithConsole()` / `WithEncoder(e)` | Encoding |
| `WithColor()` / `WithTimeFormat(layout)` / `WithClock(fn)` | Presentation |
| `WithCaller()` / `WithCallerSkip(n)` | Record the call site |
| `WithStacktrace()` | Attach stacks at ERROR and above |
| `WithSampler(s)` / `WithRedactor(r)` / `WithHook(h, opts...)` | Behavior |
| `WithAsync()` and `WithAsyncQueue/Batch/DropOldest/Blocking` | Background writing |
| `WithMaxFieldBytes(n)` / `WithMaxMessageBytes(n)` / `WithDeduplicateFields()` | Per-entry limits |
| `WithContextExtractor(fn)` / `WithErrorHandler(fn)` / `WithUnsynchronized()` | Everything else |

Only three things change after construction, because only three have a genuine
runtime use: `SetLevel`, `SetOutput`/`SetOutputWithCloser`, and `AddHook`.

### Wrapping the logger

If you wrap the logger in your own helper, tell it how many frames to skip so
entries point at your callers rather than at your wrapper:

```go
logger := log.Must(log.New(log.WithCaller(), log.WithCallerSkip(1)))
```

## Asynchronous logging

```go
logger := log.Must(log.New(log.WithAsyncQueue(4096), log.WithAsyncBatch(64, time.Second)))
defer logger.Close()   // drains the queue
```

A full queue drops the newest entry by default; `WithAsyncDropOldest` keeps the
most recent history instead, and `WithAsyncBlocking` never loses one. `Flush`
drains without tearing the worker down, and `Sync` also fsyncs a file sink.
`AsyncStats` reports queue depth and drops.

## Redaction

```go
logger := log.Must(log.New(
    log.WithRedactor(log.NewKeyRedactor("password", "token")),
))
// or regex-based, which also scrubs the message:
log.NewPatternScrubber(regexp.MustCompile(`Bearer \S+`))
```

```go
logger.Info("login", log.String("user", "alice"), log.String("password", "hunter2"))
```

```text
2026-08-01 09:41:22 - [INFO] login user=alice password=[REDACTED]
```

Redaction runs before both the encoder and the hooks, so a secret reaches
neither — including values produced by `log.Lazy`.

## Sampling

```go
log.NewEveryNSampler(100)                        // one in every hundred
log.NewBurstSampler(time.Second, 5, 100)         // first five, then one in a hundred
log.NewLevelSampler(map[log.LogLevel]log.Sampler{log.DEBUG: log.NewEveryNSampler(10)})
```

## Encoders

An encoder is one interface with one method:

```go
type Encoder interface {
    Encode(buf []byte, e Entry) []byte
}
```

`Entry` carries the level, message, fields, timestamp and call site. Append to
`buf`, return the extended slice — the logger owns the buffer, so an encoder
allocates nothing per entry.

```go
type logfmtEncoder struct{}

func (logfmtEncoder) Encode(buf []byte, e log.Entry) []byte {
    buf = append(buf, "ts="...)
    buf = e.Time.AppendFormat(buf, time.RFC3339)
    return fmt.Appendf(buf, " level=%s msg=%q\n", e.Level, e.Message)
}

logger := log.Must(log.New(log.WithEncoder(logfmtEncoder{})))
```

Built in: `TextEncoder` (the default), `JSONEncoder`, `ConsoleEncoder`. The same
two entries through each of them:

```text
# TextEncoder
2026-08-01 09:41:22 - [INFO] server started addr=:8080
2026-08-01 09:41:22 - [ERROR] upstream failed error=connection refused

# JSONEncoder — one object per line, valid whatever the values contain
{"timestamp":"2026-08-01T09:41:22Z","level":"INFO","message":"server started","addr":":8080"}
{"timestamp":"2026-08-01T09:41:22Z","level":"ERROR","message":"upstream failed","error":"connection refused"}

# ConsoleEncoder — dimmed timestamp, colored and padded level, values quoted
# when they contain spaces (color not shown here)
09:41:22.461 INFO  server started addr=:8080
09:41:22.461 ERROR upstream failed error="connection refused"
```

`WithCaller()` adds the file and line that logged, resolved on the goroutine
that logged even in async mode:

```text
2026-08-01 09:41:22 - main.go:42 - [INFO] with call site
```

Read the call site off `Entry.Caller` rather than walking the stack yourself:
in async mode your encoder runs on the worker goroutine, whose stack says
nothing about who logged. The logger resolves it on the goroutine that logged
and hands it over. `Entry.Time` works the same way, and is zero when a record
genuinely has no timestamp — emit nothing rather than substituting the current
time.

To change how one type renders without writing an encoder, use
`WithFieldEncoder`. It is scoped to the logger it builds, so a library cannot
change how every logger in your process renders a type:

```go
log.New(log.WithFieldEncoder(
    func(id UserID) (string, bool) { return id.String(), true },
    func(id UserID) (any, bool)    { return id.String(), true },
))
```

## Configuration from data

For configuration that arrives as a file or environment variables:

```go
cfg, err := log.LoadConfigFromFile("logger.json")   // or log.LoadConfigFromEnv()
logger, err := log.FromConfig(cfg)
```

```json
{
  "level": "debug",
  "output": "app.log",
  "format": "json",
  "enable_caller": true,
  "rotate": true,
  "rotation": {"max_size_mb": 50, "max_age_days": 30, "max_backups": 7}
}
```

Levels are written by name. Numbers are rejected: a numeric level means whatever
position that level happens to occupy, so adding one silently changes what an
existing config file means.

### Hot reload

```go
go logger.Watch(ctx, "logger.json",
    log.WatchInterval(5*time.Second),
    log.WatchErrorHandler(func(err error) { logger.Warn("reload failed", log.Err("error", err)) }))

go logger.ReloadFrom(ctx, configs)   // or push Config values down a channel
```

A failed reload is reported and the watcher keeps running.

| Variable | Description |
|---|---|
| `LOG_LEVEL` | `trace`..`fatal` |
| `LOG_OUTPUT` | `stdout`, `stderr`, or a file path |
| `LOG_FORMAT` | `text`, `json`, `console` |
| `LOG_ENABLE_CALLER` | Include the call site |
| `LOG_INCLUDE_STACKTRACE` | Stacks on error entries |
| `LOG_COLORIZE` | ANSI color |
| `LOG_TIME_FORMAT` | Go time layout |
| `LOG_SYNC_WRITES` | `false` to stop serializing writes |
| `LOG_ROTATE`, `LOG_ROTATE_MAX_SIZE`, `LOG_ROTATE_MAX_AGE`, `LOG_ROTATE_MAX_BACKUPS`, `LOG_ROTATE_COMPRESS` | Rotation |

## Bridges

Each bridge is a subpackage; the gRPC one is a separate module so gRPC stays out
of your dependency graph.

**`bridge/slogbridge`** — route `log/slog` through this logger. It passes the
standard library's own `testing/slogtest` conformance suite in full, including
nested groups, `Record.Time`, `Record.PC` and the empty-attribute rules.

```go
handler := slogbridge.NewHandler(logger, nil)   // nil: follow the logger's level
slog.SetDefault(slog.New(handler))
```

**`bridge/httplog`** — runtime level control over HTTP, and request-ID
propagation.

```go
mux.Handle("/loglevel", httplog.LevelHandler(logger))   // GET/PUT {"level":"debug"}
handler = httplog.RequestIDMiddleware("X-Request-Id")(handler)
```

**`bridge/oteltrace`** — put the active trace and span IDs on every
context-aware entry.

```go
logger := log.Must(log.New(log.WithContextExtractor(oteltrace.Chain)))
```

**`bridge/otlp`** — export entries to an OTLP collector. Records are batched by
a background worker and each export is time-bounded, so a log call never waits
on the collector. **Close the hook before exit**, or queued records are lost.

```go
hook := otlp.NewHook(exporter, otlp.WithServiceName("checkout"))
logger.AddHook(hook)
defer hook.Close(ctx)
```

**`bridge/grpclog`** — unary and stream interceptors recording method, status
code, latency and request IDs.

```go
go get github.com/pod32g/simple-logger/bridge/grpclog

grpc.NewServer(
    grpc.UnaryInterceptor(grpclog.UnaryServerInterceptor(logger)),
    grpc.StreamInterceptor(grpclog.StreamServerInterceptor(logger)),
)
```

## Testing

`logtest` records entries in memory so tests can assert on them without parsing
output:

```go
observer, logger := logtest.New(log.DEBUG)

// exercise the code under test

entry := observer.AssertLogged(t, "cache miss")
if got, _ := entry.Field("http.status"); got != 500 {
    t.Errorf("status = %v", got)
}
if observer.FilterLevel(log.ERROR).Len() != 0 {
    t.Error("no errors expected")
}
```

The observer is a hook, so it sees exactly what hooks see: after redaction and
field normalization.

## Capturing output from other libraries

`Writer` turns each line written to it into an entry, which is how code that
only knows how to write lines gets into structured logging:

```go
stdlog.SetOutput(logger.Writer())
srv.ErrorLog = stdlog.New(logger.WriterLevel(log.ERROR), "", 0)
```

## Operational helpers

```go
defer logger.Recover()             // log a panic with its stack, then re-panic
defer logger.RecoverAndContinue()  // log it and carry on

logger.WriteErrors()               // count of failed sink writes
log.New(log.WithErrorHandler(func(err error) { /* sink write failed */ }))
```

## Performance

Apple M4 Pro, Go 1.26.5, one entry with one typed field to `io.Discard`:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| zerolog | 46.8 | 0 | 0 |
| **simple-logger** | **118.6** | **40** | **2** |
| simple-logger (JSON) | 131.7 | 40 | 2 |
| zap | 163.6 | 64 | 1 |
| zap (sugared) | 201.4 | 136 | 2 |
| logrus | 914.3 | 1258 | 22 |

Every library logs the same message with the same typed field, so the numbers
compare encoders rather than calling styles. Reproduce with `make bench`.

Enabling `WithCaller` costs roughly 370ns per entry, which is why it is off by
default.

## Development

```bash
make check    # gofmt, vet, tests
make race     # tests under the race detector
make bench    # benchmarks
make fuzz     # fuzz the JSON encoder
make ci       # everything CI runs
```

## Known limitations

- **Caller reporting costs about 220ns per entry**, because resolving a call
  site means asking the runtime to unwind the stack. That is why it is off by
  default. For scale, the same benchmark puts zap's `AddCaller` at 373ns
  against our 337ns, so this is the going rate rather than something to tune
  around.

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Issues and pull requests welcome. `make ci` should pass before you open one.
