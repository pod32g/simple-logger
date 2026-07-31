
# Simple Logger

[![CI](https://github.com/pod32g/simple-logger/actions/workflows/ci.yml/badge.svg)](https://github.com/pod32g/simple-logger/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/pod32g/simple-logger.svg)](https://pkg.go.dev/github.com/pod32g/simple-logger)
[![Go Report Card](https://goreportcard.com/badge/github.com/pod32g/simple-logger)](https://goreportcard.com/report/github.com/pod32g/simple-logger)

## Description

**Simple Logger** is a lightweight, flexible logging library for Go (Golang) that supports multiple log levels, customizable output formats, including plain text and JSON, and allows for user-defined custom formats. It is designed to be easy to integrate into your projects, with minimal configuration required.

## Features

- Supports multiple log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.
- Customizable output destinations (e.g., stdout, stderr, or files).
- Supports plain text, JSON, and custom log formats.
- Simple API for setting log levels, outputs, and formats.
- Dynamic configuration updates at runtime.
- Thread-safe logging: concurrent log calls are serialized to keep entries intact, with an opt-out switch when you control the writer.
- Optional caller information, disabled by default to minimize overhead.
- Secure file output: logs written via `ApplyConfig` use `0600` permissions.
- Structured fields via helper functions (`String`, `Int`, `Bool`, `Any`, etc.) for nicer JSON output.
- Context-aware logging helpers to pull request-scoped metadata from `context.Context`.
- Configurable sampling controls to keep noisy hot paths under control.
- Hooks and multi-sink outputs for forwarding logs to additional destinations.
- Optional asynchronous mode with configurable buffers and drop policies.
- Hook filters and async stats so you can limit callbacks and monitor queue health.
- Hot reload helpers for watching config files or reacting to config pushes.
- Derived loggers with bound fields (`With`), component naming (`Named`), and a process-wide `Default` logger.
- Sensitive-data redaction (key-based and regex), field de-duplication, and size limits.
- Trace correlation via an OpenTelemetry context extractor and OTLP `trace_id`/`span_id` promotion.
- Operational helpers: `Flush`/`Sync`, sink write-error callbacks, per-level output mirroring, and a panic-capturing `Recover`.
- A human-friendly `ConsoleFormatter` and an `httplog` subpackage for runtime level control and request-ID propagation.

## Installation

You can install the Simple Logger package using `go get`:

```bash
go get github.com/pod32g/simple-logger
```

## Usage

### Basic Example

Here’s a simple example of how to use Simple Logger in your project:

```go
package main

import (
	log "github.com/pod32g/simple-logger"
	"os"
)

func main() {
	// Create a new logger instance with the default formatter
	logger := log.NewLogger(os.Stdout, log.INFO, &log.DefaultFormatter{})
	defer logger.Close()

	// Log messages at different levels
	logger.Debug("This is a debug message")
	logger.Info("This is an info message")
	logger.Warn("This is a warning message")
	logger.Error("This is an error message")
	logger.Fatal("This is a fatal message") // This will log the message and exit the application
}
```

### Example: Using `LoggerConfig`

You can configure the logger using the `LoggerConfig` struct for more control over logging behavior:

```go
package main

import (
    log "github.com/pod32g/simple-logger"
)

func main() {
    config := log.LoggerConfig{
        Level:        log.DEBUG,
        Output:       "stdout",
        Format:       "json",
        EnableCaller: true,
        SyncWrites:   true, // disable when you control the writer and need maximum throughput
    }

    logger := log.ApplyConfig(config)
    defer logger.Close()

    logger.Debug("This is a debug message with caller info.")
    logger.Info("This is an info message in JSON format.")
    logger.Warn("This is a warning message.")
    logger.Error("This is an error message.")
}
```

### Configuring Log Levels

You can set the logging level to control the verbosity of the logger. Available levels are `DEBUG`, `INFO`, `WARN`, `ERROR`, and `FATAL`.

In a JSON config file the level may be written either as a name or as a number,
matching `LOG_LEVEL` and the HTTP level endpoint:

```json
{"level": "debug", "format": "json"}
```

#### Example: Changing Log Level at Runtime

```go
package main

import (
    log "github.com/pod32g/simple-logger"
    "os"
)

func main() {
    logger := log.NewLogger(os.Stdout, log.INFO, &log.DefaultFormatter{})
    defer logger.Close()

    logger.Info("Initial log level is Info.")

    // Changing log level to Debug
    logger.SetLevel(log.DEBUG)
    logger.Debug("Now logging at Debug level.")
}
```

### Logging to a File

You can log messages to a file by specifying the filename in the `Output` field of the `LoggerConfig` struct:

```go
package main

import (
    log "github.com/pod32g/simple-logger"
)

func main() {
    config := log.LoggerConfig{
        Level:        log.INFO,
        Output:       "app.log",  // Specify the filename here
        Format:       "text",
        EnableCaller: false,
        SyncWrites:   true,
    }

    logger := log.ApplyConfig(config)
    defer logger.Close()

    logger.Info("This message will be logged to a file.")
}
```

Alternatively, you can change the log output to a file or any other `io.Writer`:

```go
package main

import (
	log "github.com/pod32g/simple-logger"
	"os"
)

func main() {
	// Open a file for logging
	file, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		panic("Failed to open log file: " + err.Error())
	}
	defer file.Close()

	// Create a new logger instance that writes to the file with the default formatter
	logger := log.NewLogger(file, log.INFO, &log.DefaultFormatter{})
	defer logger.Close()

	logger.Info("Logging to a file now!")
}
```

### Using a Custom Formatter

You can create and use a custom formatter by implementing the `CustomFormatter` interface:

```go
package main

import (
	log "github.com/pod32g/simple-logger"
	"fmt"
)

func main() {
	config := log.DefaultConfig()
	config.Format = "custom"
	config.Custom = &MyCustomFormatter{} // Provide your custom formatter

	logger := log.ApplyConfig(config)
	defer logger.Close()

	logger.Info("This is an info message with a custom format.")
	logger.Debug("This is a debug message with a custom format.")
}

// MyCustomFormatter is a sample custom formatter
type MyCustomFormatter struct{}

func (f *MyCustomFormatter) Format(level log.LogLevel, message string) string {
	return fmt.Sprintf("**CUSTOM LOG** [%s] %s\n", logLevelToString(level), message)
}

func logLevelToString(level log.LogLevel) string {
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
		return "UNKNOWN"
	}
}
```

### Structured Logging with Fields

Use the field helpers to emit structured key/value pairs alongside your message.
The default formatter renders them as `key=value`, while the JSON formatter
adds them as additional properties.

```go
logger := log.NewLogger(os.Stdout, log.INFO, &log.JSONFormatter{})
defer logger.Close()

logger.InfoFields(
	"user login",
	log.String("user", "alice"),
	log.Int("attempt", 3),
	log.Bool("success", true),
)
```

Field helpers include `String`, `Int`, `Int64`, `Uint`, `Float64`, `Bool`,
`Error`, and `Any` for arbitrary values. You can mix traditional variadic calls
with structured logging as needed.

### Formatted Messages

`Debugf`, `Infof`, `Warnf`, `Errorf`, and `Fatalf` take a format string:

```go
logger.Errorf("upload %s failed after %d retries", name, attempts)
```

They check the level before formatting, so a `Debugf` on a hot path costs
nothing when `DEBUG` is disabled — unlike `logger.Debug(fmt.Sprintf(...))`, which
formats whether or not the entry survives. Because they follow the printf naming
convention, `go vet` checks the format strings at your call sites.

### Derived Loggers with Bound Fields

Use `With` to derive a logger that carries persistent fields on every entry —
ideal for per-request or per-component loggers (`request_id`, `user`,
`component`, ...). The derived logger is a `*Logger`, so it drops straight into
any API that expects one (HTTP middleware, gRPC interceptors, the `slog` bridge).

```go
reqLogger := logger.With(
    log.String("request_id", rid),
    log.String("user", userID),
)

reqLogger.Info("processing payment")          // includes request_id and user
reqLogger.Error("charge failed", log.Error("error", err))
```

Bound fields appear ahead of per-call and context-extracted fields. Derived
loggers **share** the parent's output, level, hooks, and async state, so runtime
changes (`SetLevel`, `SetOutput`, `AddHook`, `EnableAsync`, ...) made through any
logger in the family are observed by all of them.

### Gating Expensive Logs

`Level()` returns the current level and `Enabled(level)` reports whether an entry
would be emitted, so you can skip building costly fields on hot paths:

```go
if logger.Enabled(log.DEBUG) {
    logger.DebugFields("state", expensiveSnapshot()...)
}
```

### Redaction & Sensitive Data

Mask credentials and PII before they reach any formatter or sink (including the
OTLP exporter):

```go
// Key-based: redact fields named password/token/authorization (case-insensitive).
logger.SetRedactor(log.NewKeyRedactor("password", "token", "authorization"))

// Pattern-based: scrub secret shapes from field values and messages.
logger.SetRedactor(log.NewPatternScrubber(
    regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`),
))
```

You can also cap log size with `SetMaxFieldBytes`/`SetMaxMessageBytes` and collapse
duplicate keys (last-wins) with `SetDeduplicateFields(true)`.

### Default Logger

For zero-config logging, use the process-wide default (swap it once at startup):

```go
log.SetDefault(log.ApplyConfig(log.DefaultConfig()))
log.Default().Info("service started")
```

### Flushing & Durability

In async mode, drain the queue at well-defined points without tearing down the
worker, and fsync the file sink:

```go
logger.Flush()        // block until queued entries are written
if err := logger.Sync(); err != nil { /* flush + fsync the file */ }
```

### Robustness Helpers

```go
logger.SetErrorHandler(func(err error) { /* sink write failed */ })

go func() {
    defer logger.Recover() // log a panic (with stack) and re-panic
    // ...
}()
```

### Trace Correlation (OpenTelemetry)

Automatically attach `trace_id`/`span_id` to every context-aware log, and have the
OTLP bridge promote them to first-class record fields:

```go
import "github.com/pod32g/simple-logger/bridge/oteltrace"

logger.SetContextExtractor(oteltrace.Chain) // trace IDs + log.WithFields data
logger.InfoContext(ctx, "handling request")  // emits trace_id, span_id
```

### HTTP Helpers

The `bridge/httplog` subpackage offers runtime level control and request-ID
propagation:

```go
import "github.com/pod32g/simple-logger/bridge/httplog"

mux.Handle("/admin/loglevel", httplog.LevelHandler(logger)) // GET/PUT the level
handler = httplog.RequestIDMiddleware("")(handler)          // request_id per request
```

#### Custom Field Encoders

Use `RegisterFieldEncoder` to customize how specific types render in text and
JSON output:

```go
log.RegisterFieldEncoder[time.Duration](
    func(d time.Duration) (string, bool) { return d.String(), true },
    func(d time.Duration) (interface{}, bool) { return d.Seconds(), true },
)

logger.InfoFields("timed", log.Any("duration", 150*time.Millisecond))
```

Built-in encoders already cover `time.Duration`, `time.Time`, and `error`.

### Context-Aware Logging

Attach request metadata to a `context.Context` and have it automatically included
with every log entry.

```go
ctx := log.WithFields(context.Background(),
	log.String("request_id", rid),
	log.String("user", userID),
)

logger.InfoContext(ctx, "processing payment",
	log.String("invoice", invoiceID),
)
```

Use `SetContextExtractor` to plug in a custom extractor when you already embed
structured data in a different context key.

### Sampling & Rate Limiting

Install a sampler when you want to suppress noisy log entries without changing call
sites. For example, log only every other message:

```go
logger.SetSampler(log.NewEveryNSampler(2))
```

Provide your own implementation by satisfying the `Sampler` interface or using
`SamplerFunc` for quick custom logic.

### Hooks & Multi-Sink Outputs

Forward log entries to additional systems—metrics, alerts, or alternate writers.

```go
logger.SetOutputs(os.Stdout, auditFile)

logger.AddHook(log.HookFunc(func(level log.LogLevel, msg string, fields []log.Field) {
	metrics.Inc(level, msg)
}))
```

Hooks run after sampling and before the final write, receiving the resolved
message and structured fields.

### Hook Filters & Async Stats

Limit callbacks to specific levels or field predicates using hook options:

```go
logger.AddHook(metricsHook,
    log.WithHookLevels(log.ERROR, log.FATAL),
    log.WithHookFilter(func(level log.LogLevel, message string, fields []log.Field) bool {
        return strings.Contains(message, "payment")
    }),
)
```

Async helpers expose queue metrics:

```go
stats := logger.AsyncStats()
fmt.Printf("length=%d dropped=%d\n", stats.QueueLength, stats.Dropped)
```

Use `SetDropStrategy` to switch between dropping new entries, dropping oldest, or
blocking when the queue is full.

### Hot Reload Helpers

Reload configuration from a JSON file or your own control plane without wiring
up boilerplate:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

if err := log.WatchConfigFileForLogger(ctx, logger, "./logger.json", 2*time.Second, func(err error) {
    logger.Error("config reload failed", log.Error("error", err))
}); err != nil {
    panic(err)
}
```

You can also push configs through a channel:

```go
configs := make(chan log.LoggerConfig, 1)
go log.ReloadLoggerFromChannel(ctx, logger, configs, func(err error) {
    logger.Warn("config update rejected", log.Error("error", err))
})

configs <- log.LoggerConfig{Level: log.DEBUG, Output: "stdout", Format: "json"}
```

Both helpers reuse `ConfigureLogger` under the hood, so they understand rotation,
formatters, and other options.

### Additional Examples

Several runnable examples demonstrate integrations beyond the basics:

- `example/http_middleware`: Wrap standard `net/http` handlers with structured request logging.
- `example/grpc_interceptor`: Attach a unary interceptor that records request IDs, latency, and errors.
- `example/cli_dynamic`: Small CLI that watches config files and accepts live tweaks from STDIN.

### End-to-End Tests

Integration tests under `e2e/` exercise configuration reload paths, async drop
metrics, hook filters, and bridge integrations end-to-end. Run

```bash
go test ./e2e
```

or `go test ./...` to include them in the full suite.

### Additional QA Checks

- Concurrency safety: `go test -race ./...`
- Static analysis: `staticcheck ./...` (install via `go install honnef.co/go/tools/cmd/staticcheck@latest`)
- Fuzzing JSON formatter stability: `go test -fuzz=FuzzJSONFormatterFormat -run=^$`

### Asynchronous Logging

Move formatting/writes off the hot path by enabling the async worker:

```go
logger.EnableAsync(log.AsyncOptions{
    QueueSize:     1024,
    DropStrategy:  log.DropNew,
    BatchSize:     64,
    FlushInterval: 10 * time.Millisecond,
})

// ... later
logger.DisableAsync() // flushes and stops the worker
```

Choose a drop strategy when the queue is full: `DropNew`, `DropOldest`, or
`BlockWhenFull`. Batching lets the worker drain entries in chunks, while
`FlushInterval` guarantees partially filled batches still reach the sink. Query
`logger.AsyncStats()` to watch queue length and drops, and adjust behavior at
runtime with `SetDropStrategy`.

### Bridging to slog

Use the `bridge/slogbridge` package to route `slog` output into `simple-logger`:

```go
handler := slogbridge.NewHandler(logger, nil) // nil: follow the logger's level
logger := slog.New(handler)
logger.Info("hello", slog.String("user", "alice"))
```

Structured attributes, groups, and context metadata are preserved. Pass `nil` as
the level to track the logger's own level, so `SetLevel` (or the HTTP level
endpoint) reaches slog callers too; pass an explicit `slog.Leveler` to gate slog
independently of the logger.

