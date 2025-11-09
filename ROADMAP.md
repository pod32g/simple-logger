# Roadmap (not tracked)

- Trace correlation helpers (auto extract trace/span IDs from context).
- Extended telemetry bridges (OpenTelemetry metrics, Prometheus exporter).
- Structured log ingestion CLI for replaying logs into sinks and validating schemas.
- Advanced hook pipelines: conditional routing, batching, retry/backoff for exporters.
- Async observability: Prometheus metrics, health endpoints, and configurable alerts.
- Persistent buffer / disk spill support for bursty workloads.
- Expanded fuzz targets covering config parsing and formatter edge cases.
- Integration harness for running examples in CI (HTTP, gRPC, CLI smoke tests).
