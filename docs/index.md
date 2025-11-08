---
layout: default
title: Simple Logger
---

# Simple Logger

Simple Logger is a lightweight logging library for Go that supports multiple log levels, customizable formats, and dynamic configuration.

## Features

- **Multiple Log Levels**: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.
- **Configurable Outputs**: direct logs to `stdout`, `stderr`, or a file.
- **Flexible Formatting**: choose between plain text, JSON, or provide your own custom formatter.
- **Runtime Configuration**: adjust log levels and formats without restarting your application.
- **Simple API**: quickly integrate logging into any Go project.
- **Thread-Safe Logging**: concurrent writers are serialized to keep log entries intact.
- **Secure File Output**: logs written via configuration use `0600` permissions by default.
- **Structured Fields**: attach key/value pairs via helpers like `String`, `Int`, `Bool`, `Any`, and more.
- **Context Awareness**: enrich logs with metadata pulled directly from `context.Context`.
- **Sampling Controls**: throttle noisy log paths with pluggable samplers.
- **Hooks & Multi-Sinks**: forward entries to additional outputs or custom listeners.
- **Async Mode**: move formatting off the hot path with buffered workers.
- **Rotation Support**: integrate with `lumberjack` for size/age based rotation.

## Advantages

- Minimal setup with sensible defaults.
- Tunable performance: disable caller lookup when you need speed.
- MIT licensed and open for contributions.
- Works anywhere Go runs.

## Getting Started

Install the package:

```bash
go get github.com/pod32g/simple-logger
```

See the [README](../README.md) for detailed usage examples and benchmark results.

---
Happy logging!

## Environment Variables

- `LOG_LEVEL`
- `LOG_OUTPUT`
- `LOG_FORMAT`
- `LOG_ENABLE_CALLER`
- `LOG_SYNC_WRITES`
- `LOG_COLORIZE`
- `LOG_TIME_FORMAT`
- `LOG_INCLUDE_STACKTRACE`
- `LOG_ROTATE`
- `LOG_ROTATE_MAX_SIZE`
- `LOG_ROTATE_MAX_AGE`
- `LOG_ROTATE_MAX_BACKUPS`
- `LOG_ROTATE_COMPRESS`

## Runtime Reconfiguration

Use `ConfigureLogger` to swap outputs, formats, or levels at runtime without
rebuilding loggers.

## Known Limitations

- **Resource management:** `ApplyConfig` may open files on your behalf. Always call `logger.Close()` when you are finished with a logger so those descriptors are released promptly.
- **Caller information overhead:** Stack inspection for caller data adds latency. Caller reporting is disabled by default; enable it only when the extra context is essential.
- **JSON caller resolution:** JSON and text formatters now agree on caller resolution, but enabling caller metadata still incurs additional overhead compared with leaving it off.
