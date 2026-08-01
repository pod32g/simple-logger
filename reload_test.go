package log_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestWatchReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logger.json")
	writeFile(t, path, `{"level":"info","output":"stdout","format":"text"}`)

	logger := log.Must(log.New(log.WithOutput(discardWriter{}), log.WithLevel(log.ERROR)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- logger.Watch(ctx, path, log.WatchInterval(5*time.Millisecond)) }()

	// The first load happens before Watch begins polling.
	waitForLevel(t, logger, log.INFO)

	writeFile(t, path, `{"level":"debug","output":"stdout","format":"text"}`)
	waitForLevel(t, logger, log.DEBUG)

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("watch returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not return after the context was cancelled")
	}
}

func TestReloadFromChannelAppliesConfigs(t *testing.T) {
	logger := log.Must(log.New(log.WithOutput(discardWriter{}), log.WithLevel(log.ERROR)))
	configs := make(chan log.Config, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		logger.ReloadFrom(ctx, configs)
		close(done)
	}()

	cfg := log.DefaultConfig()
	cfg.Level = log.DEBUG
	configs <- cfg
	waitForLevel(t, logger, log.DEBUG)

	cfg.Level = log.WARN
	configs <- cfg
	waitForLevel(t, logger, log.WARN)

	close(configs)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReloadFrom did not return when the channel closed")
	}
}

func TestReloadFromChannelReportsErrors(t *testing.T) {
	logger := log.Must(log.New(log.WithOutput(discardWriter{}), log.WithLevel(log.INFO)))
	configs := make(chan log.Config, 1)
	errs := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go logger.ReloadFrom(ctx, configs, log.WatchErrorHandler(func(err error) { errs <- err }))

	bad := log.DefaultConfig()
	bad.Format = "not-an-encoder"
	configs <- bad

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected a non-nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a failing config was not reported")
	}
}

func waitForLevel(t *testing.T, logger *log.Logger, want log.LogLevel) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if logger.Level() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("level did not reach %v, still %v", want, logger.Level())
}
