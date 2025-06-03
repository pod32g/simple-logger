package main

import (
	"bytes"
	"fmt"

	log "github.com/pod32g/simple-logger"
)

func main() {
	logger := log.ApplyConfig(log.DefaultConfig())

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.Info("first message written to buffer")

	logger.SetLevel(log.DEBUG)
	logger.SetFormatter(&log.JSONFormatter{})
	logger.Debug("second message as JSON")

	fmt.Print(buf.String())
}
