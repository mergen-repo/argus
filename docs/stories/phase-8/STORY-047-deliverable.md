# Deliverable: STORY-047 — Frontend Monitoring Pages

## Summary

4 monitoring pages: Live Sessions (real-time WS), Jobs (progress bars + WS), eSIM Profiles (actions), Audit Log (search + JSON diff + hash chain verify). 8 new type/hook files + 4 full page implementations.

## Files Changed
| File | Purpose |
|------|---------|
| `web/src/types/session.ts, job.ts, esim.ts, audit.ts` | TypeScript types |
| `web/src/hooks/use-sessions.ts, use-jobs.ts, use-esim.ts, use-audit.ts` | TanStack Query + WS hooks |
| `web/src/pages/sessions/index.tsx` | Live Sessions page |
| `web/src/pages/jobs/index.tsx` | Jobs page |
| `web/src/pages/esim/index.tsx` | eSIM Profiles page |
| `web/src/pages/audit/index.tsx` | Audit Log page |

## Test Coverage
- TypeScript strict, npm run build clean, 18/18 ACs verified
