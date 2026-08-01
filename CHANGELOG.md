# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

A release about the public API. Every capability from 0.7.1 is still here; the
surface reaching it is much smaller, and several long-standing gaps are closed.

### Changed — the API

- **One constructor.** `New(opts ...Option) (*Logger, error)`, with `Must` for
  package-level use, replaces `NewLogger`, `ApplyConfig` and `ConfigureLogger`.
  An invalid configuration is now an error rather than a silent fallback to a
  logger you did not ask for. `Development()` and `Production()` are presets.
- **Three ways to log per level, not six.** `Info(msg, fields...)`,
  `Infof(format, args...)` and `InfoContext(ctx, msg, fields...)`. The `*String`
  and `*1` variants existed only to avoid allocations, which made an
  implementation detail into the caller's first decision; `Info("msg")` now
  costs what `InfoString` cost.
- **Options instead of setters.** Fifteen `Set*` mutators become options.
  `SetLevel`, `SetOutput`, `SetOutputWithCloser` and `AddHook` remain, because
  changing those at runtime is a real use.
- **One encoder interface.** `Encoder` with a single `Encode(buf, Entry) []byte`
  replaces eight formatter interfaces whose real contract was an undocumented
  chain of type assertions. `DefaultFormatter`, `JSONFormatter` and
  `ConsoleFormatter` become `TextEncoder`, `JSONEncoder` and `ConsoleEncoder`.
- **Config.** A typed `Format`, no more `Output`/`Filepath` overlap, no more
  stringly-typed `"custom"` discriminator (supply an `Encoder`), and
  `SyncWrites`/`Compress` inverted to `Unsynchronized`/`NoCompress` so defaults
  are the zero value. Numeric levels are rejected in favour of names.
- **Reload.** Six package symbols become `logger.Watch` and `logger.ReloadFrom`.
- **Async mechanics are internal**, reached through `WithAsync*` options.
- `Error(key, err)` becomes `Err(key, err)`, freeing `log.Error` for the
  package-level logging functions every comparable library has.
- Removed `FieldEncoder` (referenced nowhere), `CustomFormatter` (identical to
  `Formatter`) and `StructuredArgsFormatter` (declared, documented, and never
  dispatched — implementing it did nothing).

### Added

- `TRACE` and `PANIC` levels, threaded through parsing, colors, and the slog
  and OTLP mappings.
- `Group` for nested fields and `Lazy` for values computed only when the entry
  survives.
- Field constructors: `Duration`, `Time`, `Stringer`, `Binary`, `Uint64`,
  `Int32`, `Uint32`, `Float32`, plus `Logger.WithError`.
- `WithCallerSkip`, so a wrapper reports its callers rather than itself.
- `Logger.Writer` and `WriterLevel`, turning line-oriented output from other
  libraries into entries — including `http.Server.ErrorLog`.
- Package `logtest`: an in-memory observer with chainable filters, so tests
  assert on entries instead of parsing output.
- Module `bridge/grpclog`: unary and stream interceptors, previously only
  example code. It is a separate module, so gRPC stays out of your graph.
- `Logger.Log` with `EntryTime` and `EntryCaller`, for bridges carrying a
  record's own timestamp and call site.

### Fixed

- **The slog bridge now passes `testing/slogtest`.** It previously failed 28
  checks: groups were flattened into dotted keys and duplicated across repeated
  `WithGroup`/`WithAttrs` pairs, `Record.Time` and `Record.PC` were discarded,
  empty attributes were emitted, and empty groups were not omitted. The suite
  now runs as part of the bridge's tests. One documented deviation remains: this
  logger always stamps a time.

### Performance

Allocations on the logging path fall from 7 per entry to 2, and time from
185ns to 119ns, measured on one machine across the same benchmark. The library
now sits between zerolog and zap:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| zerolog | 46.8 | 0 | 0 |
| simple-logger | 118.6 | 40 | 2 |
| zap | 163.6 | 64 | 1 |
| logrus | 914.3 | 1258 | 22 |

The win comes from the encoder contract: the logger owns one pooled buffer per
entry and the encoder appends into it, which removed the per-entry writer
wrapper, the closures, the `io.WriteString` conversions and the escaping frame
buffer.


## [0.7.1] - 2026-07-31

This release is mostly corrective: thirteen defects found in a review of 0.7.0,
plus the dependency and toolchain work that followed. It also carries a handful
of API additions and several behavior changes, so read *Changed* before
upgrading — in particular the OTLP hook now has to be closed.

### Added
- `Debugf`/`Infof`/`Warnf`/`Errorf`/`Fatalf` printf-style methods. They check the
  level before formatting, so a disabled `Debugf` costs nothing — unlike the
  `Info(fmt.Sprintf(...))` workaround they replace. `go vet` checks their format
  strings at call sites.
- `CallerAwareFormatter`, an optional formatter interface that receives a call
  site resolved by the logger rather than walking its own stack.
- OTLP hook: `Flush`, plus `WithBatchSize`, `WithFlushInterval`, `WithQueueSize`,
  `WithExportTimeout` and `WithSynchronousExport`. `HookStats` gains `Dropped`
  and `Queued`.
