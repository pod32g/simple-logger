// Package httplog provides net/http helpers for simple-logger: a runtime
// log-level admin handler and request-ID propagation middleware. It lives in a
// subpackage so the core logger does not depend on net/http.
package httplog

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	log "github.com/pod32g/simple-logger"
)

// LevelHandler returns an http.Handler to inspect and change a logger's level at
// runtime. GET returns {"level":"info"}; PUT or POST with a "level" query value
// or a JSON body {"level":"debug"} updates it. Mount it on an admin mux, e.g.
// mux.Handle("/loglevel", httplog.LevelHandler(logger)).
func LevelHandler(logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeLevel(w, logger)
		case http.MethodPut, http.MethodPost:
			name := r.URL.Query().Get("level")
			if name == "" {
				var body struct {
					Level string `json:"level"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				name = body.Level
			}
			lvl, err := log.ParseLevel(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			logger.SetLevel(lvl)
			writeLevel(w, logger)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func writeLevel(w http.ResponseWriter, logger *log.Logger) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"level": strings.ToLower(logger.Level().String())})
}

// RequestIDMiddleware returns middleware that ensures each request carries a
// correlation ID: it reads the ID from the given header (default
// "X-Request-Id"), generating one when absent, echoes it back on the response,
// and stores it in the request context via log.WithRequestID so *Context log
// calls emit request_id automatically.
func RequestIDMiddleware(header string) func(http.Handler) http.Handler {
	if header == "" {
		header = "X-Request-Id"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(header)
			if id == "" {
				id = newID()
			}
			w.Header().Set(header, id)
			next.ServeHTTP(w, r.WithContext(log.WithRequestID(r.Context(), id)))
		})
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
