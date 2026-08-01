package logtest_test

import (
	"testing"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/logtest"
)

func TestObserverRecordsEntries(t *testing.T) {
	obs, logger := logtest.New(log.DEBUG)

	logger.Debug("cache miss", log.String("key", "user:1"))
	logger.Info("served", log.Int("status", 200))
	logger.Debug("cache miss", log.String("key", "user:2"))

	if got := obs.Len(); got != 3 {
		t.Fatalf("recorded %d entries, want 3", got)
	}
	if got := obs.FilterMessage("cache miss").Len(); got != 2 {
		t.Errorf("expected two cache misses, got %d", got)
	}
	if got := obs.FilterLevel(log.INFO).Len(); got != 1 {
		t.Errorf("expected one INFO entry, got %d", got)
	}
	if got := obs.FilterField("key", "user:2").Len(); got != 1 {
		t.Errorf("expected one entry for user:2, got %d", got)
	}
}

func TestObserverReadsGroupedFields(t *testing.T) {
	obs, logger := logtest.New(log.INFO)

	logger.Info("request", log.Group("http", log.Int("status", 500)))

	entry := obs.AssertLogged(t, "request")
	got, ok := entry.Field("http.status")
	if !ok || got != 500 {
		t.Errorf("http.status = %v (present=%v), want 500", got, ok)
	}
	if _, ok := entry.Field("http.missing"); ok {
		t.Error("expected a missing nested key to report absent")
	}
}

// The observer sees what hooks see: post-redaction.
func TestObserverSeesRedactedValues(t *testing.T) {
	logger := log.Must(log.New(log.WithLevel(log.INFO),
		log.WithRedactor(log.NewKeyRedactor("password"))))
	obs := logtest.Attach(logger)

	logger.Info("login", log.String("password", "hunter2"))

	entry := obs.AssertLogged(t, "login")
	if got, _ := entry.Field("password"); got == "hunter2" {
		t.Error("observer saw the unredacted secret")
	}
}

func TestTakeAllClears(t *testing.T) {
	obs, logger := logtest.New(log.INFO)
	logger.Info("one")
	if len(obs.TakeAll()) != 1 {
		t.Fatal("expected one entry")
	}
	if obs.Len() != 0 {
		t.Error("TakeAll should clear the record")
	}
}
