package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// CustomFormatter is an interface that users can implement to provide custom log formatting
type CustomFormatter interface {
	Format(level LogLevel, message string) string
}

// LoggerConfig holds all configurable settings for the logger
type LoggerConfig struct {
	Level             LogLevel        `json:"level"`
	Output            string          `json:"output"` // Can be "stdout", "stderr", or a filepath
	Format            string          `json:"format"` // Can be "text", "json", or "custom"
	Filepath          string          `json:"filepath"`
	EnableCaller      bool            `json:"enable_caller"`
	SyncWrites        bool            `json:"sync_writes"`
	Colorize          bool            `json:"colorize"`
	TimeFormat        string          `json:"time_format"`
	IncludeStacktrace bool            `json:"include_stacktrace"`
	Rotation          RotationConfig  `json:"rotation"`
	Custom            CustomFormatter `json:"-"` // Custom formatter provided by the user
}

type RotationConfig struct {
	Enable     bool `json:"enable"`
	MaxSize    int  `json:"max_size_mb"`
	MaxAge     int  `json:"max_age_days"`
	MaxBackups int  `json:"max_backups"`
	Compress   bool `json:"compress"`
}

// DefaultConfig returns a LoggerConfig with default values
func DefaultConfig() LoggerConfig {
	return LoggerConfig{
		Level:             INFO,
		Output:            "stdout",
		Format:            "text",
		Filepath:          "",
		EnableCaller:      false,
		SyncWrites:        true,
		Colorize:          false,
		TimeFormat:        "",
		IncludeStacktrace: false,
		Rotation: RotationConfig{
			Enable:     false,
			MaxSize:    100,
			MaxAge:     30,
			MaxBackups: 7,
			Compress:   true,
		},
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

	// Colorize
	if colorize := os.Getenv("LOG_COLORIZE"); colorize != "" {
		if parsed, err := strconv.ParseBool(colorize); err == nil {
			config.Colorize = parsed
		}
	}

	if timeFormat := os.Getenv("LOG_TIME_FORMAT"); timeFormat != "" {
		config.TimeFormat = timeFormat
	}
	if includeStack := os.Getenv("LOG_INCLUDE_STACKTRACE"); includeStack != "" {
		if parsed, err := strconv.ParseBool(includeStack); err == nil {
			config.IncludeStacktrace = parsed
		}
	}

	if rotate := os.Getenv("LOG_ROTATE"); rotate != "" {
		if parsed, err := strconv.ParseBool(rotate); err == nil {
			config.Rotation.Enable = parsed
		}
	}
	if maxSize := os.Getenv("LOG_ROTATE_MAX_SIZE"); maxSize != "" {
		if parsed, err := strconv.Atoi(maxSize); err == nil {
			config.Rotation.MaxSize = parsed
		}
	}
	if maxAge := os.Getenv("LOG_ROTATE_MAX_AGE"); maxAge != "" {
		if parsed, err := strconv.Atoi(maxAge); err == nil {
			config.Rotation.MaxAge = parsed
		}
	}
	if backups := os.Getenv("LOG_ROTATE_MAX_BACKUPS"); backups != "" {
		if parsed, err := strconv.Atoi(backups); err == nil {
			config.Rotation.MaxBackups = parsed
		}
	}
	if compress := os.Getenv("LOG_ROTATE_COMPRESS"); compress != "" {
		if parsed, err := strconv.ParseBool(compress); err == nil {
			config.Rotation.Compress = parsed
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
	logger, err := ConfigureLogger(nil, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply logger config: %v\n", err)
		fallback := NewLogger(os.Stdout, config.Level, &DefaultFormatter{
			IncludeCaller: config.EnableCaller,
			Colorize:      config.Colorize,
			TimeLayout:    config.TimeFormat,
		})
		fallback.SetIncludeStacktrace(config.IncludeStacktrace)
		fallback.SetSynchronized(config.SyncWrites)
		return fallback
	}
	return logger
}

// ConfigureLogger applies the provided configuration to an existing logger or creates a new one.
func ConfigureLogger(logger *Logger, config LoggerConfig) (*Logger, error) {
	var output io.Writer = os.Stdout
	var closer io.Closer
	if config.Output == "stderr" {
		output = os.Stderr
	} else if config.Output != "stdout" {
		if config.Rotation.Enable {
			lj := &lumberjack.Logger{
				Filename:   config.Output,
				MaxSize:    config.Rotation.MaxSize,
				MaxAge:     config.Rotation.MaxAge,
				MaxBackups: config.Rotation.MaxBackups,
				Compress:   config.Rotation.Compress,
			}
			output = lj
			closer = lj
		} else {
			file, err := os.OpenFile(config.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return nil, fmt.Errorf("open log file: %w", err)
			}
			output = file
			closer = file
		}
	}

	formatter, err := formatterForConfig(config)
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = NewLogger(output, config.Level, formatter)
	} else {
		logger.SetLevel(config.Level)
		logger.SetFormatter(formatter)
	}
	if closer != nil {
		logger.SetOutputWithCloser(output, closer)
	} else {
		logger.SetOutput(output)
	}
	logger.SetSynchronized(config.SyncWrites)
	logger.SetIncludeStacktrace(config.IncludeStacktrace)
	if df, ok := formatter.(*DefaultFormatter); ok {
		df.TimeLayout = config.TimeFormat
	}
	if jf, ok := formatter.(*JSONFormatter); ok {
		jf.TimeLayout = config.TimeFormat
	}
	return logger, nil
}

func formatterForConfig(config LoggerConfig) (Formatter, error) {
	switch strings.ToLower(config.Format) {
	case "json":
		return &JSONFormatter{IncludeCaller: config.EnableCaller, TimeLayout: config.TimeFormat}, nil
	case "custom":
		if config.Custom == nil {
			return nil, fmt.Errorf("custom formatter requested but Custom field is nil")
		}
		return config.Custom, nil
	default:
		return &DefaultFormatter{IncludeCaller: config.EnableCaller, Colorize: config.Colorize, TimeLayout: config.TimeFormat}, nil
	}
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
