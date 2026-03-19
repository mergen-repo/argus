# STORY-052: AAA Performance Tuning & Benchmark Suite

## User Story
As a platform operator, I want the AAA engine to meet strict latency budgets (p50<5ms, p95<20ms, p99<50ms) at 10K+ auth/s throughput, so that the platform can handle production-scale IoT traffic.

## Description
Performance optimization of the AAA path: Redis-first session lookup (avoid DB on hot path), in-memory policy cache (compiled rules cached in Go process, invalidated via NATS), connection pooling, zero-allocation packet parsing where possible. Benchmark suite using Go testing.B and custom load generator. Profile and optimize based on pprof results.

## Architecture Reference
- Services: SVC-04 (AAA Engine), SVC-05 (Policy Engine), SVC-06 (Operator Router)
- Packages: internal/aaa, internal/policy, internal/cache, internal/protocol/radius
- Source: docs/architecture/services/_index.md (SVC-04, SVC-05)

## Screen Reference
- SCR-120: System Health — latency percentiles display, auth/s gauge

## Acceptance Criteria
- [ ] Latency budget: p50 < 5ms, p95 < 20ms, p99 < 50ms for RADIUS Access-Request → Accept
- [ ] Throughput: sustain 10K+ authentications per second on single node
- [ ] Redis-first session lookup: RADIUS auth checks Redis before DB (cache hit ratio > 95%)
- [ ] In-memory policy cache: compiled rules loaded into Go process memory
- [ ] Policy cache invalidation: NATS "policy.updated" event triggers cache refresh
- [ ] Policy cache warm-up: pre-load all active policy versions on startup
- [ ] SIM lookup: Redis cache with configurable TTL (default 5min), LRU eviction
- [ ] Connection pooling: Redis pool (min 10, max 100), PG pool via PgBouncer
- [ ] Zero-allocation RADIUS parsing: use byte slices, avoid string conversion on hot path
- [ ] Benchmark suite: Go benchmark tests for auth path, accounting path, policy evaluation
- [ ] Load generator: simulate N concurrent RADIUS clients with configurable auth rate
- [ ] pprof integration: CPU and memory profiling endpoints enabled in dev mode
- [ ] Benchmark results documented: baseline numbers, improvement deltas
- [ ] No goroutine leaks: verify with goleak in tests
- [ ] GC tuning: GOGC configured for low-latency (documented trade-offs)

## Dependencies
- Blocked by: STORY-015 (RADIUS server), STORY-022 (policy evaluator), STORY-017 (session management)
- Blocks: None (optimization story)

## Test Scenarios
- [ ] Benchmark: 10K auth/s sustained for 60s → no errors, latency within budget
- [ ] Benchmark: p99 latency < 50ms under 10K auth/s load
- [ ] Redis cache hit ratio > 95% under sustained load
- [ ] Policy cache invalidation: update policy → next auth uses new rules (< 1s propagation)
- [ ] Connection pool: 10K concurrent connections → no pool exhaustion errors
- [ ] Memory profiling: no significant growth after 1M authentications (no leaks)
- [ ] CPU profiling: hot path identified and optimized
- [ ] Goleak test: no goroutine leaks after load test

## Effort Estimate
- Size: L
- Complexity: High
