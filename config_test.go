package log_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestDefaultConfig(t *testing.T) {
	cfg := log.DefaultConfig()
	if cfg.Level != log.INFO {
		t.Errorf("expected level INFO, got %v", cfg.Level)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected output stdout, got %s", cfg.Output)
	}
	if cfg.Format != "text" {
		t.Errorf("expected format text, got %s", cfg.Format)
	}
	if !cfg.EnableCaller {
		t.Errorf("expected EnableCaller true")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	os.Setenv("LOG_OUTPUT", "stderr")
	os.Setenv("LOG_FORMAT", "json")
	os.Setenv("LOG_ENABLE_CALLER", "false")
	defer os.Unsetenv("LOG_LEVEL")
	defer os.Unsetenv("LOG_OUTPUT")
	defer os.Unsetenv("LOG_FORMAT")
	defer os.Unsetenv("LOG_ENABLE_CALLER")

	cfg := log.LoadConfigFromEnv()
	if cfg.Level != log.DEBUG {
		t.Errorf("expected level DEBUG, got %v", cfg.Level)
	}
	if cfg.Output != "stderr" {
		t.Errorf("expected output stderr, got %s", cfg.Output)
	}
	if cfg.Format != "json" {
		t.Errorf("expected format json, got %s", cfg.Format)
	}
	if cfg.EnableCaller {
		t.Errorf("expected EnableCaller false")
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cfg.json")
	data := []byte(`{"level":3,"output":"stdout","format":"json","enable_caller":false}`)
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	cfg, err := log.LoadConfigFromFile(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Level != log.ERROR {
		t.Errorf("expected level ERROR, got %v", cfg.Level)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected output stdout, got %s", cfg.Output)
	}
	if cfg.Format != "json" {
		t.Errorf("expected format json, got %s", cfg.Format)
	}
	if cfg.EnableCaller {
		t.Errorf("expected EnableCaller false")
	}
}

func TestUpdateLogLevelAndFormat(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.UpdateLogLevel(log.DEBUG)
	cfg.UpdateLogFormat("json")
	if cfg.Level != log.DEBUG {
		t.Errorf("expected level DEBUG, got %v", cfg.Level)
	}
	if cfg.Format != "json" {
		t.Errorf("expected format json, got %s", cfg.Format)
	}
}

func TestApplyConfigText(t *testing.T) {
	cfg := log.DefaultConfig()
	logger := log.ApplyConfig(cfg)
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("hello")
	if !containsLogMessage(buf.String(), "INFO", "hello") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestApplyConfigJSON(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Format = "json"
	logger := log.ApplyConfig(cfg)
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("hello")
	if !isValidJSON(buf.String()) {
		t.Errorf("expected JSON output, got %s", buf.String())
	}
}

func TestApplyConfigCustomFormatter(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Format = "custom"
	cfg.Custom = &testFormatter{}
	logger := log.ApplyConfig(cfg)
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("msg")
	if buf.String() != "CUSTOM(INFO) msg\n" {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

type testFormatter struct{}

func (testFormatter) Format(level log.LogLevel, message string) string {
	return "CUSTOM(" + logLevelToString(level) + ") " + message + "\n"
}

func TestApplyConfigFileOutput(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "log.txt")
	cfg := log.DefaultConfig()
	cfg.Output = file
	logger := log.ApplyConfig(cfg)
	logger.Info("file message")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("unable to read file: %v", err)
	}
	if !bytes.Contains(data, []byte("file message")) {
		t.Errorf("expected message in file, got %s", string(data))
	}
}

func TestApplyConfigDisableCaller(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.EnableCaller = false
	logger := log.ApplyConfig(cfg)
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("msg")
	if strings.Contains(buf.String(), ".go:") {
		t.Errorf("expected no caller info, got %s", buf.String())
	}
}
