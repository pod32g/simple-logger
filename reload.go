package log

import (
	"context"
	"errors"
	"os"
	"time"
)

// WatchOption configures Watch.
type WatchOption func(*watchOptions)

type watchOptions struct {
	interval time.Duration
	onError  func(error)
}

// WatchInterval sets how often the file is polled. It defaults to one second.
func WatchInterval(d time.Duration) WatchOption {
	return func(o *watchOptions) { o.interval = d }
}

// WatchErrorHandler is called when a reload fails. The watcher keeps running:
// a bad edit should not silently stop configuration from being followed.
func WatchErrorHandler(fn func(error)) WatchOption {
	return func(o *watchOptions) { o.onError = fn }
}

// Watch applies the configuration file at path, then re-applies it whenever the
// file changes, until ctx is done. It returns an error only if the initial load
// fails; later failures go to the error handler and the watch continues.
//
//	go logger.Watch(ctx, "log.json", log.WatchInterval(5*time.Second))
func (l *Logger) Watch(ctx context.Context, path string, opts ...WatchOption) error {
	if l == nil {
		return errors.New("logger is nil")
	}
	o := watchOptions{interval: time.Second}
	for _, opt := range opts {
		opt(&o)
	}
	if o.interval <= 0 {
		o.interval = time.Second
	}

	sig, err := l.loadAndApply(path)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				report(o.onError, err)
				continue
			}
			if next := fileSignatureFromInfo(info); next == sig {
				continue
			}
			s, err := l.loadAndApply(path)
			if err != nil {
				report(o.onError, err)
				continue
			}
			sig = s
		}
	}
}

// ReloadFrom applies every configuration received on configs until the channel
// closes or ctx is done. Failures go to the error handler, if one is given.
func (l *Logger) ReloadFrom(ctx context.Context, configs <-chan Config, opts ...WatchOption) {
	if l == nil {
		return
	}
	var o watchOptions
	for _, opt := range opts {
		opt(&o)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case cfg, ok := <-configs:
			if !ok {
				return
			}
			if err := l.Apply(cfg); err != nil {
				report(o.onError, err)
			}
		}
	}
}

type fileSignature struct {
	modTime time.Time
	size    int64
}

func fileSignatureFromInfo(info os.FileInfo) fileSignature {
	return fileSignature{modTime: info.ModTime(), size: info.Size()}
}

func (l *Logger) loadAndApply(path string) (fileSignature, error) {
	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		return fileSignature{}, err
	}
	if err := l.Apply(cfg); err != nil {
		return fileSignature{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileSignature{}, err
	}
	return fileSignatureFromInfo(info), nil
}

func report(handler func(error), err error) {
	if err != nil && handler != nil {
		handler(err)
	}
}
