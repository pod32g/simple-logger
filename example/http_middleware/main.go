package main

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	log "github.com/pod32g/simple-logger"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	lrw.status = status
	lrw.ResponseWriter.WriteHeader(status)
}

func (lrw *loggingResponseWriter) Write(p []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(p)
	lrw.bytes += n
	return n, err
}

func requestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lrw := &loggingResponseWriter{ResponseWriter: w}
			next.ServeHTTP(lrw, r)
			duration := time.Since(start)

			logger.Info("handled request",
				log.String("method", r.Method),
				log.String("path", r.URL.Path),
				log.Int("status", lrw.status),
				log.Int("bytes", lrw.bytes),
				log.Any("duration", duration),
			)
		})
	}
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	payload := map[string]string{"message": "hello"}
	_ = json.NewEncoder(w).Encode(payload)
}

func main() {
	logger := log.Must(log.New(log.WithJSON()))
	logger.SetLevel(log.INFO)
	defer logger.Close()

	h := requestLogger(logger)(http.HandlerFunc(helloHandler))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: h,
	}

	logger.Info("starting http server", log.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server exited", log.Err("error", err))
		os.Exit(1)
	}
}
