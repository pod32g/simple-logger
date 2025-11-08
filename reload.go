package log

import (
	"context"
	"errors"
	"os"
	"time"
)

// ConfigApplier applies a LoggerConfig. Returning an error aborts the specific
// reload attempt but leaves the reloader running.
type ConfigApplier func(LoggerConfig) error

// ApplyConfigTo returns a ConfigApplier that applies configs to the provided logger.
func ApplyConfigTo(logger *Logger) ConfigApplier {
	return func(cfg LoggerConfig) error {
		if logger == nil {
			return errors.New("logger is nil")
		}
		_, err := ConfigureLogger(logger, cfg)
		return err
	}
}

// WatchConfigFile polls the provided file at the given interval and invokes
// apply whenever the file changes. The watcher stops when the context is done.
func WatchConfigFile(ctx context.Context, path string, interval time.Duration, apply ConfigApplier, onError func(error)) error {
	if apply == nil {
		return errors.New("apply function cannot be nil")
	}
	if interval <= 0 {
		interval = time.Second
	}

	sig, err := loadAndApply(path, apply)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				handleReloadError(err, onError)
				continue
			}
			next := fileSignatureFromInfo(info)
			if next == sig {
				continue
			}

			s, err := loadAndApply(path, apply)
			if err != nil {
				handleReloadError(err, onError)
				continue
			}
			sig = s
		}
	}
}

// WatchConfigFileForLogger wraps WatchConfigFile to automatically update logger.
func WatchConfigFileForLogger(ctx context.Context, logger *Logger, path string, interval time.Duration, onError func(error)) error {
	return WatchConfigFile(ctx, path, interval, ApplyConfigTo(logger), onError)
}

// ReloadFromChannel listens for configurations on the provided channel and
// applies them to the supplied ConfigApplier. The reloader exits when the
// context is done or the channel closes.
func ReloadFromChannel(ctx context.Context, configs <-chan LoggerConfig, apply ConfigApplier, onError func(error)) {
	if apply == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case cfg, ok := <-configs:
			if !ok {
				return
			}
			if err := apply(cfg); err != nil {
				handleReloadError(err, onError)
			}
		}
	}
}

// ReloadLoggerFromChannel is a convenience wrapper that applies configs to logger.
func ReloadLoggerFromChannel(ctx context.Context, logger *Logger, configs <-chan LoggerConfig, onError func(error)) {
	ReloadFromChannel(ctx, configs, ApplyConfigTo(logger), onError)
}

type fileSignature struct {
	modTime time.Time
	size    int64
}

func fileSignatureFromInfo(info os.FileInfo) fileSignature {
	return fileSignature{modTime: info.ModTime(), size: info.Size()}
}

func loadAndApply(path string, apply ConfigApplier) (fileSignature, error) {
	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		return fileSignature{}, err
	}
	if err := apply(cfg); err != nil {
		return fileSignature{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileSignature{}, err
	}
	return fileSignatureFromInfo(info), nil
}

func handleReloadError(err error, handler func(error)) {
	if err != nil && handler != nil {
		handler(err)
	}
}
