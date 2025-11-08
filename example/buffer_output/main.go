package main

import (
	"bytes"
	"fmt"
	"os"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	logger := log.ApplyConfig(log.DefaultConfig())
	defer logger.Close()

	var buf bytes.Buffer
	logger.SetOutputs(os.Stdout, &buf)

	logger.Info("written to stdout and buffer")
	logger.EnableAsync(log.AsyncOptions{
		QueueSize:     8,
		DropStrategy:  log.DropNew,
		BatchSize:     4,
		FlushInterval: 5 * time.Millisecond,
	})
	logger.InfoString("buffer-only message")
	logger.DisableAsync()

	fmt.Println("captured buffer:")
	fmt.Print(buf.String())
}
