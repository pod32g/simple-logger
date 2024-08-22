package main

import (
	"fmt"
	"os"

	log "github.com/pod32g/simple-logger"
)

func main() {
	// Example 1: Basic Logging with Default Configuration
	// ---------------------------------------------------
	// This example uses the default configuration, which logs messages in plain text format
	// to stdout with an INFO log level.
	config := log.DefaultConfig()
	logger := log.ApplyConfig(config)

	// Log some messages using the default configuration
	logger.Info("Example 1: This is an info message")
	logger.Debug("Example 1: This debug message will not be shown because the level is set to INFO by default")
	logger.Warn("Example 1: This is a warning message")
	logger.Error("Example 1: This is an error message")

	// Example 2: Configuring the Logger Using Environment Variables
	// -------------------------------------------------------------
	// This example shows how to configure the logger using environment variables
	// to change the log level and format.
	// Set environment variables
	os.Setenv("LOG_LEVEL", "DEBUG")
	os.Setenv("LOG_FORMAT", "json")

	// Load configuration from environment variables
	config = log.LoadConfigFromEnv()
	logger = log.ApplyConfig(config)

	// Log some messages with environment variable configuration
	logger.Info("Example 2: This is an info message")
	logger.Debug("Example 2: This is a debug message, now visible because the log level is set to DEBUG")
	logger.Warn("Example 2: This is a warning message in JSON format")
	logger.Error("Example 2: This is an error message in JSON format")

	// Example 3: Logging to a File with JSON Format
	// ---------------------------------------------
	// This example demonstrates how to log messages to a file in JSON format.
	// Custom configuration to log to a file in JSON format
	config = log.LoggerConfig{
		Level:        log.DEBUG,
		Output:       "mylogfile.json", // Log to a file
		Format:       "json",           // Use JSON format
		EnableCaller: true,             // Include caller information
	}
	logger = log.ApplyConfig(config)

	// Log some messages with file output configuration
	logger.Info("Example 3: This is an info message")
	logger.Debug("Example 3: This is a debug message")
	logger.Warn("Example 3: This is a warning message")
	logger.Error("Example 3: This is an error message")

	// Example 4: Dynamic Log Level Update at Runtime
	// ----------------------------------------------
	// This example shows how to change the log level dynamically at runtime.
	// Load the default configuration
	config = log.DefaultConfig()
	logger = log.ApplyConfig(config)

	// Log at default level (INFO)
	logger.Info("Example 4: This is an info message")
	logger.Debug("Example 4: This debug message will not be shown because the level is set to INFO by default")

	// Dynamically update the log level to DEBUG
	config.UpdateLogLevel(log.DEBUG)
	logger = log.ApplyConfig(config) // Reapply the config after changing the level

	// Log again at the updated level
	logger.Info("Example 4: This is another info message")
	logger.Debug("Example 4: Now this debug message will be shown")

	// Example 5: Loading Configuration from a JSON File
	// -------------------------------------------------
	// This example shows how to load the logger configuration from a JSON file.
	// Assume we have a config.json file with the following content:
	// {
	//   "level": "DEBUG",
	//   "output": "stdout",
	//   "format": "json",
	//   "enable_caller": true
	// }
	config, err := log.LoadConfigFromFile("config.json")
	if err != nil {
		logger.Error("Failed to load config: ", err)
	}

	logger = log.ApplyConfig(config)

	// Log some messages with JSON file configuration
	logger.Info("Example 5: This is an info message loaded from JSON config")
	logger.Debug("Example 5: This is a debug message loaded from JSON config")
	logger.Warn("Example 5: This is a warning message loaded from JSON config")
	logger.Error("Example 5: This is an error message loaded from JSON config")

	// Example 6: Logging in JSON Format to stdout
	// -------------------------------------------
	// This example demonstrates how to configure the logger to output JSON-formatted
	// logs directly to stdout. This setup is useful for environments where logs are
	// streamed to a centralized logging system or consumed by tools like `docker logs`.

	// Custom configuration to log to stdout in JSON format
	config = log.LoggerConfig{
		Level:        log.DEBUG, // Set log level to DEBUG to capture all logs
		Output:       "stdout",  // Log to stdout
		Format:       "json",    // Use JSON format for log messages
		EnableCaller: true,      // Include caller information in logs
	}
	logger = log.ApplyConfig(config)

	// Log some messages
	logger.Info("This is an info message")
	logger.Debug("This is a debug message")
	logger.Warn("This is a warning message")
	logger.Error("This is an error message")

	// Example 7: Using a Custom Formatter
	// -----------------------------------
	// This example shows how to use a custom formatter for logging.
	config = log.DefaultConfig()
	config.Format = "custom"             // Use custom format
	config.Custom = &MyCustomFormatter{} // Provide the custom formatter

	logger = log.ApplyConfig(config)

	// Log some messages with the custom formatter
	logger.Info("Example 7: This is a custom formatted info message")
	logger.Debug("Example 7: This is a custom formatted debug message")
}

// MyCustomFormatter is a sample custom formatter for demonstration
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
