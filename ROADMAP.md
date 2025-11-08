# Roadmap (not tracked)

- Pluggable field encoders for common structs (time.Duration, net.IP, errors).
- Hook filters (per-level, regex) and aggregation metrics (drop counts, queue sizes).
- OTLP / slog bridges to integrate with observability stacks out of the box.
- Structured log parsing CLI or example for log ingestion pipelines.
- Trace correlation helpers (auto extract trace/span IDs from context).
- Async logging metrics (gauge drop counters, queue length) and health checks.
- More examples: HTTP middleware, gRPC interceptors, structured metrics emission.
- Structured field encoders: registry for time.Duration, net.IP, url.URL, errors with text/JSON support.
- Observability bridges: OTLP exporter and slog adapter for seamless integration.
- Hook filters & metrics: per-level filters, drop counters, async queue length stats.
- Advanced async tuning: batching, backpressure mode, flush interval controls.
- Hot reload helpers: file watchers or channel-based reconfigure utilities.
- Additional examples/CLI: HTTP middleware, gRPC interceptors, runtime config demo tool.
