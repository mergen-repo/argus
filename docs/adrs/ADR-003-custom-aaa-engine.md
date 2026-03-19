# ADR-003: Custom Go AAA Engine (Not FreeRADIUS)

## Status: Accepted

## Context
Need AAA engine supporting RADIUS + Diameter + 5G SBA for 10M+ SIMs with 10K+ auth/s, integrated policy enforcement, and multi-operator routing.

## Decision
Custom AAA engine written in Go using layeh/radius library as RADIUS protocol foundation. Custom Diameter implementation based on RFC 6733. HTTP/2 proxy for 5G SBA interfaces.

## Alternatives Considered
- **FreeRADIUS as core**: Rejected — no Diameter support, SQL bottleneck at 2K pps, C codebase hard to extend, no native policy engine.
- **FreeRADIUS + custom wrapper**: Rejected — two language worlds, fragile integration points.
- **Radiator (commercial)**: Rejected — Perl-based, license cost, less control over internals.

## Consequences
- Positive: Full control over auth pipeline, can integrate policy evaluation inline
- Positive: Go goroutines handle concurrent RADIUS/Diameter requests efficiently
- Positive: Single language (Go) for entire stack
- Negative: Must implement RADIUS/Diameter protocol handling (mitigated by existing Go libraries)
- Negative: Must handle protocol edge cases ourselves (mitigated by FreeRADIUS dictionary files as reference)
- Risks: Protocol compliance — need thorough testing against real operator equipment
