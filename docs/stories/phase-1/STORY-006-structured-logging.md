# STORY-006: Structured Logging, Config & NATS Event Bus

## User Story
As a developer, I want structured JSON logging, centralized config management, and NATS event bus wired up, so that all services have shared infrastructure.

## Description
Set up zerolog for structured logging with correlation IDs, envconfig for configuration, and NATS JetStream client with topic definitions for all event types.

## Architecture Reference
- Services: All SVC-01 to SVC-10 (shared infra)
- Packages: internal/config, internal/bus, internal/gateway (correlation ID middleware)
- Source: docs/ARCHITECTURE.md (Technology Stack)
- Spec: docs/architecture/CONFIG.md (env vars), docs/architecture/MIDDLEWARE.md (correlation ID)

## Screen Reference
- None (infrastructure)

## Acceptance Criteria
- [ ] All log entries are JSON with: timestamp, level, correlation_id, service, message, fields
- [ ] Correlation ID generated per HTTP request, propagated through context
- [ ] Log levels configurable per package via LOG_LEVEL env var
- [ ] Config struct loaded from .env via envconfig with validation
- [ ] NATS JetStream connection established on startup
- [ ] NATS streams created: EVENTS (session.*, sim.*, operator.*, policy.*, alert.*, job.*, notification.*), JOBS (job queue)
- [ ] Bus package provides Publish(topic, payload) and Subscribe(topic, handler) abstractions
- [ ] Redis client initialized with connection pooling
- [ ] Graceful shutdown: close NATS, Redis, DB connections on SIGTERM/SIGINT

## Dependencies
- Blocked by: STORY-001 (Docker infra)
- Blocks: STORY-007 (audit needs event bus)

## Test Scenarios
- [ ] Log output is valid JSON parseable
- [ ] Correlation ID appears in all log entries for a single request
- [ ] Config validation fails on missing required env vars
- [ ] NATS publish/subscribe roundtrip works
- [ ] Graceful shutdown completes within 5s

## Effort Estimate
- Size: M
- Complexity: Medium
