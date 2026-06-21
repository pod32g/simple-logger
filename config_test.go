package log_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	if cfg.EnableCaller {
		t.Errorf("expected EnableCaller false")
	}
	if !cfg.SyncWrites {
		t.Errorf("expected SyncWrites true")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	os.Setenv("LOG_OUTPUT", "stderr")
	os.Setenv("LOG_FORMAT", "json")
	os.Setenv("LOG_ENABLE_CALLER", "false")
	os.Setenv("LOG_SYNC_WRITES", "false")
	os.Setenv("LOG_COLORIZE", "true")
	os.Setenv("LOG_TIME_FORMAT", time.RFC822)
	os.Setenv("LOG_INCLUDE_STACKTRACE", "true")
	defer os.Unsetenv("LOG_LEVEL")
	defer os.Unsetenv("LOG_OUTPUT")
	defer os.Unsetenv("LOG_FORMAT")
	defer os.Unsetenv("LOG_ENABLE_CALLER")
	defer os.Unsetenv("LOG_SYNC_WRITES")
	defer os.Unsetenv("LOG_COLORIZE")
	defer os.Unsetenv("LOG_TIME_FORMAT")
	defer os.Unsetenv("LOG_INCLUDE_STACKTRACE")

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
	if cfg.SyncWrites {
		t.Errorf("expected SyncWrites false")
	}
	if !cfg.Colorize {
		t.Errorf("expected Colorize true")
	}
	if cfg.TimeFormat != time.RFC822 {
		t.Errorf("expected TimeFormat %q, got %q", time.RFC822, cfg.TimeFormat)
	}
	if !cfg.IncludeStacktrace {
		t.Errorf("expected IncludeStacktrace true")
	}
	if cfg.Rotation.Enable {
		t.Errorf("expected rotation disabled by default when LOG_ROTATE not set")
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
	if !isValidJSONConfig(buf.String()) {
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

func TestApplyConfigSyncWrites(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.SyncWrites = false
	logger := log.ApplyConfig(cfg)
	if logger.Synchronized() {
		t.Fatalf("expected logger to disable synchronized writes")
	}
	logger.SetSynchronized(true)
	if !logger.Synchronized() {
		t.Fatalf("expected to re-enable synchronized writes")
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
	if err := logger.Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("unable to read file: %v", err)
	}
	if !bytes.Contains(data, []byte("file message")) {
		t.Errorf("expected message in file, got %s", string(data))
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// Unix file-permission bits are not meaningful on Windows, where os.Chmod
	// only toggles the read-only attribute, so skip the assertion there.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("expected file permissions 0600, got %o", info.Mode().Perm())
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

func TestLoadConfigFromEnvBoolVariants(t *testing.T) {
	t.Setenv("LOG_ENABLE_CALLER", "TRUE")
	cfg := log.LoadConfigFromEnv()
	if !cfg.EnableCaller {
		t.Errorf("expected EnableCaller true for LOG_ENABLE_CALLER=TRUE")
	}

	t.Setenv("LOG_COLORIZE", "1")
	cfg = log.LoadConfigFromEnv()
	if !cfg.Colorize {
		t.Errorf("expected Colorize true for LOG_COLORIZE=1")
	}

	t.Setenv("LOG_ENABLE_CALLER", "0")
	cfg = log.LoadConfigFromEnv()
	if cfg.EnableCaller {
		t.Errorf("expected EnableCaller false for LOG_ENABLE_CALLER=0")
	}
}

func TestLoadConfigFromEnvRotation(t *testing.T) {
	t.Setenv("LOG_ROTATE", "true")
	t.Setenv("LOG_ROTATE_MAX_SIZE", "10")
	t.Setenv("LOG_ROTATE_MAX_AGE", "5")
	t.Setenv("LOG_ROTATE_MAX_BACKUPS", "3")
	t.Setenv("LOG_ROTATE_COMPRESS", "false")

	cfg := log.LoadConfigFromEnv()
	if !cfg.Rotation.Enable {
		t.Fatalf("expected rotation enabled")
	}
	if cfg.Rotation.MaxSize != 10 {
		t.Fatalf("expected MaxSize 10, got %d", cfg.Rotation.MaxSize)
	}
	if cfg.Rotation.MaxAge != 5 {
		t.Fatalf("expected MaxAge 5, got %d", cfg.Rotation.MaxAge)
	}
	if cfg.Rotation.MaxBackups != 3 {
		t.Fatalf("expected MaxBackups 3, got %d", cfg.Rotation.MaxBackups)
	}
	if cfg.Rotation.Compress {
		t.Fatalf("expected Compress false")
	}
}

func TestConfigureLoggerColorize(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Colorize = true
	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("colored")

	if !strings.Contains(buf.String(), "\u001b[") {
		t.Fatalf("expected ANSI color codes in output, got %q", buf.String())
	}
}

func TestConfigureLoggerSwitchFormat(t *testing.T) {
	logger := log.NewLogger(io.Discard, log.INFO, &log.DefaultFormatter{})
	defer logger.Close()

	cfg := log.DefaultConfig()
	cfg.Format = "json"
	_, err := log.ConfigureLogger(logger, cfg)
	if err != nil {
		t.Fatalf("configure logger: %v", err)
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("json message")
	if !isValidJSONConfig(buf.String()) {
		t.Fatalf("expected JSON output, got %q", buf.String())
	}
}

func TestConfigureLoggerTimeFormatAndStacktrace(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.TimeFormat = "2006-01-02"
	cfg.IncludeStacktrace = true
	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	expected := time.Now().Format(cfg.TimeFormat)
	logger.Error("with stack")

	output := buf.String()
	if !strings.Contains(output, "with stack") {
		t.Fatalf("expected message, got %q", output)
	}
	if !strings.Contains(output, "stacktrace") {
		t.Fatalf("expected stacktrace field, got %q", output)
	}
	if !strings.Contains(output, expected) {
		t.Fatalf("expected custom time layout %q, got %q", expected, output)
	}
}

func TestConfigureLoggerRotation(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Output = filepath.Join(t.TempDir(), "rotating.log")
	cfg.Rotation = log.RotationConfig{Enable: true, MaxSize: 1}

	logger, err := log.ConfigureLogger(nil, cfg)
	if err != nil {
		t.Fatalf("configure logger: %v", err)
	}
	defer logger.Close()

	logger.Info("rotation test")
}

func isValidJSONConfig(s string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}
