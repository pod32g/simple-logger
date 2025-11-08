package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	cfg := log.DefaultConfig()
	cfg.Format = "custom"
	cfg.Custom = &SimpleStreamingFormatter{}

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("User", 7, "performed action")
	logger.InfoFields("User fields", log.Int("user_id", 7), log.String("action", "performed"))
}

// SimpleStreamingFormatter implements ArgsFormatter for demonstration
type SimpleStreamingFormatter struct{}

func (f *SimpleStreamingFormatter) Format(level log.LogLevel, message string) string {
	var b strings.Builder
	f.FormatArgs(level, &b, message)
	return b.String()
}

func (f *SimpleStreamingFormatter) FormatWithFields(level log.LogLevel, message string, fields []log.Field) string {
	var b strings.Builder
	f.FormatArgsWithFields(level, fields, &b, message)
	return b.String()
}

func (f *SimpleStreamingFormatter) FormatArgsWithFields(level log.LogLevel, fields []log.Field, w io.Writer, v ...interface{}) {
	fmt.Fprintf(w, "%s [%s]", time.Now().Format("2006-01-02 15:04:05"), levelToString(level))
	for _, val := range v {
		fmt.Fprintf(w, " %v", val)
	}
	for _, field := range fields {
		fmt.Fprintf(w, " %s=%v", field.Key, field.Value)
	}
	fmt.Fprint(w, "\n")
}

func (f *SimpleStreamingFormatter) FormatArgs(level log.LogLevel, w io.Writer, v ...interface{}) {
	f.FormatArgsWithFields(level, nil, w, v...)
}

func levelToString(level log.LogLevel) string {
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