- `LogLevel` implements `json.Marshaler`/`json.Unmarshaler`, so JSON configs can
  name the level (`{"level":"debug"}`) as every other entry point already did.

### Changed
- **The minimum Go version is now 1.25**, required by `go.opentelemetry.io/otel`,
  which moved to `go 1.25.0` at v1.42.0 and is needed at 1.44.0 for a security fix.
- **`google.golang.org/grpc` is no longer a dependency of this module.** It was
  only ever imported by `example/grpc_interceptor`, which now has its own
  `go.mod`, so importing simple-logger no longer pulls grpc (and `golang.org/x/net`,
  `golang.org/x/text` and `genproto` with it) into your module graph.
- **The OTLP hook batches by default.** Records are queued and exported in
  batches (512 records or 1s) with a 10s per-export deadline, instead of one
  synchronous, unbounded gRPC round trip per entry on the logging goroutine.
  Close (or flush) the hook before exit, or use `WithSynchronousExport` to keep
  the previous behavior.
- Text output quotes values containing control characters, so an automatic
  stacktrace field now renders as a single quoted line.
- Unsynchronized writes take a read lock. They still proceed concurrently, but a
  writer swap or `Close` now waits for them instead of closing underneath them.

### Fixed
- `Flush()` could block forever: under `DropOldest` a producer freeing a queue
  slot could discard the flush barrier, which nothing then closed. Barriers are
  now preserved and re-queued behind the dropped entry.
- `Fatal` exited without draining the async queue, discarding the entries that
  explained why the process was dying. It now flushes first.
- Caller and stacktrace capture ran on the async worker, so `IncludeCaller`
  reported an unrelated location and automatic stacktraces showed the worker's
  stack. Both are now captured on the goroutine that logged.
- Caller resolution read file and line straight off the PC, which reports the
  wrong function once the compiler inlines — `IncludeCaller` was inaccurate in
  synchronous mode too. Frames are now expanded with `runtime.CallersFrames`.
- `JSONFormatter` could emit unparseable entries: field keys were written
  unescaped (a crafted key could also forge fields), and strings were quoted with
  `strconv.Quote`, which produces Go escapes such as `\xNN` and `\U0001d173` that
  JSON does not accept.
- Field and message truncation cut on byte boundaries, splitting UTF-8 runes and
  so manufacturing the invalid input above. The cut now backs off to a boundary.
- Text output did not escape control characters, so a newline in a message or a
  field value ended the record and forged a new one.
- `ConfigureLogger` opened the log file before building the formatter and leaked
  the descriptor when formatter construction failed — once per poll under a
  config watcher.
- `NewBurstSampler` never removed buckets, so high-cardinality messages grew its
  map without bound (~25 MiB per 200k distinct messages). Closed windows are now
  swept.
- Writers superseded by `SetOutputWithCloser` were held open until `Close` in
  unsynchronized mode; they are now closed when replaced.
- The slog bridge ignored the logger's level, so runtime level changes (including
  the `httplog` level endpoint) could never turn slog output back up. A nil
  `Leveler` now follows the logger.

### Security
- Updated dependencies to clear known vulnerabilities: `google.golang.org/grpc`
  to 1.82.1 (xDS RBAC and HTTP/2 issues, high), `golang.org/x/net` to 0.56.0
  (HTML parser DoS, plus a later advisory that affects 0.55.0), `golang.org/x/text`
  to 0.39.0 and `go.opentelemetry.io/otel` to 1.44.0. None were reachable from
  this module; `govulncheck` reports no vulnerabilities.

## [0.7.0] - 2026-06-21
### Added
- `Logger.With(fields ...Field) *Logger` — derived loggers that bind persistent
  fields (e.g. per-request `request_id`) and share the parent's output, level,
  hooks, and async state. The most-requested production logging affordance.
- `Logger.Named(name)` for dotted component sub-loggers (`logger=http.auth`).
- `Logger.Level()` and `Logger.Enabled(level)` to read the current level and gate
  expensive field construction on hot paths.
- `Default()`/`SetDefault()` process-wide logger for zero-config use.
- `ParseLevel(string)` and `LogLevel.String()` for level (de)serialization.
- Redaction: `SetRedactor`, `NewKeyRedactor` (key-based PII/secret masking), and
  `NewPatternScrubber` (regex secret scrubbing of fields and messages).
- `SetErrorHandler`/`WriteErrors()` so sink write failures are observable instead
  of silently dropped.
- `Flush()` (drain the async queue without teardown) and `Sync()` (flush + fsync
  the file sink) — enabling the `defer logger.Sync()` idiom.
- `Recover()`/`RecoverAndContinue()` panic-capture helpers for goroutine/handler
  boundaries.
- `AddLevelOutput(minLevel, w)` to mirror entries at/above a level to extra sinks
  (e.g. errors to an alerting sink).
- `ConsoleFormatter` — a colorized, human-friendly encoder for local development
  (also `Format: "console"`).
