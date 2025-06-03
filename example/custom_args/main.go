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

	logger.Info("User", 7, "performed action")
}

// SimpleStreamingFormatter implements ArgsFormatter for demonstration
type SimpleStreamingFormatter struct{}

func (f *SimpleStreamingFormatter) Format(level log.LogLevel, message string) string {
	var b strings.Builder
	f.FormatArgs(level, &b, message)
	return b.String()
}

func (f *SimpleStreamingFormatter) FormatArgs(level log.LogLevel, w io.Writer, v ...interface{}) {
	fmt.Fprintf(w, "%s [%s] ", time.Now().Format("2006-01-02 15:04:05"), levelToString(level))
	for i, val := range v {
		if i > 0 {
			fmt.Fprint(w, " ")
		}
		fmt.Fprint(w, val)
	}
	fmt.Fprint(w, "\n")
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