### OTLP Export Hook

Forward log entries to an OTLP collector by attaching the `otlp` hook:

```go
// exp is any value implementing the otlp.Exporter interface
// (Export + Shutdown); see bridge/otlp/hook_test.go for a minimal example.
hook := otlp.NewHook(exp,
    otlp.WithServiceName("checkout"),
    otlp.WithErrorHandler(func(err error) {
        logger.Error("otlp export failed", log.Error("error", err))
    }),
)
logger.AddHook(hook)
```

The hook converts log entries into OTLP `ResourceLogs`; you can adapt any exporter
implementing the simple `otlp.Exporter` interface. Export failures are counted
(`hook.Stats().Failed`) and, when `WithErrorHandler` is supplied, reported to your
callback instead of being silently dropped.

Records are queued and exported in batches by a background goroutine, so a log
call never waits on the collector, and each export carries a deadline. **Close
the hook before the process exits**, or records still queued are lost:

```go
defer hook.Close(context.Background()) // exports what is pending, then shuts down
hook.Flush(ctx)                        // or force delivery at a chosen point
```

Tune with `WithBatchSize`, `WithFlushInterval`, `WithQueueSize`, and
`WithExportTimeout`. When the queue is full, records are dropped and counted in
`hook.Stats().Dropped` rather than blocking the application. `WithSynchronousExport`
exports inline instead, coupling log calls to collector latency.

