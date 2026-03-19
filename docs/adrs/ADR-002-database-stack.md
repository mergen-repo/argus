# ADR-002: PostgreSQL + TimescaleDB + Redis + NATS Data Stack

## Status: Accepted

## Context
Need to support: 10M+ SIM OLTP, time-series analytics (CDRs, metrics), sub-ms session lookups, async job processing, and real-time events.

## Decision
- PostgreSQL 16: Primary relational store (tenants, users, SIMs, APNs, policies)
- TimescaleDB extension: Time-series for sessions, CDRs, operator health (same PG cluster)
- Redis 7: Session cache, policy cache, rate limiting counters
- NATS JetStream: Event bus, job queue, cache invalidation, real-time WebSocket feed

## Alternatives Considered
- **ClickHouse for analytics**: Rejected — separate database engine adds operational complexity. TimescaleDB handles our analytics volume within PG.
- **Kafka for events**: Rejected — NATS is simpler to operate, Go-native, sufficient for our event patterns. Kafka's partition management is overkill.
- **MongoDB for SIMs**: Rejected — relational integrity critical for SIM↔APN↔Policy↔Operator relationships. PG with JSONB gives flexibility where needed.

## Consequences
- Positive: Single PG cluster for OLTP + analytics (TimescaleDB continuous aggregates)
- Positive: Redis sub-ms latency on AAA hot path
- Positive: NATS lightweight, single binary, perfect for Go
- Negative: TimescaleDB compression/retention needs careful tuning
- Risks: PG as single DB engine — if analytics volume exceeds TimescaleDB capacity, may need ClickHouse later
