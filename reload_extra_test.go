package log_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestApplyConfigToNilLogger(t *testing.T) {
	apply := log.ApplyConfigTo(nil)
	if err := apply(log.DefaultConfig()); err == nil {
		t.Fatal("expected an error applying config to a nil logger")
	}
}

func TestApplyConfigToSurfacesError(t *testing.T) {
	var sink discardWriter
	logger := log.NewLogger(&sink, log.INFO, &log.DefaultFormatter{})
	apply := log.ApplyConfigTo(logger)

	cfg := log.DefaultConfig()
	cfg.Format = "custom"
	cfg.Custom = nil // ConfigureLogger should return an error

	if err := apply(cfg); err == nil {
		t.Fatal("expected ApplyConfigTo to surface the ConfigureLogger error")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestWatchConfigFileContinuesAfterError verifies the watcher keeps running
// after a failed reload (malformed file) and applies a subsequent valid config.
func TestWatchConfigFileContinuesAfterError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logger.json")
	writeFile(t, path, `{"output":"stdout","format":"text"}`)

	applies := make(chan struct{}, 16)
	errs := make(chan error, 16)
	apply := func(cfg log.LoggerConfig) error {
		applies <- struct{}{}
		return nil
	}
	onError := func(err error) { errs <- err }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = log.WatchConfigFile(ctx, path, 10*time.Millisecond, apply, onError)
	}()

	// Initial apply happens synchronously inside the watcher.
	waitSignal(t, applies, "initial apply")

	// Malformed content must trigger onError but not stop the watcher.
	writeFile(t, path, `{bad json`)
	waitError(t, errs, "reload error on malformed file")

	// A subsequent valid config must still be applied -> watcher kept running.
	writeFile(t, path, `{"output":"stdout","format":"json","colorize":true}`)
	waitSignal(t, applies, "apply after recovery")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitError(t *testing.T, ch <-chan error, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
