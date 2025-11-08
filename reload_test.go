package log_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestWatchConfigFileReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logger.json")

	cfg := log.DefaultConfig()
	cfg.Level = log.INFO
	writeConfigFile(t, path, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	levels := make([]log.LogLevel, 0, 2)
	apply := func(c log.LoggerConfig) error {
		mu.Lock()
		levels = append(levels, c.Level)
		mu.Unlock()
		return nil
	}

	errCh := make(chan error, 1)
	go func() { errCh <- log.WatchConfigFile(ctx, path, 5*time.Millisecond, apply, func(error) {}) }()

	waitForLevel := func(expected log.LogLevel) {
		deadline := time.Now().Add(500 * time.Millisecond)
		for {
			mu.Lock()
			hit := len(levels) > 0 && levels[len(levels)-1] == expected
			mu.Unlock()
			if hit {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timeout waiting for level %v", expected)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	waitForLevel(log.INFO)

	cfg.Level = log.DEBUG
	writeConfigFile(t, path, cfg)
	waitForLevel(log.DEBUG)

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("watcher returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("watcher did not exit after cancel")
	}

	mu.Lock()
	if len(levels) < 2 {
		t.Fatalf("expected at least two reloads, got %d", len(levels))
	}
	mu.Unlock()
}

func TestReloadFromChannelAppliesConfigs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configs := make(chan log.LoggerConfig, 1)
	done := make(chan struct{})

	var mu sync.Mutex
	var last log.LoggerConfig
	apply := func(cfg log.LoggerConfig) error {
		mu.Lock()
		last = cfg
		mu.Unlock()
		return nil
	}

	go func() {
		log.ReloadFromChannel(ctx, configs, apply, nil)
		close(done)
	}()

	cfg := log.DefaultConfig()
	cfg.Level = log.ERROR
	configs <- cfg

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		mu.Lock()
		level := last.Level
		mu.Unlock()
		if level == log.ERROR {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for level update")
		}
		time.Sleep(5 * time.Millisecond)
	}

	errs := make(chan error, 1)
	applyErr := func(cfg log.LoggerConfig) error {
		errs <- errSentinel
		return errSentinel
	}

	errorCtx, errorCancel := context.WithCancel(context.Background())
	defer errorCancel()

	errorCh := make(chan log.LoggerConfig, 1)
	seenErr := make(chan struct{})
	go func() {
		log.ReloadFromChannel(errorCtx, errorCh, applyErr, func(err error) {
			if err == errSentinel {
				close(seenErr)
			}
		})
	}()

	errorCh <- cfg

	select {
	case <-seenErr:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected error handler to be invoked")
	}

	errorCancel()
	close(configs)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("reloader did not exit after channel close")
	}
}

var errSentinel = errors.New("apply error")

func writeConfigFile(t *testing.T, path string, cfg log.LoggerConfig) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
