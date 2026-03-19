# ADR-001: Go Modular Monolith Architecture

## Status: Accepted

## Context
Argus has 10 logical services (API Gateway, AAA Engine, Policy Engine, etc.). Need to decide between microservices and monolith for a solo developer + Claude Code team.

## Decision
Go modular monolith — single binary with 10 internal packages. Multiple protocol listeners (RADIUS :1812, Diameter :3868, HTTP :8080, WebSocket :8081) run as goroutines in the same process.

## Alternatives Considered
- **Microservices**: Each SVC-NN as separate container. Rejected — too much operational overhead for solo dev (10 Dockerfiles, service discovery, inter-service auth, distributed debugging).
- **FreeRADIUS + Go wrapper**: Rejected in Discovery phase — two language worlds, no Diameter, config complexity.

## Consequences
- Positive: Single deployment, simple debugging, shared memory for caches, no inter-service latency
- Positive: Package boundaries enforce separation — can split to microservices later
- Negative: Can't scale AAA engine independently from API (mitigated by horizontal scaling of entire binary)
- Risks: Memory leak in one package affects entire process (mitigated by Go's GC and structured error handling)
