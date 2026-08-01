package log

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Format names a built-in encoder. It is a type rather than a free string so
// that an unknown value is reported instead of silently defaulting.
type Format string

const (
	FormatText    Format = "text"
	FormatJSON    Format = "json"
	FormatConsole Format = "console"
)

// Config is the serialisable form of a logger's setup, for configuration files
// and environment variables. Building a logger in code is better served by New
// and its options; this exists for configuration that arrives as data.
//
// Every field has a working default, so the zero Config is usable.
type Config struct {
	Level  LogLevel `json:"level"`
	Output string   `json:"output"` // "stdout", "stderr", or a file path
	Format Format   `json:"format"` // text (default), json, or console

	EnableCaller      bool     `json:"enable_caller"`
	IncludeStacktrace bool     `json:"include_stacktrace"`
	Colorize          bool     `json:"colorize"`
	TimeFormat        string   `json:"time_format"`
	Unsynchronized    bool     `json:"unsynchronized"`
	Rotate            bool     `json:"rotate"`
	Rotation          Rotation `json:"rotation"`

	// Encoder overrides Format with an encoder supplied in code. It has no
	// serialised form, which is why an unknown Format is an error rather than a
	// silent fallback: a config file cannot name an encoder that exists only in
	// the program.
	Encoder Formatter `json:"-"`
}

// DefaultConfig returns the configuration used when nothing else is specified.
func DefaultConfig() Config {
	return Config{
		Level:  INFO,
		Output: "stdout",
		Format: FormatText,
		Rotation: Rotation{
			MaxSizeMB:  100,
			MaxAgeDays: 30,
			MaxBackups: 7,
		},
	}
}

// Options converts a Config into the options New takes, so that both paths
// build exactly the same logger.
func (c Config) Options() []Option {
	opts := []Option{WithLevel(c.Level)}

	if c.TimeFormat != "" {
		opts = append(opts, WithTimeFormat(c.TimeFormat))
	}
	if c.Colorize {
		opts = append(opts, WithColor())
	}

	switch {
	case c.Encoder != nil:
		opts = append(opts, WithEncoder(c.Encoder))
	case c.Format == FormatJSON:
		opts = append(opts, WithJSON())
	case c.Format == FormatConsole:
		opts = append(opts, WithConsole())
	case c.Format == FormatText || c.Format == "":
		// the default encoder
	default:
		format := c.Format
		opts = append(opts, func(*builder) error {
			return fmt.Errorf("unknown log format %q", format)
		})
	}

	switch c.Output {
	case "", "stdout":
		opts = append(opts, WithOutput(os.Stdout))
	case "stderr":
		opts = append(opts, WithOutput(os.Stderr))
	default:
		if c.Rotate {
			opts = append(opts, WithRotatingFile(c.Output, c.Rotation))
		} else {
			opts = append(opts, WithFile(c.Output))
		}
	}

	if c.EnableCaller {
		opts = append(opts, WithCaller())
	}
	if c.IncludeStacktrace {
		opts = append(opts, WithStacktrace())
	}
	if c.Unsynchronized {
		opts = append(opts, WithUnsynchronized())
	}
	return opts
}

// FromConfig builds a logger from a Config.
func FromConfig(cfg Config) (*Logger, error) {
	return New(cfg.Options()...)
}

// Apply reconfigures an existing logger in place, which is what the reload
// helpers use. Level, encoder, output and stacktrace settings are replaced;
// hooks and samplers registered in code are left alone.
func (l *Logger) Apply(cfg Config) error {
	probe, err := New(cfg.Options()...)
	if err != nil {
		return err
	}

	l.SetLevel(cfg.Level)
	if fh := probe.formatter.Load(); fh != nil {
		l.setFormatter(fh.f)
	}
	l.setIncludeStacktrace(cfg.IncludeStacktrace)
	l.setSynchronized(!cfg.Unsynchronized)
	if wh := probe.output.Load(); wh != nil {
		l.SetOutputWithCloser(wh.w, probe.closer)
	}
	return nil
}

// LoadConfigFromEnv reads configuration from the LOG_* environment variables.
func LoadConfigFromEnv() Config {
	config := DefaultConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Level = parseLogLevel(level)
	}
	if output := os.Getenv("LOG_OUTPUT"); output != "" {
		config.Output = output
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Format = Format(strings.ToLower(format))
	}
	envBool("LOG_ENABLE_CALLER", &config.EnableCaller)
	envBool("LOG_INCLUDE_STACKTRACE", &config.IncludeStacktrace)
	envBool("LOG_COLORIZE", &config.Colorize)
	envBool("LOG_ROTATE", &config.Rotate)
	if timeFormat := os.Getenv("LOG_TIME_FORMAT"); timeFormat != "" {
		config.TimeFormat = timeFormat
	}
	// LOG_SYNC_WRITES reads the other way round, so that the default
	// (synchronised) stays the zero value of the field.
	if sync := os.Getenv("LOG_SYNC_WRITES"); sync != "" {
		if parsed, err := strconv.ParseBool(sync); err == nil {
			config.Unsynchronized = !parsed
		}
	}
	envInt("LOG_ROTATE_MAX_SIZE", &config.Rotation.MaxSizeMB)
	envInt("LOG_ROTATE_MAX_AGE", &config.Rotation.MaxAgeDays)
	envInt("LOG_ROTATE_MAX_BACKUPS", &config.Rotation.MaxBackups)
	if compress := os.Getenv("LOG_ROTATE_COMPRESS"); compress != "" {
		if parsed, err := strconv.ParseBool(compress); err == nil {
			config.Rotation.NoCompress = !parsed
		}
	}
	return config
}

func envBool(key string, dst *bool) {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			*dst = parsed
		}
	}
}

func envInt(key string, dst *int) {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			*dst = parsed
		}
	}
}

// LoadConfigFromFile reads a JSON configuration file.
func LoadConfigFromFile(filePath string) (Config, error) {
	config := DefaultConfig()
	file, err := os.Open(filePath)
	if err != nil {
		return config, err
	}
	// The close error on a read-only file is not actionable.
	defer func() { _ = file.Close() }()

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return config, err
	}
	return config, nil
}

// parseLogLevel converts a level name to a LogLevel, falling back to INFO for
// anything unrecognised.
func parseLogLevel(level string) LogLevel {
	lvl, _ := ParseLevel(level)
	return lvl
}