### File Rotation

Built-in rotation mirrors `lumberjack.Logger` options:

```go
cfg := log.DefaultConfig()
cfg.Output = "app.log"
cfg.Rotation.Enable = true
cfg.Rotation.MaxSize = 50   // MB
cfg.Rotation.MaxAge = 14    // days
cfg.Rotation.MaxBackups = 5
cfg.Rotation.Compress = true

logger := log.ApplyConfig(cfg)
```

You can also manage rotation manually by constructing your own `*lumberjack.Logger`
and passing it to `SetOutputWithCloser`.

## Managing Logger Lifecycle

Loggers may own resources such as open files. When you create a logger via `ApplyConfig`—or use `SetOutputWithCloser`—always close it when you are finished:

```go
logger := log.ApplyConfig(log.DefaultConfig())
defer logger.Close()
```

To hand the logger a resource to manage explicitly, supply a closer:

```go
file, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
if err != nil {
	logger.Fatal(err)
}
logger.SetOutputWithCloser(file, file)
```

`SetOutput` remains available for writers that do not require cleanup.

## Tuning Synchronization

By default, `Logger` serializes writes to guarantee line integrity even when the underlying writer is not concurrency-safe. When you control the destination and know it can handle concurrent access (for example, a sharded writer or a buffered channel), you can disable synchronization to squeeze out a bit more throughput:

