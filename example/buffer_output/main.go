// Demonstrates writing the same entries to several destinations, and batching
// them through the async writer.
package main

import (
	"bytes"
	"fmt"
	"os"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	var buf bytes.Buffer

	logger, err := log.New(
		log.WithOutputs(os.Stdout, &buf),
		log.WithAsyncQueue(8),
		log.WithAsyncBatch(4, 5*time.Millisecond),
	)
	if err != nil {
		panic(err)
	}

	logger.Info("written to stdout and to the buffer")
	logger.Info("batched behind the async writer", log.Int("batch", 1))

	// Close drains the queue before returning, so the buffer is complete below.
	logger.Close()

	fmt.Println("captured buffer:")
	fmt.Print(buf.String())
}
