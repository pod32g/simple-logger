package log_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestApplyRejectsUnknownFormat(t *testing.T) {
	var sink discardWriter
	logger := log.Must(log.New(log.WithOutput(&sink), log.WithLevel(log.INFO)))

	cfg := log.DefaultConfig()
	cfg.Format = "custom" // no such built-in encoder

	if err := logger.Apply(cfg); err == nil {
		t.Fatal("expected Apply to reject an unknown format")
	}
}

// A failed Apply must leave the logger as it was rather than half-configured.
func TestApplyLeavesLoggerUsableAfterError(t *testing.T) {
	var buf lockedBuffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	cfg := log.DefaultConfig()
	cfg.Format = "nonsense"
	if err := logger.Apply(cfg); err == nil {
		t.Fatal("expected an error")
	}

	logger.Info("still working")
	if !strings.Contains(buf.String(), "still working") {
		t.Errorf("logger stopped working after a failed Apply: %q", buf.String())
	}
}

// The watcher keeps running after a failed reload (a malformed file) and picks
// up the next valid configuration.
func TestWatchContinuesAfterError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logger.json")
	writeFile(t, path, `{"output":"stdout","format":"text"}`)

	var mu sync.Mutex
	var errs []error

	logger := log.Must(log.New(log.WithOutput(discardWriter{}), log.WithLevel(log.INFO)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = logger.Watch(ctx, path,
			log.WatchInterval(10*time.Millisecond),
			log.WatchErrorHandler(func(err error) {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}))
	}()

	// A malformed file must be reported, not fatal.
	time.Sleep(30 * time.Millisecond)
	writeFile(t, path, `{not json`)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(errs)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	n := len(errs)
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected the malformed config to be reported")
	}

	// A subsequent valid file is applied, proving the watcher survived.
	writeFile(t, path, `{"output":"stdout","format":"json","level":"warn"}`)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if logger.Level() == log.WARN {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("watcher did not apply the later config, level is %v", logger.Level())
}

func TestWatchReturnsErrorForMissingFile(t *testing.T) {
	logger := log.Must(log.New(log.WithOutput(discardWriter{}), log.WithLevel(log.INFO)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := logger.Watch(ctx, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
