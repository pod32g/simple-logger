
# Simple Logger

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
- Hot-path helpers (`InfoString`, `Info1`, etc.) to skip variadic allocations when you already have formatted values.

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
		log.Fatal("Failed to open log file")
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
	log.Fatal(err)
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
macOS (Apple M4 Pro, ARM64) using Go 1.22.3. The benchmark suite lives in
`benchmark_compare_test.go` so you can reproduce locally.

```bash
$ go test -bench Benchmark -benchmem
BenchmarkSimpleLogger-14           8764585       120.9 ns/op     200 B/op       5 allocs/op
BenchmarkSimpleLoggerNoSync-14     9946131       120.9 ns/op     200 B/op       5 allocs/op
BenchmarkZapSugar-14               6779299       177.5 ns/op      32 B/op       2 allocs/op
BenchmarkLogrus-14                 1963604       608.5 ns/op     488 B/op      16 allocs/op
BenchmarkZerolog-14               25052126        47.0 ns/op       0 B/op       0 allocs/op
BenchmarkLoggerDefault-14           968043      1247 ns/op     535 B/op       7 allocs/op
BenchmarkLoggerNoCaller-14         9881538       122.6 ns/op     200 B/op       5 allocs/op
BenchmarkFmtSprintf-14            26704794        45.9 ns/op      39 B/op       2 allocs/op
```

### Takeaways

- With caller lookup disabled (the default) `simple-logger` now beats the sugared
  `zap` logger on this workload while keeping a simpler API.
- Disabling `SyncWrites` still has little effect when writing to `io.Discard`; the
  critical section is short. The toggle is useful only when the destination writer
  itself is the bottleneck.
- `zerolog` remains the zero-allocation champion for structured logging; use it
  when absolute minimum overhead matters and you are comfortable with its API.
- `logrus` trades speed for flexibility. Compared to it, `simple-logger` offers
  a 4–5× throughput advantage while preserving a familiar API.
- Keeping caller lookup enabled (`LoggerDefault`) multiplies the cost by roughly
  10× due to stack inspection. Only enable it when you really need file/line
  metadata.

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

- **Resource management:** `Logger.ApplyConfig` and `SetOutputWithCloser` can own file handles; remember to call `logger.Close()` when you are done to release resources promptly.
- **Caller information overhead:** Enabling caller reporting requires walking the call stack, which adds latency to every log call. The default configuration leaves caller reporting disabled to avoid this cost.
- **JSON caller resolution:** `JSONFormatter` now looks up caller information in line with the text formatter, but costs remain higher when caller tracking is enabled.
