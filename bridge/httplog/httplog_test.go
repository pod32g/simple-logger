package httplog_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/httplog"
)

func TestLevelHandlerGetAndSet(t *testing.T) {
	logger := log.NewLogger(httptest.NewRecorder(), log.INFO, &log.DefaultFormatter{})
	h := httplog.LevelHandler(logger)

	// GET reports the current level.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/loglevel", nil))
	if !strings.Contains(rec.Body.String(), `"info"`) {
		t.Fatalf("GET level = %q", rec.Body.String())
	}

	// PUT updates it.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/loglevel?level=debug", nil))
	if rec.Code != http.StatusOK || logger.Level() != log.DEBUG {
		t.Fatalf("PUT did not set level: code=%d level=%v", rec.Code, logger.Level())
	}

	// Invalid level is rejected.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/loglevel?level=bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad level, got %d", rec.Code)
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	mw := httplog.RequestIDMiddleware("")

	var seen string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = log.RequestID(r.Context())
	}))

	// Generates an ID when absent and echoes it back.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if seen == "" || rec.Header().Get("X-Request-Id") != seen {
		t.Fatalf("expected generated request id in context and header, ctx=%q header=%q", seen, rec.Header().Get("X-Request-Id"))
	}

	// Propagates an incoming ID.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "incoming-123")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "incoming-123" {
		t.Fatalf("expected incoming request id propagated, got %q", seen)
	}
}
