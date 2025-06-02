package log_test

import (
	"fmt"
	"io"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func BenchmarkLoggerDefault(b *testing.B) {
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{IncludeCaller: true})
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkLoggerNoCaller(b *testing.B) {
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkFmtSprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("benchmark message %d", i)
	}
}
