package log_test

import (
	"bytes"
	stdlog "log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestWriterTurnsLinesIntoEntries(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	w := logger.Writer()
	defer w.Close()

	// One Write carrying several lines, then a line split across two writes.
	w.Write([]byte("first\nsecond\n"))
	w.Write([]byte("thi"))
	w.Write([]byte("rd\n"))

	out := buf.String()
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if n := strings.Count(strings.TrimRight(out, "\n"), "\n"); n != 2 {
		t.Errorf("expected three entries, got %d newlines in %q", n, out)
	}
}

// A trailing line without a newline must not be lost.
func TestWriterCloseFlushesPartialLine(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	w := logger.Writer()
	w.Write([]byte("no trailing newline"))
	if strings.Contains(buf.String(), "no trailing newline") {
		t.Fatal("partial line should not be emitted before Close")
	}
	w.Close()
	if !strings.Contains(buf.String(), "no trailing newline") {
		t.Errorf("Close did not flush the partial line: %q", buf.String())
	}
}

func TestWriterLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	w := logger.WriterLevel(log.ERROR)
	defer w.Close()
	w.Write([]byte("upstream failed\n"))

	if !strings.Contains(buf.String(), "[ERROR] upstream failed") {
		t.Errorf("expected an ERROR entry, got %q", buf.String())
	}
}

// The case this exists for: net/http writes its errors to a *log.Logger.
func TestWriterCapturesStdlibAndHTTPServer(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Must(log.New(log.WithOutput(&buf), log.WithLevel(log.INFO)))

	w := logger.WriterLevel(log.ERROR)
	defer w.Close()

	stdlogger := stdlog.New(w, "", 0)
	stdlogger.Print("legacy line")

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Config.ErrorLog = stdlogger
	srv.Start()
	srv.Close()

	if !strings.Contains(buf.String(), "legacy line") {
		t.Errorf("stdlib log output was not captured: %q", buf.String())
	}
}
