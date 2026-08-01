package log_test

import (
	"io"
	"testing"

	log "github.com/pod32g/simple-logger"
	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Every benchmark below logs the same message with the same single typed field,
// so the comparison is between encoders rather than between calling styles. The
// earlier version of this file compared our concatenating variadic call against
// zerolog's typed field, which flattered nobody.

func BenchmarkSimpleLogger(b *testing.B) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", log.Int("i", i))
	}
}

func BenchmarkSimpleLoggerJSON(b *testing.B) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO), log.WithJSON()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", log.Int("i", i))
	}
}

func BenchmarkSimpleLoggerNoSync(b *testing.B) {
	logger := log.Must(log.New(log.WithOutput(io.Discard), log.WithLevel(log.INFO), log.WithUnsynchronized()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", log.Int("i", i))
	}
}

func zapCore() zapcore.Core {
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "",
		LevelKey:       "level",
		NameKey:        "",
		CallerKey:      "",
		MessageKey:     "msg",
		StacktraceKey:  "",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	return zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(io.Discard), zapcore.InfoLevel)
}

func BenchmarkZap(b *testing.B) {
	logger := zap.New(zapCore())
	b.Cleanup(func() { _ = logger.Sync() })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", zap.Int("i", i))
	}
}

func BenchmarkZapSugar(b *testing.B) {
	logger := zap.New(zapCore()).Sugar()
	b.Cleanup(func() { _ = logger.Sync() })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Infow("benchmark message", "i", i)
	}
}

func BenchmarkLogrus(b *testing.B) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.InfoLevel)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithField("i", i).Info("benchmark message")
	}
}

func BenchmarkZerolog(b *testing.B) {
	logger := zerolog.New(io.Discard).Level(zerolog.InfoLevel)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info().Int("i", i).Msg("benchmark message")
	}
}
