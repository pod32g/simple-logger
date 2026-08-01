// Demonstrates reconfiguring a running logger from a channel of configs.
package main

import (
	"context"
	"fmt"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	logger, err := log.New()
	if err != nil {
		panic(err)
	}
	defer logger.Close()

	logger.Info("this line is text")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configs := make(chan log.Config, 1)
	go logger.ReloadFrom(ctx, configs, log.WatchErrorHandler(func(err error) {
		fmt.Println("reconfigure failed:", err)
	}))

	cfg := log.DefaultConfig()
	cfg.Format = log.FormatJSON
	configs <- cfg
	close(configs)

	time.Sleep(20 * time.Millisecond)
	logger.Info("this line is JSON", log.String("switched", "at runtime"))
}
