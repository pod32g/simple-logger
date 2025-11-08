# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

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