- `SetDeduplicateFields`, `SetMaxFieldBytes`, `SetMaxMessageBytes` for last-wins
  field de-dup and value/message truncation.
- `NewBurstSampler` (first-N-then-every-M) and `NewLevelSampler` (per-level
  sampling) in addition to `EveryNSampler`.
- Injectable clock (`Now func() time.Time`) on `DefaultFormatter`/`JSONFormatter`
  for deterministic test output.
- `ErrorVerbose(key, err)` to capture the wrapped error chain and stack via `%+v`.
- `WithRequestID`/`RequestID` correlation-ID helpers; new `bridge/httplog`
  subpackage with `LevelHandler` (runtime level control over HTTP) and
  `RequestIDMiddleware`.
- New `bridge/oteltrace` subpackage: a `ContextExtractor` that injects
  OpenTelemetry `trace_id`/`span_id`; the OTLP hook promotes those keys to
  first-class `LogRecord.TraceId`/`SpanId`.
- Continuous integration via GitHub Actions: cross-platform test matrix
  (Linux/macOS/Windows × the two most recent Go releases) with the race detector
  and coverage, plus dedicated lint, staticcheck, module-tidy, and fuzz-smoke jobs.
- CodeQL security scanning workflow.
- Tag-driven release workflow that publishes GitHub releases from `CHANGELOG.md`
  and warms the Go module proxy.
- Dependabot configuration for Go modules and GitHub Actions.
- `.golangci.yml` linter configuration and a `Makefile` of standard developer tasks.
- OTLP hook: `WithErrorHandler` option and `Stats()` so export failures are
  observable instead of silently dropped.
- `LoggerConfig.Filepath` is now honored as a file destination (taking precedence
  over `Output`) instead of being a no-op field.

### Fixed
- **Critical:** logging concurrently with `DisableAsync`/`Close`/`EnableAsync`
  no longer panics with "send on closed channel". The async producer/closer
  handoff is now coordinated so the queue channel is never closed while a
  producer may still send.
- **Critical:** `Fatal` now always terminates the process synchronously, even
  when async logging is enabled or an aggressive sampler is installed. It is no
  longer sampled away or deferred to the async worker.
- Data races on the async drop strategy (read on the logging hot path while
  `SetDropStrategy`/`EnableAsync` mutated it) are eliminated; the strategy is now
  stored atomically and the worker receives an immutable snapshot.
- Hook panics are isolated with `recover` so a misbehaving hook can no longer
  crash the caller or kill the async worker.
- `DropOldest` now counts every dropped entry, keeping `AsyncStats().Dropped`
  accurate under producer contention.
- Writers retired by `SetOutputWithCloser` in non-synchronized mode are no longer
  closed while another goroutine may still use them (use-after-close); they are
  closed on `Close()` instead.
- The OTLP `NewHook` tolerates a nil exporter without panicking on first log.
- File-permission assertion in tests is now skipped on Windows, where Unix mode
  bits are not meaningful, allowing the suite to run on all platforms.
- README examples no longer reference a non-existent package-level `log.Fatal`;
  they use `logger.Fatal`/`panic` as appropriate so the snippets compile.

### Security
- Bumped `go.opentelemetry.io/otel` to 1.41.0 to address a high-severity remote
  DoS amplification in multi-value `baggage` header extraction (affecting
  1.36.0–1.40.0), pulled in transitively by the new `bridge/oteltrace` package.

## [0.6.0] - 2025-11-09
### Added
- Expanded end-to-end suite covering async drop stats, hook filters, `slog` bridge, and OTLP exporter.
- Race detector, static analysis, and fuzzing guidance documented across README/docs.
- JSON formatter fuzz target for automated robustness testing.

### Changed
- gRPC example now uses `grpc.NewClient` with modern dialing semantics.
- Reload helpers and bridges documentation refreshed with latest workflows.

### Fixed
- Addressed race during JSON formatter reconfiguration under hot reload.
- Eliminated staticcheck warnings in slog bridge and OTLP hook.

## [0.5.0] - 2025-11-08
### Added
- Pluggable field encoder registry with built-in support for `time.Duration`, `time.Time`, and `error`.
- OTLP export hook and `slog` bridge packages for interoperability.
- Async batching controls (batch size, flush interval) and queue metrics helpers.
- Hot reload utilities for file-based and channel-driven configuration updates.
- New runnable examples covering HTTP middleware, gRPC interceptor, and interactive CLI.

### Changed
- Bridges and reload helpers documented across README and docs site.

## [0.3.0] - 2025-06-02
### Added
- `ArgsFormatter` interface for streaming log formatting.
- GitHub Pages documentation under `docs/`.
- Benchmark results showing performance impact of caller lookup.
- Additional unit tests covering configuration and formatters.

### Changed
- Logger internals optimized for reduced allocations and faster formatting.
- Default and JSON formatters now respect the `EnableCaller` option.
- README examples updated and expanded.

### Fixed
- JSON formatter now always appends a newline.
- Example code corrected to call `logger.Fatal`.

## [0.2.0] - 2024-08-21
See previous release notes for earlier changes.
