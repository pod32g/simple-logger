// A tour of the library: each section is a self-contained function showing one
// capability. Run it with `go run ./example`.
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	section("Getting started", helloWorld)
	section("Structured fields", structuredLogging)
	section("Derived loggers", derivedLoggers)
	section("Presets", presets)
	section("Sampling and hooks", samplingAndHooks)
	section("Redaction", redaction)
	section("Asynchronous writing", asyncLogging)
	section("File rotation", rotation)
	section("Custom encoder", customEncoder)
}

func section(title string, fn func()) {
	fmt.Printf("\n== %s ==\n", title)
	fn()
}

// The shortest thing that works: no configuration at all.
func helloWorld() {
	logger := log.Must(log.New())
	defer logger.Close()

	logger.Info("ready")
	logger.Warnf("listening on port %d", 8080)
}

func structuredLogging() {
	logger := log.Must(log.New(log.WithJSON()))
	defer logger.Close()

	logger.Info("user login",
		log.String("user", "alice"),
		log.Int("attempt", 3),
		log.Bool("success", true))
}

// With binds fields to every entry a derived logger emits; Named tags a
// component. Both share the parent's output, level and hooks.
func derivedLoggers() {
	logger := log.Must(log.New())
	defer logger.Close()

	requestLogger := logger.With(log.String("request_id", "req-42")).Named("http")
	requestLogger.Info("handling request")
	requestLogger.Info("request complete", log.String("status", "200"))
}

// Development and Production bundle the settings each context usually wants,
// and compose with any option that follows them.
func presets() {
	dev := log.Must(log.New(log.Development(), log.WithLevel(log.INFO)))
	defer dev.Close()
	dev.Info("development: console encoding, color, call sites")

	prod := log.Must(log.New(log.Production(), log.WithOutput(os.Stdout)))
	defer prod.Close()
	prod.Info("production: JSON, async, stacktraces on errors")
}

func samplingAndHooks() {
	logger := log.Must(log.New(
		log.WithSampler(log.NewEveryNSampler(3)),
		log.WithHook(log.HookFunc(func(level log.LogLevel, message string, fields []log.Field) {
			fmt.Printf("   hook saw %s %q with %d fields\n", level, message, len(fields))
		}))))
	defer logger.Close()

	for i := 1; i <= 6; i++ {
		logger.Info("heartbeat", log.Int("iteration", i))
	}
}

// Redactors run before formatting and before hooks, so a secret reaches
// neither.
func redaction() {
	logger := log.Must(log.New(
		log.WithJSON(),
		log.WithRedactor(log.NewKeyRedactor("password", "token"))))
	defer logger.Close()

	logger.Info("credentials received",
		log.String("user", "alice"),
		log.String("password", "hunter2"))

	scrubbed := log.Must(log.New(
		log.WithRedactor(log.NewPatternScrubber(regexp.MustCompile(`Bearer \S+`)))))
	defer scrubbed.Close()
	scrubbed.Info("upstream rejected Bearer abc123.def")
}

// The async writer keeps the logging goroutine off the sink. Close drains it.
func asyncLogging() {
	logger := log.Must(log.New(
		log.WithAsyncQueue(256),
		log.WithAsyncBatch(16, 10*time.Millisecond),
		log.WithAsyncDropOldest()))

	for i := 0; i < 3; i++ {
		logger.Info("queued", log.Int("i", i))
	}
	logger.Flush()
	fmt.Printf("   queue stats: %+v\n", logger.AsyncStats())
	logger.Close()
}

func rotation() {
	path := "example-rotation.log"
	logger := log.Must(log.New(
		log.WithJSON(),
		log.WithRotatingFile(path, log.Rotation{MaxSizeMB: 1, MaxBackups: 3})))

	logger.Info("written to a rotating file", log.String("path", path))
	logger.Close()

	if err := os.Remove(path); err == nil {
		fmt.Println("   wrote and cleaned up", path)
	}
}

// An encoder is one interface with one method.
type bracketEncoder struct{}

func (bracketEncoder) Encode(buf []byte, e log.Entry) []byte {
	return fmt.Appendf(buf, "<%s> %s\n", e.Level, e.Message)
}

func customEncoder() {
	logger := log.Must(log.New(log.WithEncoder(bracketEncoder{})))
	defer logger.Close()

	logger.Info("rendered by a custom encoder")
	logger.InfoContext(context.Background(), "context-aware too")
}
