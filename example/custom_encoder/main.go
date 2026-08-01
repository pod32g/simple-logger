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
// One interface, one method: append to buf, return the extended slice.
type logfmtEncoder struct{}

func (logfmtEncoder) Encode(buf []byte, e log.Entry) []byte {
	buf = append(buf, "ts="...)
	buf = e.Time.AppendFormat(buf, time.RFC3339)
	buf = fmt.Appendf(buf, " level=%s msg=%q", strings.ToLower(e.Level.String()), e.Message)
	for _, f := range e.Fields {
		buf = fmt.Appendf(buf, " %s=%v", f.Key, f.Value)
	}
	return append(buf, '\n')
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
