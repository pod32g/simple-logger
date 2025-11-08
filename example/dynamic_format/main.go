package main

import (
	"context"
	"fmt"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	cfg := log.DefaultConfig()
	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("This is printed in text format")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configs := make(chan log.LoggerConfig, 1)
	go log.ReloadLoggerFromChannel(ctx, logger, configs, func(err error) {
		fmt.Println("reconfigure failed:", err)
	})

	cfg.Format = "json"
	configs <- cfg
	close(configs)

	time.Sleep(20 * time.Millisecond)
	logger.Info("This is printed in JSON format using ReloadLoggerFromChannel")
}
