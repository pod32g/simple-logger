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

// NewHandler creates a slog handler backed by the provided simple logger.
func NewHandler(logger *log.Logger, level slog.Leveler) *Handler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &Handler{logger: logger, level: level, attrs: make([]slog.Attr, 0)}
}

func (h *Handler) Enabled(_ context.Context, lvl slog.Level) bool {
	base := slog.LevelInfo
	if h.level != nil {
		base = h.level.Level()
	}
	return lvl >= base
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
		h.logger.DebugFields(msg, fields...)
	case log.INFO:
		h.logger.InfoFields(msg, fields...)
	case log.WARN:
		h.logger.WarnFields(msg, fields...)
	case log.ERROR:
		h.logger.ErrorFields(msg, fields...)
	case log.FATAL:
		h.logger.FatalFields(msg, fields...)
	default:
		h.logger.InfoFields(msg, fields...)
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
	value := resolveValue(attr.Value)
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

func resolveValue(v slog.Value) slog.Value {
	for v.Kind() == slog.KindLogValuer {
		lv := v.LogValuer()
		if lv == nil {
			return slog.StringValue("")
		}
		v = lv.LogValue()
	}
	return v
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
