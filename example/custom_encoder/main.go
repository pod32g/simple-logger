// Demonstrates supplying your own encoder. One interface, one method: the
// logger hands you the level, the message and the fields, and takes whatever
// string you return.
package main

import (
	"fmt"
	"strings"
	"time"

	log "github.com/pod32g/simple-logger"
)

// logfmtEncoder renders entries in the logfmt style used by Heroku and friends.
type logfmtEncoder struct{}

func (logfmtEncoder) Format(level log.LogLevel, message string) string {
	return logfmtEncoder{}.FormatWithFields(level, message, nil)
}

func (logfmtEncoder) FormatWithFields(level log.LogLevel, message string, fields []log.Field) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ts=%s level=%s msg=%q",
		time.Now().Format(time.RFC3339), strings.ToLower(level.String()), message)
	for _, f := range fields {
		fmt.Fprintf(&b, " %s=%v", f.Key, f.Value)
	}
	b.WriteByte('\n')
	return b.String()
}

func main() {
	logger, err := log.New(log.WithEncoder(logfmtEncoder{}))
	if err != nil {
		panic(err)
	}
	defer logger.Close()

	logger.Info("user action",
		log.Int("user_id", 7),
		log.String("action", "checkout"))
	logger.Warn("slow response", log.String("route", "/api/orders"))
}
