package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

func TestConfigReloadEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	configPath := filepath.Join(dir, "logger.json")

	cfg := log.DefaultConfig()
	cfg.Output = logPath
	cfg.Format = "text"
	cfg.Level = log.INFO
	cfg.SyncWrites = true

	writeConfig(t, configPath, cfg)

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 8)
	go func() {
		err := log.WatchConfigFileForLogger(ctx, logger, configPath, 20*time.Millisecond, func(err error) {
			select {
			case errCh <- err:
			default:
			}
		})
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	logger.Info("startup", log.String("phase", "initial"))

	if err := waitForSubstring(logPath, "startup", 500*time.Millisecond); err != nil {
		t.Fatalf("did not observe initial log entry: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	cfg.Format = "json"
	cfg.Level = log.DEBUG
	writeConfig(t, configPath, cfg)

	deadline := time.Now().Add(4 * time.Second)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		logger.DebugFields("reload-debug", log.String("attempt", fmt.Sprintf("%d", attempt)))
		logger.InfoFields("reload-info", log.String("attempt", fmt.Sprintf("%d", attempt)))
		if err := hasJSONEntry(logPath, "reload-info", "INFO"); err == nil {
			cancel()
			time.Sleep(20 * time.Millisecond)
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("watcher returned error on shutdown: %v", err)
				}
			default:
			}
			return
		}
		select {
		case err := <-errCh:
			t.Fatalf("watcher error: %v", err)
		default:
		}
		time.Sleep(60 * time.Millisecond)
	}

	if data, err := os.ReadFile(logPath); err == nil {
		t.Fatalf("expected JSON log entry but none found; file contents:\n%s", string(data))
	} else {
		t.Fatalf("expected JSON log entry but failed to read log file: %v", err)
	}
}

func TestChannelReloadEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "channel.log")

	cfg := log.DefaultConfig()
	cfg.Output = logPath
	cfg.Format = "text"
	cfg.Level = log.INFO
	cfg.SyncWrites = true

	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updates := make(chan log.LoggerConfig, 1)
	errCh := make(chan error, 4)

	done := make(chan struct{})
	go func() {
		log.ReloadLoggerFromChannel(ctx, logger, updates, func(err error) {
			select {
			case errCh <- err:
			default:
			}
		})
		close(done)
	}()

	logger.Info("channel-start")
	if err := waitForSubstring(logPath, "channel-start", 500*time.Millisecond); err != nil {
		t.Fatalf("expected initial entry: %v", err)
	}

	newCfg := cfg
	newCfg.Format = "json"
	newCfg.Level = log.DEBUG

	updates <- newCfg

	deadline := time.Now().Add(3 * time.Second)
	success := false
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		logger.InfoFields("channel-info", log.Int("attempt", attempt))
		if err := hasJSONEntry(logPath, "channel-info", "INFO"); err == nil {
			success = true
			break
		}
		select {
		case err := <-errCh:
			t.Fatalf("reloader error: %v", err)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !success {
		if data, err := os.ReadFile(logPath); err == nil {
			t.Fatalf("expected JSON entry after channel reload; file contents:\n%s", string(data))
		} else {
			t.Fatalf("expected JSON entry but failed to read file: %v", err)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("reloader did not exit after cancel")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("reloader reported error on shutdown: %v", err)
		}
	default:
	}
}

func writeConfig(t *testing.T, path string, cfg log.LoggerConfig) {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func waitForSubstring(path, substr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), substr) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("substring %q not found in %s within %s", substr, path, timeout)
}

func hasJSONEntry(path, message, expectedLevel string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, message) {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		level, _ := payload["level"].(string)
		msg, _ := payload["message"].(string)
		if strings.EqualFold(level, expectedLevel) && msg == message {
			return nil
		}
	}
	return fmt.Errorf("json message %q not found", message)
}
