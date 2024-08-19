
# Simple Logger

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Description

**Simple Logger** is a lightweight, flexible logging library for Go (Golang) that supports multiple log levels and customizable output formats, including plain text and JSON. It is designed to be easy to integrate into your projects, with minimal configuration required.

## Features

- Supports multiple log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.
- Customizable output destinations (e.g., stdout, stderr, or files).
- Supports plain text and JSON log formats.
- Simple API for setting log levels and outputs.

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
	// Create a new logger instance
	logger := log.NewLogger(os.Stdout, log.INFO)

	// Log messages at different levels
	logger.Debug("This is a debug message")
	logger.Info("This is an info message")
	logger.Warn("This is a warning message")
	logger.Error("This is an error message")
	logger.Fatal("This is a fatal message") // This will log the message and exit the application
}
```

### Customizing Output

You can change the log output to a file or any other `io.Writer`:

```go
package main

import (
	log "github.com/pod32g/simple-logger"
	"os"
)

func main() {
	// Open a file for logging
	file, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal("Failed to open log file")
	}
	defer file.Close()

	// Create a new logger instance that writes to the file
	logger := log.NewLogger(file, log.INFO)

	logger.Info("Logging to a file now!")
}
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! If you have ideas, suggestions, or bug fixes, please open an issue or submit a pull request.

## Contact

For any questions or issues, please reach out via GitHub or contact the project maintainer.

---

Happy logging!
