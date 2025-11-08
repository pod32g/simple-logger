package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	log "github.com/pod32g/simple-logger"
)

func main() {
	configPath := flag.String("config", "", "optional path to logger JSON config")
	flag.Parse()

	logger := log.ApplyConfig(log.DefaultConfig())
	defer logger.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if *configPath != "" {
		go func() {
			if err := log.WatchConfigFileForLogger(ctx, logger, *configPath, 2*time.Second, func(err error) {
				logger.Warn("config reload failed", log.Error("error", err))
			}); err != nil {
				logger.Error("watcher exited", log.Error("error", err))
			}
		}()
	}

	updates := make(chan log.LoggerConfig, 1)
	go log.ReloadLoggerFromChannel(ctx, logger, updates, func(err error) {
		logger.Warn("apply config failed", log.Error("error", err))
	})

	go promptLoop(ctx, logger, updates)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return
		case <-ticker.C:
			logger.Info("still running", log.Any("ts", time.Now()))
		}
	}
}

func promptLoop(ctx context.Context, logger *log.Logger, updates chan<- log.LoggerConfig) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("type commands like 'level debug' or 'format json'. ctrl+c to exit")

	cfg := log.DefaultConfig()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			fmt.Println("usage: level <debug|info|warn|error|fatal> | format <text|json>")
			continue
		}

		key, value := strings.ToLower(parts[0]), strings.ToLower(parts[1])
		updated := false
		switch key {
		case "level":
			switch value {
			case "debug":
				cfg.Level = log.DEBUG
			case "info":
				cfg.Level = log.INFO
			case "warn":
				cfg.Level = log.WARN
			case "error":
				cfg.Level = log.ERROR
			case "fatal":
				cfg.Level = log.FATAL
			default:
				fmt.Println("unknown level", value)
				continue
			}
			updated = true
		case "format":
			switch value {
			case "text", "json":
				cfg.Format = value
			default:
				fmt.Println("unknown format", value)
				continue
			}
			updated = true
		default:
			fmt.Println("unknown command", key)
		}

		if updated {
			select {
			case updates <- cfg:
			case <-ctx.Done():
				return
			}
			logger.Info("applied configuration", log.String("level", levelName(cfg.Level)), log.String("format", cfg.Format))
		}
	}
}

func levelName(level log.LogLevel) string {
	switch level {
	case log.DEBUG:
		return "debug"
	case log.INFO:
		return "info"
	case log.WARN:
		return "warn"
	case log.ERROR:
		return "error"
	case log.FATAL:
		return "fatal"
	default:
		return fmt.Sprintf("%d", level)
	}
}
