package log_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

// TestApplyConfigFallbackOnError verifies that ApplyConfig never returns a
// broken logger: when the configuration is invalid (custom format with no
// Custom formatter), it falls back to a working DefaultFormatter logger.
func TestApplyConfigFallbackOnError(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Format = "custom"
	cfg.Custom = nil // forces formatterForConfig to error

	logger := log.ApplyConfig(cfg)
	if logger == nil {
		t.Fatal("ApplyConfig must return a non-nil fallback logger")
	}
	defer logger.Close()

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("fallback works")
	if !strings.Contains(buf.String(), "fallback works") {
		t.Fatalf("expected fallback logger to log, got %q", buf.String())
	}
}

func TestConfigureLoggerFileOpenError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory-as-file open semantics differ on Windows")
	}
	cfg := log.DefaultConfig()
	// Parent directory does not exist, so os.OpenFile must fail.
	cfg.Output = filepath.Join(t.TempDir(), "missing", "app.log")

	_, err := log.ConfigureLogger(nil, cfg)
	if err == nil {
		t.Fatal("expected an error opening an unwritable log file")
	}
	if !strings.Contains(err.Error(), "open log file") {
		t.Fatalf("expected wrapped open error, got %v", err)
	}
}

// TestConfigFilepathPrecedence verifies that the Filepath field designates the
// file destination even when Output keeps its default.
func TestConfigFilepathPrecedence(t *testing.T) {
	file := filepath.Join(t.TempDir(), "via-filepath.log")
	cfg := log.DefaultConfig() // Output stays "stdout"
	cfg.Filepath = file

	logger := log.ApplyConfig(cfg)
	logger.Info("routed via filepath")
	if err := logger.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("expected log file created from Filepath: %v", err)
	}
	if !strings.Contains(string(data), "routed via filepath") {
		t.Fatalf("expected message in Filepath destination, got %q", string(data))
	}
}
