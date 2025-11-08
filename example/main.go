package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	fmt.Println("== Basic configuration with color and custom time layout ==")
	basicLogging()

	fmt.Println("\n== Structured fields and context metadata ==")
	structuredLogging()

	fmt.Println("\n== Sampling and hooks ==")
	samplingAndHooks()

	fmt.Println("\n== Asynchronous logging ==")
	asyncLogging()

	fmt.Println("\n== Rolling file output (lumberjack) ==")
	rotationExample()

	fmt.Println("\n== Custom formatter using ArgsFormatter ==")
	customFormatter()
}

func basicLogging() {
	cfg := log.DefaultConfig()
	cfg.Colorize = true
	cfg.TimeFormat = time.RFC822
	cfg.IncludeStacktrace = true

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("welcome to simple-logger")
	logger.SetIncludeStacktrace(true)
	logger.Error("stacktraces appear automatically on errors")
}

func structuredLogging() {
	logger := log.ApplyConfig(log.DefaultConfig())
	defer logger.Close()

	logger.InfoFields("user login",
		log.String("user", "alice"),
		log.Bool("success", true),
	)

	ctx := log.WithFields(context.Background(), log.String("request_id", "req-123"))
	logger.InfoContext(ctx, "checkout complete", log.Float64("total", 42.10))
}

func samplingAndHooks() {
	logger := log.ApplyConfig(log.DefaultConfig())
	defer logger.Close()

	logger.SetSampler(log.NewEveryNSampler(3))
	logger.AddHook(log.HookFunc(func(level log.LogLevel, message string, fields []log.Field) {
		fmt.Printf("hook -> %s %q fields=%v\n", levelName(level), message, fields)
	}))

	for i := 1; i <= 6; i++ {
		logger.InfoFields("periodic heartbeat", log.Int("iteration", i))
	}
}

func asyncLogging() {
	logger := log.ApplyConfig(log.DefaultConfig())
	defer logger.Close()

	logger.EnableAsync(log.AsyncOptions{QueueSize: 64, DropStrategy: log.DropNew})
	for i := 0; i < 5; i++ {
		logger.InfoString(fmt.Sprintf("async message %d", i))
	}
	logger.DisableAsync()
}

func rotationExample() {
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("app-%d.log", time.Now().UnixNano()))

	cfg := log.DefaultConfig()
	cfg.Output = logPath
	cfg.Format = "json"
	cfg.Rotation.Enable = true
	cfg.Rotation.MaxSize = 1 // MB

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("rotation example", log.String("path", logPath))
	fmt.Printf("rotation example wrote to %s\n", logPath)
}

func customFormatter() {
	cfg := log.DefaultConfig()
	cfg.Format = "custom"
	cfg.Custom = &StreamingFormatter{}

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("custom formatter", log.String("user", "emma"))
}

// StreamingFormatter demonstrates implementing ArgsFormatter for high-performance logging
type StreamingFormatter struct{}

func (f *StreamingFormatter) Format(level log.LogLevel, message string) string {
	return fmt.Sprintf("[%s] %s\n", levelName(level), message)
}

func (f *StreamingFormatter) FormatArgs(level log.LogLevel, w io.Writer, v ...interface{}) {
	f.FormatArgsWithFields(level, nil, w, v...)
}

func (f *StreamingFormatter) FormatWithFields(level log.LogLevel, message string, fields []log.Field) string {
	var b strings.Builder
	f.FormatArgsWithFields(level, fields, &b, message)
	return b.String()
}

func (f *StreamingFormatter) FormatArgsWithFields(level log.LogLevel, fields []log.Field, w io.Writer, v ...interface{}) {
	fmt.Fprintf(w, "[%s]", levelName(level))
	for _, val := range v {
		fmt.Fprintf(w, " %v", val)
	}
	for _, field := range fields {
		fmt.Fprintf(w, " %s=%v", field.Key, field.Value)
	}
	fmt.Fprint(w, "\n")
}

func levelName(level log.LogLevel) string {
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
