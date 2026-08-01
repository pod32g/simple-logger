package log_test

import (
	"bytes"
	"encoding/json"
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

// --- merged from config_json_test.go ---

func TestLoadConfigAcceptsLevelName(t *testing.T) {
	cases := map[string]log.LogLevel{
		`{"level":"debug"}`:   log.DEBUG,
		`{"level":"WARN"}`:    log.WARN,
		`{"level":"warning"}`: log.WARN,
		`{"level":"error"}`:   log.ERROR,
		`{"level":3}`:         log.ERROR,
	}
	for body, want := range cases {
		path := filepath.Join(t.TempDir(), "cfg.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := log.LoadConfigFromFile(path)
		if err != nil {
			t.Errorf("%s: %v", body, err)
			continue
		}
		if cfg.Level != want {
			t.Errorf("%s: level = %v, want %v", body, cfg.Level, want)
		}
	}
}

func TestLoadConfigRejectsUnknownLevel(t *testing.T) {
	for _, body := range []string{`{"level":"louder"}`, `{"level":9}`} {
		path := filepath.Join(t.TempDir(), "cfg.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := log.LoadConfigFromFile(path); err == nil {
			t.Errorf("%s: expected an error", body)
		}
	}
}

func TestConfigLevelRoundTrips(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Level = log.WARN
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var back log.LoggerConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("%v (encoded as %s)", err, data)
	}
	if back.Level != log.WARN {
		t.Errorf("level = %v, want WARN", back.Level)
	}
}

// A configuration error must not leave the log file open behind it.
func TestConfigureLoggerClosesFileWhenFormatterFails(t *testing.T) {
	cfg := log.DefaultConfig()
	cfg.Output = filepath.Join(t.TempDir(), "app.log")
	cfg.Format = "custom" // Custom is nil, so formatter construction fails

	before := openFileCount(t)
	for i := 0; i < 20; i++ {
		if _, err := log.ConfigureLogger(nil, cfg); err == nil {
			t.Fatal("expected an error")
		}
	}
	if leaked := openFileCount(t) - before; leaked > 0 {
		t.Errorf("leaked %d descriptors over 20 failed calls", leaked)
	}
}

func openFileCount(t *testing.T) int {
	t.Helper()
	d, err := os.Open("/dev/fd")
	if err != nil {
		t.Skipf("cannot inspect open descriptors: %v", err)
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		t.Skipf("cannot inspect open descriptors: %v", err)
	}
	return len(names)
}