```go
logger.SetSynchronized(false)
```

The same setting is exposed in `LoggerConfig` as `SyncWrites` and via the environment variable `LOG_SYNC_WRITES`. Remember that disabling synchronization shifts the responsibility for atomic writes to your writer implementation.

> **Note:** When synchronization is disabled, writers replaced via `SetOutputWithCloser` (including live config reloads) are not closed immediately, because another goroutine may still hold the previous writer. They are closed when you call `logger.Close()`. Keep `SyncWrites` enabled (the default) if you swap outputs frequently while logging and want each replaced writer closed eagerly.

### Allocation-Free Helpers

If you already have preformatted strings or a single additional value, use the `*String`/`*1` helpers to avoid the slice allocation that variadic calls introduce:

```go
logger.InfoString("application started")
logger.Info1("user-count", userCount)
```

The classic variadic methods still work for convenience; mix and match based on your hot paths.

## Comparison

| Library          | Caller Info (default) | Write Sync (default) | JSON Support | Notable Focus               |
|------------------|------------------------|----------------------|--------------|-----------------------------|
| simple-logger    | off                    | on                   | built-in     | Lightweight, speed-first    |
| uber-go/zap      | off                    | on                   | built-in     | High-performance structure  |
| sirupsen/logrus  | on                     | on                   | via hook     | Feature-rich, pluggable     |
| rs/zerolog       | off                    | off                  | native       | Zero-allocation structured  |
| go.uber.org/zap* | off                    | on                   | built-in     | Structured logging (sugared)|

