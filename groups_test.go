package log_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestGroupNestsInJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	logger.Info("request",
		log.String("id", "r1"),
		log.Group("http",
			log.String("method", "GET"),
			log.Int("status", 200),
			log.Group("client", log.String("ip", "10.0.0.1"))))

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	http, ok := out["http"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a nested http object, got %v", out["http"])
	}
	if http["method"] != "GET" || http["status"] != float64(200) {
		t.Errorf("unexpected group contents: %v", http)
	}
	client, ok := http["client"].(map[string]interface{})
	if !ok || client["ip"] != "10.0.0.1" {
		t.Errorf("expected a nested client object, got %v", http["client"])
	}
}

// Text output stays one line per entry, so groups flatten with a dotted prefix.
func TestGroupFlattensInText(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	logger.Info("request", log.Group("http", log.String("method", "GET"), log.Int("status", 200)))

	got := strings.TrimRight(buf.String(), "\n")
	if strings.Contains(got, "\n") {
		t.Fatalf("expected a single line, got %q", got)
	}
	for _, want := range []string{"http.method=GET", "http.status=200"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// The point of Lazy: an entry that is dropped costs nothing.
func TestLazyIsNotEvaluatedWhenDropped(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.ERROR)))

	calls := 0
	expensive := log.Lazy("detail", func() interface{} {
		calls++
		return "computed"
	})

	logger.Info("below threshold", expensive)
	if calls != 0 {
		t.Errorf("lazy value computed %d times for a dropped entry", calls)
	}

	logger.Error("emitted", expensive)
	if calls != 1 {
		t.Errorf("lazy value computed %d times, want 1", calls)
	}
	if !strings.Contains(buf.String(), "detail=computed") {
		t.Errorf("expected the resolved value, got %q", buf.String())
	}
}

func TestLazyInsideGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO), log.WithJSON()))

	logger.Info("m", log.Group("g", log.Lazy("k", func() interface{} { return 42 })))

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	g, ok := out["g"].(map[string]interface{})
	if !ok || g["k"] != float64(42) {
		t.Errorf("expected the lazy value resolved inside the group, got %v", out["g"])
	}
}

// Redaction runs after lazy resolution, so a deferred secret is still masked.
func TestLazyValuesAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO),
		log.WithRedactor(log.NewKeyRedactor("password"))))

	logger.Info("login", log.Lazy("password", func() interface{} { return "hunter2" }))

	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("a deferred secret escaped redaction: %q", buf.String())
	}
}
