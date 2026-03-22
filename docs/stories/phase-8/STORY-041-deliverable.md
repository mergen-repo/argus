# Deliverable: STORY-041 — React + Vite + Tailwind + shadcn/ui Scaffold

## Summary

Complete React frontend scaffold with Vite, Tailwind CSS v4, shadcn/ui components, React Router v6, Zustand stores, TanStack Query, Axios API client with JWT refresh, WebSocket client with reconnect, and command palette. Dark-first design with all FRONTEND.md tokens. 136KB gzipped bundle.

## Key Files
| Path | Purpose |
|------|---------|
| `web/src/index.css` | 40+ FRONTEND.md design tokens via @theme |
| `web/src/router.tsx` | 26 routes from SCREENS.md |
| `web/src/components/layout/DashboardLayout.tsx` | Collapsible sidebar + topbar |
| `web/src/components/layout/AuthLayout.tsx` | Centered card with logo |
| `web/src/components/ui/` | 13 shadcn/ui components |
| `web/src/stores/auth.ts` | Zustand auth store |
| `web/src/stores/ui.ts` | Zustand UI store |
| `web/src/lib/api.ts` | Axios + JWT + refresh |
| `web/src/lib/ws.ts` | WebSocket + reconnect |
| `web/src/lib/query.ts` | TanStack Query config |
| `web/src/components/command-palette/` | Ctrl+K search |
| `web/src/pages/` | 21 placeholder pages |

## Test Coverage
- npm run build: 136KB gzipped (under 500KB budget)
- npm run dev: starts in ~114ms with HMR
- go build ./...: backend unaffected
- All 17 ACs verified