`simple-logger` aims to bridge the gap between extremely fast structured loggers like `zerolog` and drop-in libraries such as `logrus`, keeping a minimal API while offering caller info and write serialization that can be toggled off when absolute throughput matters.

## Benchmarks

All numbers below were gathered with `go test -bench Benchmark -benchmem` on
macOS (Apple M4 Pro, ARM64) using Go 1.24. The benchmark suite lives in
`benchmark_compare_test.go` so you can reproduce locally.

```bash
$ go test -bench Benchmark -benchmem
BenchmarkSimpleLogger-14           8703645       134.4 ns/op     200 B/op       5 allocs/op
BenchmarkSimpleLoggerNoSync-14     9231789       131.5 ns/op     200 B/op       5 allocs/op
BenchmarkZapSugar-14               6581017       185.1 ns/op      32 B/op       2 allocs/op
BenchmarkLogrus-14                 1958559       600.5 ns/op     488 B/op      16 allocs/op
BenchmarkZerolog-14               25791891        45.66 ns/op      0 B/op       0 allocs/op
BenchmarkLoggerDefault-14          1480904       813.6 ns/op     199 B/op       5 allocs/op
BenchmarkLoggerNoCaller-14         9050629       134.5 ns/op     200 B/op       5 allocs/op
BenchmarkFmtSprintf-14            27270996        44.97 ns/op      39 B/op       2 allocs/op
```

