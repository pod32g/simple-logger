package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoggerConfig holds all configurable settings for the logger
type LoggerConfig struct {
	Level        LogLevel `json:"level"`
	Output       string   `json:"output"` // Can be "stdout", "stderr", or a filepath
	Format       string   `json:"format"` // Can be "text" or "json"
	Filepath     string   `json:"filepath"`
	EnableCaller bool     `json:"enable_caller"`
}

// DefaultConfig returns a LoggerConfig with default values
func DefaultConfig() LoggerConfig {
	return LoggerConfig{
		Level:        INFO,
		Output:       "stdout",
		Format:       "text",
		Filepath:     "",
		EnableCaller: true,
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
	enableCaller := os.Getenv("LOG_ENABLE_CALLER")
	if enableCaller == "false" {
		config.EnableCaller = false
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

// ApplyConfig applies the loaded configuration to the Logger
func ApplyConfig(config LoggerConfig) *Logger {
	var output io.Writer = os.Stdout
	if config.Output == "stderr" {
		output = os.Stderr
	} else if config.Output != "stdout" {
		file, err := os.OpenFile(config.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening log file: %v", err)
			output = os.Stdout
		} else {
			output = file
		}
	}

	logger := NewLogger(output, config.Level)

	// Customize logger based on config
	if config.Format == "json" {
		logger.LogMessage = logger.JsonLogMessage
	}

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
