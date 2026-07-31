package otlp

import (
	"context"
	"testing"

	log "github.com/pod32g/simple-logger"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

type fakeExporter struct {
	seen []*commonpb.KeyValue
}

func (f *fakeExporter) Export(_ context.Context, resourceLogs *logspb.ResourceLogs) error {
	if len(resourceLogs.ScopeLogs) > 0 {
		records := resourceLogs.ScopeLogs[0].LogRecords
		if len(records) > 0 {
			f.seen = records[0].Attributes
		}
	}
	return nil
}

func (f *fakeExporter) Shutdown(context.Context) error { return nil }

func TestHookExportsFields(t *testing.T) {
	fake := &fakeExporter{}
	hook := NewHook(fake, WithServiceName("simple"), WithSynchronousExport())

	hook.Fire(log.INFO, "hello", []log.Field{log.String("user", "alice")})

	if len(fake.seen) == 0 {
		t.Fatalf("expected attributes to be exported")
	}
	if fake.seen[0].GetKey() != "user" {
		t.Fatalf("expected user attribute, got %s", fake.seen[0].GetKey())
	}
}
