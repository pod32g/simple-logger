package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// CustomFormatter is an interface that users can implement to provide custom log formatting
type CustomFormatter interface {
	Format(level LogLevel, message string) string
}

// LoggerConfig holds all configurable settings for the logger
type LoggerConfig struct {
	Level        LogLevel        `json:"level"`
	Output       string          `json:"output"` // Can be "stdout", "stderr", or a filepath
	Format       string          `json:"format"` // Can be "text", "json", or "custom"
	Filepath     string          `json:"filepath"`
	EnableCaller bool            `json:"enable_caller"`
	SyncWrites   bool            `json:"sync_writes"`
	Custom       CustomFormatter `json:"-"` // Custom formatter provided by the user
}

// DefaultConfig returns a LoggerConfig with default values
func DefaultConfig() LoggerConfig {
	return LoggerConfig{
		Level:        INFO,
		Output:       "stdout",
		Format:       "text",
		Filepath:     "",
		EnableCaller: false,
		SyncWrites:   true,
	}
}

// LoadConfigFromEnv loads the logger configuration from environment variables
func LoadConfigFromEnv() LoggerConfig {
	config := DefaultConfig()

	// Log level
	level := os.Getenv("LOG_LEVEL")
	if level != "" {
		config.Level = parseLogLevel(level)
	}

	// Output destination
	output := os.Getenv("LOG_OUTPUT")
	if output != "" {
		config.Output = output
	}

	// Log format
	format := os.Getenv("LOG_FORMAT")
	if format != "" {
		config.Format = strings.ToLower(format)
	}

	// Enable caller
	if enableCaller := os.Getenv("LOG_ENABLE_CALLER"); enableCaller != "" {
		if parsed, err := strconv.ParseBool(enableCaller); err == nil {
			config.EnableCaller = parsed
		}
	}

	// Sync writes
	if syncWrites := os.Getenv("LOG_SYNC_WRITES"); syncWrites != "" {
		if parsed, err := strconv.ParseBool(syncWrites); err == nil {
			config.SyncWrites = parsed
		}
	}

	return config
}

// LoadConfigFromFile loads the logger configuration from a JSON file
func LoadConfigFromFile(filePath string) (LoggerConfig, error) {
	config := DefaultConfig()
	file, err := os.Open(filePath)
	if err != nil {
		return config, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return config, err
	}

	return config, nil
}

// UpdateLogLevel allows for dynamically updating the log level at runtime
func (config *LoggerConfig) UpdateLogLevel(level LogLevel) {
	config.Level = level
}

// UpdateLogFormat allows for dynamically updating the log format at runtime
func (config *LoggerConfig) UpdateLogFormat(format string) {
	config.Format = strings.ToLower(format)
}

// ApplyConfig applies the loaded configuration to the Logger
func ApplyConfig(config LoggerConfig) *Logger {
	var output io.Writer = os.Stdout
	var closer io.Closer
	if config.Output == "stderr" {
		output = os.Stderr
	} else if config.Output != "stdout" {
		file, err := os.OpenFile(config.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening log file: %v", err)
			output = os.Stdout
		} else {
			output = file
			closer = file
		}
	}

	// Select the appropriate formatter
	var formatter Formatter
	switch config.Format {
	case "json":
		formatter = &JSONFormatter{IncludeCaller: config.EnableCaller}
	case "custom":
		if config.Custom != nil {
			formatter = config.Custom
		} else {
			fmt.Fprintf(os.Stderr, "Error: Custom formatter is nil")
			formatter = &DefaultFormatter{IncludeCaller: config.EnableCaller}
		}
	default:
		formatter = &DefaultFormatter{IncludeCaller: config.EnableCaller}
	}

	// Create and return the logger
	logger := NewLogger(output, config.Level, formatter)
	if closer != nil {
		logger.SetOutputWithCloser(output, closer)
	}
	logger.SetSynchronized(config.SyncWrites)

	return logger
}

// parseLogLevel converts a string representation of a log level to the corresponding LogLevel
func parseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO // Default log level
	}
}
