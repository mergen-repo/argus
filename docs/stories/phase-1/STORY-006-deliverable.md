# Deliverable: STORY-006 — Structured Logging, Config & NATS Event Bus

**Date:** 2026-03-20
**Status:** Complete

## Summary

Implemented shared infrastructure: zerolog structured JSON logging with correlation IDs, config validation, NATS JetStream event bus with Publish/Subscribe abstractions, and graceful shutdown.

## New Files

| File | Purpose |
|------|---------|
| `internal/gateway/correlation.go` | CorrelationID middleware + GetCorrelationID helper |
| `internal/gateway/logging.go` | ZerologRequestLogger + RecoveryWithZerolog middleware |
| `internal/config/config_test.go` | 11 tests for config validation |
| `internal/gateway/correlation_test.go` | 4 tests for correlation ID |
| `internal/gateway/middleware_test.go` | 6 tests for middleware chain |
| `internal/bus/bus_test.go` | 4 tests for bus constants and serialization |

## Modified Files

| File | Change |
|------|--------|
| `cmd/argus/main.go` | Logger setup with service field, EnsureStreams call, graceful shutdown ordering |
| `internal/apierr/apierr.go` | Added CorrelationIDKey context key |
| `internal/bus/nats.go` | Added zerolog logger, EnsureStreams for EVENTS/JOBS streams, JetStream publish |
| `internal/config/config.go` | Added Validate() method with semantic rules |
| `internal/gateway/router.go` | Replaced chi middleware with custom zerolog-based middleware |
| `internal/job/runner.go` | Updated QueueSubscribe call to match new signature |

## Key Features

- **Structured Logging**: JSON output with timestamp, level, correlation_id, service, message, fields
- **Correlation ID**: Generated per HTTP request, propagated via context
- **Config Validation**: Semantic rules (JWT secret length, port ranges, duration formats)
- **NATS Streams**: EVENTS (session/sim/operator/policy/alert/job/notification), JOBS (job queue)
- **Graceful Shutdown**: Ordered cleanup on SIGTERM/SIGINT (NATS → Redis → DB)

## Test Results

- 36 new tests, all passing
- Full suite (24 packages) passing, no regressions
- Gate: PASS (0 fixes needed)
