# Deliverable: STORY-043 — Frontend Main Dashboard

## Summary

Full-stack dashboard: backend API aggregating SIM counts, sessions, operator health, alerts + React frontend with 4 metric cards (sparklines), SIM distribution pie, operator health bars, APN traffic bars, live alert feed. Real-time via WebSocket (auth/s, alerts). Recharts for charts, TanStack Query with 30s auto-refresh.

## Files Changed

### Backend
| File | Purpose |
|------|---------|
| `internal/store/sim.go` | CountByState method |
| `internal/api/dashboard/handler.go` | GET /api/v1/dashboard aggregation |
| `internal/gateway/router.go` | Dashboard route |
| `cmd/argus/main.go` | Wiring |

### Frontend
| File | Purpose |
|------|---------|
| `web/src/types/dashboard.ts` | TypeScript types |
| `web/src/hooks/use-dashboard.ts` | TanStack Query + WS hooks |
| `web/src/pages/dashboard/index.tsx` | Full dashboard page |

## Test Coverage
- Go build + vet clean, TypeScript clean
- npm run build succeeds
- All 13 ACs verified
