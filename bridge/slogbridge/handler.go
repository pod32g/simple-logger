package slogbridge

import (
	"context"
	"log/slog"

	log "github.com/pod32g/simple-logger"
)

// Handler routes slog records into simple-logger.
type Handler struct {
	logger *log.Logger
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

// NewHandler creates a slog handler backed by the provided simple logger. Pass a
// nil level to follow the logger's own level, so that changing it at runtime --
// with SetLevel, or the level endpoint in the httplog bridge -- reaches slog
// callers too. Pass an explicit Leveler to gate slog independently of it.
func NewHandler(logger *log.Logger, level slog.Leveler) *Handler {
	return &Handler{logger: logger, level: level, attrs: make([]slog.Attr, 0)}
}

// Enabled reports whether a record at lvl would be logged. slog consults this
// before building a record, so a level fixed here is the binding one: when it
// was pinned at construction, turning the underlying logger down to DEBUG could
// never make debug records appear.
func (h *Handler) Enabled(_ context.Context, lvl slog.Level) bool {
	if h.level != nil {
		return lvl >= h.level.Level()
	}
	if h.logger == nil {
		return lvl >= slog.LevelInfo
	}
	return h.logger.Enabled(levelToLogLevel(lvl))
}

func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]log.Field, 0, record.NumAttrs()+len(h.attrs))
	for _, attr := range h.attrs {
		h.appendAttr(&fields, attr, h.groups)
	}
	record.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&fields, a, h.groups)
		return true
	})

	msg := record.Message
	switch levelToLogLevel(record.Level) {
	case log.DEBUG:
		h.logger.Debug(msg, fields...)
	case log.INFO:
		h.logger.Info(msg, fields...)
	case log.WARN:
		h.logger.Warn(msg, fields...)
	case log.ERROR:
		h.logger.Error(msg, fields...)
	case log.FATAL:
		h.logger.Fatal(msg, fields...)
	default:
		h.logger.Info(msg, fields...)
	}
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	dup := *h
	dup.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &dup
}

func (h *Handler) WithGroup(name string) slog.Handler {
	dup := *h
	dup.groups = append(append([]string(nil), h.groups...), name)
	return &dup
}

func (h *Handler) appendAttr(dst *[]log.Field, attr slog.Attr, groups []string) {
	value := attr.Value.Resolve()
	key := qualifiedKey(groups, attr.Key)

	switch value.Kind() {
	case slog.KindString:
		*dst = append(*dst, log.String(key, value.String()))
	case slog.KindBool:
		*dst = append(*dst, log.Bool(key, value.Bool()))
	case slog.KindInt64:
		*dst = append(*dst, log.Any(key, value.Int64()))
	case slog.KindUint64:
		*dst = append(*dst, log.Any(key, value.Uint64()))
	case slog.KindFloat64:
		*dst = append(*dst, log.Any(key, value.Float64()))
	case slog.KindDuration:
		*dst = append(*dst, log.Any(key, value.Duration()))
	case slog.KindTime:
		*dst = append(*dst, log.Any(key, value.Time()))
	case slog.KindGroup:
		for _, child := range value.Group() {
			h.appendAttr(dst, child, append(groups, attr.Key))
		}
	default:
		*dst = append(*dst, log.Any(key, value.Any()))
	}
}

func qualifiedKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	full := groups[0]
	for _, g := range groups[1:] {
		full += "." + g
	}
	if key != "" {
		full += "." + key
	}
	return full
}

func levelToLogLevel(lvl slog.Level) log.LogLevel {
	switch {
	case lvl <= slog.LevelDebug:
		return log.DEBUG
	case lvl <= slog.LevelInfo:
		return log.INFO
	case lvl <= slog.LevelWarn:
		return log.WARN
	case lvl <= slog.LevelError:
		return log.ERROR
	default:
		return log.ERROR
	}
}
