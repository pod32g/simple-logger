package main

import log "github.com/pod32g/simple-logger"

func main() {
	cfg := log.DefaultConfig()
	logger := log.ApplyConfig(cfg)
	logger.Info("This is printed in text format")

	cfg.UpdateLogFormat("json")
	logger = log.ApplyConfig(cfg)
	logger.Info("This is printed in JSON format")
}
