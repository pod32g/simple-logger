# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]
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
