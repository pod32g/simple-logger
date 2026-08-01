package slogbridge

import (
	"context"
	"log/slog"
	"runtime"

	log "github.com/pod32g/simple-logger"
)

// Handler routes slog records into simple-logger.
//
// It satisfies the contract testing/slogtest checks, which an earlier version
// did not: groups nest rather than flattening into dotted keys, the record's
// own timestamp and source location are used instead of being discarded, empty
// attributes are skipped, and a group with no attributes is omitted entirely.
type Handler struct {
	logger *log.Logger
	level  slog.Leveler
	// fields is the accumulated tree of attrs bound by WithAttrs. groups is the
	// path within that tree where new attrs are attached, so repeated
	// WithGroup/WithAttrs pairs merge into one node instead of producing
	// sibling groups with the same key.
	fields []log.Field
	groups []string
}

// NewHandler creates a slog handler backed by the provided logger. Pass a nil
// level to follow the logger's own level, so that changing it at runtime --
// with SetLevel, or the level endpoint in the httplog bridge -- reaches slog
// callers too. Pass an explicit Leveler to gate slog independently of it.
func NewHandler(logger *log.Logger, level slog.Leveler) *Handler {
	return &Handler{logger: logger, level: level}
}

// Enabled reports whether a record at lvl would be logged. slog consults this
// before building a record, so a level fixed here is the binding one.
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
	var recordFields []log.Field
	record.Attrs(func(a slog.Attr) bool {
		recordFields = appendAttr(recordFields, a)
		return true
	})
	// Record attrs attach at the current path, merging into the same group nodes
	// the bound attrs already occupy.
	fields := insertAt(h.fields, h.groups, recordFields)

	var opts []log.EntryOption
	if record.Time.IsZero() {
		// slog says a record with no time gets no timestamp, rather than the
		// moment it happened to be encoded.
		opts = append(opts, log.EntryNoTime())
	} else {
		opts = append(opts, log.EntryTime(record.Time))
	}
	if record.PC != 0 {
		if frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next(); frame.File != "" {
			opts = append(opts, log.EntryCaller(basename(frame.File), frame.Line))
		}
	}

	h.logger.Log(levelToLogLevel(record.Level), record.Message, fields, opts...)
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	var converted []log.Field
	for _, a := range attrs {
		converted = appendAttr(converted, a)
	}
	dup := *h
	dup.fields = insertAt(h.fields, h.groups, converted)
	return &dup
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	dup := *h
	dup.groups = append(append([]string(nil), h.groups...), name)
	return &dup
}

// appendAttr converts one attr. Empty attrs are dropped, a group with an empty
// key is inlined into its parent, and an empty group disappears -- all three
// are conformance requirements rather than preferences.
func appendAttr(dst []log.Field, attr slog.Attr) []log.Field {
	if attr.Equal(slog.Attr{}) {
		return dst
	}
	value := attr.Value.Resolve()

	if value.Kind() == slog.KindGroup {
		var nested []log.Field
		for _, child := range value.Group() {
			nested = appendAttr(nested, child)
		}
		if len(nested) == 0 {
			return dst
		}
		if attr.Key == "" {
			return append(dst, nested...)
		}
		return append(dst, log.Group(attr.Key, nested...))
	}
	if attr.Key == "" {
		return dst
	}
	return append(dst, fieldFor(attr.Key, value))
}

func fieldFor(key string, value slog.Value) log.Field {
	switch value.Kind() {
	case slog.KindString:
		return log.String(key, value.String())
	case slog.KindBool:
		return log.Bool(key, value.Bool())
	case slog.KindInt64:
		return log.Int64(key, value.Int64())
	case slog.KindUint64:
		return log.Uint64(key, value.Uint64())
	case slog.KindFloat64:
		return log.Float64(key, value.Float64())
	case slog.KindDuration:
		return log.Duration(key, value.Duration())
	case slog.KindTime:
		return log.Time(key, value.Time())
	default:
		return log.Any(key, value.Any())
	}
}

// insertAt attaches add to tree at the given group path, reusing a group node
// that is already there rather than adding a second one with the same key.
// Without that merge, WithGroup("G").WithAttrs(a).WithGroup("H").WithAttrs(b)
// produces two "G" keys, and one of them wins.
func insertAt(tree []log.Field, path []string, add []log.Field) []log.Field {
	if len(add) == 0 {
		return append([]log.Field(nil), tree...)
	}
	if len(path) == 0 {
		return append(append([]log.Field(nil), tree...), add...)
	}

	out := append([]log.Field(nil), tree...)
	name := path[0]
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Key != name {
			continue
		}
		if children, ok := out[i].Value.([]log.Field); ok {
			out[i] = log.Group(name, insertAt(children, path[1:], add)...)
			return out
		}
	}
	return append(out, log.Group(name, insertAt(nil, path[1:], add)...))
}

func basename(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func levelToLogLevel(lvl slog.Level) log.LogLevel {
	switch {
	case lvl < slog.LevelDebug:
		// slog has no TRACE; anything below Debug maps onto it.
		return log.TRACE
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
