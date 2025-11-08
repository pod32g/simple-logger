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

func BenchmarkSimpleLogger(b *testing.B) {
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkSimpleLoggerNoSync(b *testing.B) {
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{IncludeCaller: false})
	logger.SetSynchronized(false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkZapSugar(b *testing.B) {
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
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(io.Discard), zapcore.InfoLevel)
	logger := zap.New(core).Sugar()
	b.Cleanup(func() { logger.Sync() })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkLogrus(b *testing.B) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.InfoLevel)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", i)
	}
}

func BenchmarkZerolog(b *testing.B) {
	logger := zerolog.New(io.Discard).Level(zerolog.InfoLevel)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info().Int("i", i).Msg("benchmark message")
	}
}
