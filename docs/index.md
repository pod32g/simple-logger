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
