package slogbridge_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/slogbridge"
)

// A nil Leveler tracks the logger, so a runtime level change reaches slog.
func TestHandlerFollowsLoggerLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))
	slogger := slog.New(slogbridge.NewHandler(logger, nil))

	slogger.Debug("hidden")
	if strings.Contains(buf.String(), "hidden") {
		t.Errorf("debug record emitted at INFO: %s", buf.String())
	}

	logger.SetLevel(log.DEBUG)
	slogger.Debug("now visible")
	if !strings.Contains(buf.String(), "now visible") {
		t.Errorf("debug record still suppressed after SetLevel(DEBUG): %s", buf.String())
	}

	logger.SetLevel(log.ERROR)
	buf.Reset()
	slogger.Warn("suppressed again")
	if strings.Contains(buf.String(), "suppressed again") {
		t.Errorf("warn record emitted at ERROR: %s", buf.String())
	}
}

// An explicit Leveler still gates slog independently of the logger.
func TestHandlerHonoursExplicitLeveler(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.DEBUG)))
	slogger := slog.New(slogbridge.NewHandler(logger, slog.LevelWarn))

	slogger.Info("below the handler's floor")
	if strings.Contains(buf.String(), "below the handler's floor") {
		t.Errorf("explicit leveler was ignored: %s", buf.String())
	}
	slogger.Warn("at the floor")
	if !strings.Contains(buf.String(), "at the floor") {
		t.Errorf("record at the handler's level was dropped: %s", buf.String())
	}
}
