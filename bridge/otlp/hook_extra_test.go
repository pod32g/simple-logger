package otlp

import (
	"context"
	"errors"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// recordingExporter captures the most recent record and resource.
type recordingExporter struct {
	record   *logspb.LogRecord
	resource *resourcepb.Resource
}

func (r *recordingExporter) Export(_ context.Context, rl *logspb.ResourceLogs) error {
	r.resource = rl.Resource
	if len(rl.ScopeLogs) > 0 && len(rl.ScopeLogs[0].LogRecords) > 0 {
		r.record = rl.ScopeLogs[0].LogRecords[0]
	}
	return nil
}

func (r *recordingExporter) Shutdown(context.Context) error { return nil }

func attrByKey(record *logspb.LogRecord, key string) *commonpb.AnyValue {
	for _, kv := range record.Attributes {
		if kv.GetKey() == key {
			return kv.GetValue()
		}
	}
	return nil
}

func TestHookAttributeValueTypes(t *testing.T) {
	exp := &recordingExporter{}
	hook := NewHook(exp)

	type custom struct{ A int }
	hook.Fire(log.INFO, "types", []log.Field{
		log.Bool("b", true),
		log.Int("i", 7),
		log.Int64("i64", 9),
		log.Any("u64", uint64(11)),
		log.Float64("f64", 1.5),
		log.Any("f32", float32(2.5)),
		log.Any("dur", 3*time.Second),
		log.Any("obj", custom{A: 1}),
	})

	if v := attrByKey(exp.record, "b"); v.GetBoolValue() != true {
		t.Fatalf("bool attr: got %v", v)
	}
	if v := attrByKey(exp.record, "i"); v.GetIntValue() != 7 {
		t.Fatalf("int attr: got %v", v)
	}
	if v := attrByKey(exp.record, "i64"); v.GetIntValue() != 9 {
		t.Fatalf("int64 attr: got %v", v)
	}
	if v := attrByKey(exp.record, "u64"); v.GetIntValue() != 11 {
		t.Fatalf("uint64 attr: got %v", v)
	}
	if v := attrByKey(exp.record, "f64"); v.GetDoubleValue() != 1.5 {
		t.Fatalf("float64 attr: got %v", v)
	}
	if v := attrByKey(exp.record, "f32"); v.GetDoubleValue() != 2.5 {
		t.Fatalf("float32 attr: got %v", v)
	}
	if v := attrByKey(exp.record, "dur"); v.GetStringValue() != "3s" {
		t.Fatalf("duration attr: got %v", v)
	}
	if v := attrByKey(exp.record, "obj"); v.GetStringValue() == "" {
		t.Fatalf("struct attr should stringify, got empty")
	}
}

func TestHookSeverityMapping(t *testing.T) {
	cases := []struct {
		level log.LogLevel
		num   logspb.SeverityNumber
		text  string
	}{
		{log.DEBUG, logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG, "DEBUG"},
		{log.INFO, logspb.SeverityNumber_SEVERITY_NUMBER_INFO, "INFO"},
		{log.WARN, logspb.SeverityNumber_SEVERITY_NUMBER_WARN, "WARN"},
		{log.ERROR, logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, "ERROR"},
		{log.FATAL, logspb.SeverityNumber_SEVERITY_NUMBER_FATAL, "FATAL"},
	}
	for _, tc := range cases {
		exp := &recordingExporter{}
		hook := NewHook(exp)
		hook.Fire(tc.level, "msg", nil)
		if exp.record.GetSeverityNumber() != tc.num {
			t.Errorf("level %v: severity number got %v want %v", tc.level, exp.record.GetSeverityNumber(), tc.num)
		}
		if exp.record.GetSeverityText() != tc.text {
			t.Errorf("level %v: severity text got %q want %q", tc.level, exp.record.GetSeverityText(), tc.text)
		}
	}
}

func TestHookResourceAttributes(t *testing.T) {
	exp := &recordingExporter{}
	hook := NewHook(exp, WithServiceName("checkout"), WithResourceAttribute("deployment.environment", "prod"))
	hook.Fire(log.INFO, "msg", nil)

	found := map[string]string{}
	for _, kv := range exp.resource.GetAttributes() {
		found[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	if found["service.name"] != "checkout" {
		t.Errorf("expected service.name=checkout, got %q", found["service.name"])
	}
	if found["deployment.environment"] != "prod" {
		t.Errorf("expected deployment.environment=prod, got %q", found["deployment.environment"])
	}
}

type shutdownExporter struct {
	called bool
	err    error
}

func (s *shutdownExporter) Export(context.Context, *logspb.ResourceLogs) error { return nil }
func (s *shutdownExporter) Shutdown(context.Context) error {
	s.called = true
	return s.err
}

func TestHookCloseShutsDownExporter(t *testing.T) {
	sentinel := errors.New("shutdown failed")
	exp := &shutdownExporter{err: sentinel}
	hook := NewHook(exp)
	if err := hook.Close(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("expected shutdown error propagated, got %v", err)
	}
	if !exp.called {
		t.Fatalf("expected Shutdown to be called")
	}
}

type failingExporter struct{ err error }

func (f *failingExporter) Export(context.Context, *logspb.ResourceLogs) error { return f.err }
func (f *failingExporter) Shutdown(context.Context) error                     { return nil }

func TestHookErrorHandlerAndStats(t *testing.T) {
	sentinel := errors.New("export failed")
	var got error
	hook := NewHook(&failingExporter{err: sentinel}, WithErrorHandler(func(err error) { got = err }))

	hook.Fire(log.INFO, "msg", nil)

	if !errors.Is(got, sentinel) {
		t.Fatalf("expected error handler to receive export error, got %v", got)
	}
	if s := hook.Stats(); s.Failed != 1 {
		t.Fatalf("expected Failed=1, got %d", s.Failed)
	}
}

func TestHookNilExporterIsSafe(t *testing.T) {
	hook := NewHook(nil)
	// Must not panic.
	hook.Fire(log.INFO, "msg", []log.Field{log.String("k", "v")})
	if err := hook.Close(context.Background()); err != nil {
		t.Fatalf("expected nil error closing a nil-exporter hook, got %v", err)
	}
}
