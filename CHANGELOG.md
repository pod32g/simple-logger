# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [0.4.0] - 2025-11-08
### Added
- Pluggable field encoder registry with built-in support for `time.Duration`, `time.Time`, and `error`.
- OTLP export hook and `slog` bridge packages for interoperability.
- Async batching controls (batch size, flush interval) and queue metrics helpers.
- Hot reload utilities for file-based and channel-driven configuration updates.
- New runnable examples covering HTTP middleware, gRPC interceptor, and interactive CLI.
- End-to-end test suite covering reload paths, async drops, hook filters, slog bridge, and OTLP hook.
- Fuzz target for the JSON formatter to harden structured output handling.

### Changed
- gRPC example updated to use the modern `grpc.NewClient` API.
- Bridges and reload helpers documented across README and docs site.

### Fixed
- Eliminated race condition when reconfiguring JSON formatter during reloads.
- Static analysis warnings around slog bridge attr resolution and unused helpers.

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
