package log_test

import (
	"fmt"
	"io"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func BenchmarkLoggerDefault(b *testing.B) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO), log.WithCaller()))
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", log.Int("i", i))
	}
}

func BenchmarkLoggerNoCaller(b *testing.B) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO)))
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", log.Int("i", i))
	}
}

func BenchmarkFmtSprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("benchmark message %d", i)
	}
}