### Takeaways

- With caller lookup disabled (the default) `simple-logger` remains faster than
  the sugared `zap` logger while keeping a simpler API.
- Disabling `SyncWrites` still has little effect when writing to `io.Discard`; the
  critical section is short. The toggle is useful only when the destination writer
  itself is the bottleneck.
- `zerolog` remains the zero-allocation champion for structured logging; use it
  when absolute minimum overhead matters and you are comfortable with its API.
- `logrus` trades speed for flexibility. Compared to it, `simple-logger` offers
  a ~5× throughput advantage while preserving a familiar API.
- Caller lookup has been optimized but still costs ~5× more than the default path.
  Enable it only when file/line metadata is required.

Even without caller information, the logger performs more work than
`fmt.Sprintf` because it writes to an `io.Writer` and formats timestamps. For
absolute maximum throughput, make sure `EnableCaller` remains `false` and only
set `SyncWrites` to `false` when the underlying writer safely handles concurrent
access.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! If you have ideas, suggestions, or bug fixes, please open an issue or submit a pull request.

## Contact

For any questions or issues, please reach out via GitHub.

---

Happy logging!

## Known Limitations

- **Resource management:** `ApplyConfig` and `SetOutputWithCloser` can own file handles; remember to call `logger.Close()` when you are done to release resources promptly.
- **Caller information overhead:** Enabling caller reporting requires walking the call stack, which adds latency to every log call. The default configuration leaves caller reporting disabled to avoid this cost.
- **JSON caller resolution:** `JSONFormatter` now looks up caller information in line with the text formatter, but costs remain higher when caller tracking is enabled.
- **Caller reporting in a custom formatter:** in async mode the entry is rendered on the worker goroutine, whose stack says nothing about who logged. The logger resolves the call site up front and hands it to formatters implementing `CallerAwareFormatter` (both built-in formatters do). A custom formatter that calls the runtime itself will report the worker instead.

### Environment Variables

| Variable            | Description                                         |
|---------------------|-----------------------------------------------------|
| `LOG_LEVEL`         | Overrides the log level (`DEBUG`..`FATAL`).         |
| `LOG_OUTPUT`        | `stdout`, `stderr`, or a file path.                 |
| `LOG_FORMAT`        | `text`, `json`, `console`, or `custom`.             |
| `LOG_ENABLE_CALLER` | `true`/`false` to include caller information.       |
| `LOG_SYNC_WRITES`   | `true`/`false` to control write serialization.      |
| `LOG_COLORIZE`      | `true`/`false` to colorize text formatter output.   |
| `LOG_TIME_FORMAT`   | Go time layout applied to timestamps (e.g. `2006-01-02T15:04:05Z07:00`). |
| `LOG_INCLUDE_STACKTRACE` | `true`/`false` to append stacktraces on error/fatal logs. |
| `LOG_ROTATE`       | `true`/`false` to enable built-in file rotation.         |
| `LOG_ROTATE_MAX_SIZE` | Max file size in MB before rotation (default 100).    |
| `LOG_ROTATE_MAX_AGE`  | Max age in days before old files are removed (default 30). |
| `LOG_ROTATE_MAX_BACKUPS` | Number of old files to keep (default 7).          |
| `LOG_ROTATE_COMPRESS` | `true`/`false` to gzip rotated logs (default true).   |

### Runtime Reconfiguration

Use `ConfigureLogger` to hot-swap formatter, level, and output without rebuilding loggers:

```go
logger := log.NewLogger(os.Stdout, log.INFO, &log.DefaultFormatter{})

cfg := log.LoggerConfig{
    Level:    log.DEBUG,
    Format:   "json",
    Output:   "stdout",
    SyncWrites: true,
}

if _, err := log.ConfigureLogger(logger, cfg); err != nil {
    log.NewLogger(os.Stderr, log.ERROR, &log.DefaultFormatter{}).Error("reconfigure failed", err)
}
```

Calling `ConfigureLogger(nil, cfg)` is equivalent to `ApplyConfig(cfg)` and returns a brand new logger.
