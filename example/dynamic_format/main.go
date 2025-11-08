package main

import (
	"fmt"

	log "github.com/pod32g/simple-logger"
)

func main() {
	cfg := log.DefaultConfig()
	logger := log.ApplyConfig(cfg)
	defer logger.Close()

	logger.Info("This is printed in text format")

	cfg.Format = "json"
	if _, err := log.ConfigureLogger(logger, cfg); err != nil {
		fmt.Println("reconfigure failed:", err)
	}
	logger.Info("This is printed in JSON format using ConfigureLogger")
}
