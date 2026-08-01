package log

import (
	"bytes"
	"io"
	"sync"
)

// Writer returns an io.WriteCloser that turns each line written to it into an
// entry at INFO. It is how output from code that only knows how to write lines
// gets into the logger:
//
//	stdlog.SetOutput(logger.Writer())                  // the standard library's log
//	srv.ErrorLog = stdlog.New(logger.WriterLevel(log.ERROR), "", 0)
//
// That second line is the one most services need: http.Server.ErrorLog takes a
// *log.Logger, and without an adapter those errors bypass structured logging
// entirely.
//
// Close flushes any trailing text that did not end in a newline. The writer is
// safe for concurrent use.
func (l *Logger) Writer() io.WriteCloser { return l.WriterLevel(INFO) }

// WriterLevel is Writer at a chosen level.
func (l *Logger) WriterLevel(level LogLevel) io.WriteCloser {
	return &lineWriter{logger: l, level: level}
}

// lineWriter splits writes on newlines, because a single Write may carry
// several lines or half of one.
type lineWriter struct {
	logger *Logger
	level  LogLevel

	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline yet: keep the remainder for the next write.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.emit(line[:len(line)-1])
	}
	return len(p), nil
}

// Close emits whatever is left, so a final line without a newline is not lost.
func (w *lineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if rest := w.buf.String(); rest != "" {
		w.buf.Reset()
		w.emit(rest)
	}
	return nil
}

func (w *lineWriter) emit(line string) {
	if line == "" {
		return
	}
	// Trailing \r for callers writing CRLF.
	if line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	w.logger.Log(w.level, line, nil)
}
