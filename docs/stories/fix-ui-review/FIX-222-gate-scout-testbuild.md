# FIX-222 — Scout Test/Build Findings

Gate: FIX-222 Operator/APN Detail Polish
Role: TestBuild (tsc, vite build, go build/vet/test, lint sweeps)

## Commands Run
- `cd web && npx tsc --noEmit`
- `cd web && npm run build`
- `go build ./...`
- `go vet ./...`
- `go test ./...`
- UI enforcement greps (hex colors, raw `<button>`, arbitrary px in new files)

<SCOUT-TESTBUILD-FINDINGS>

F-B1 | PASS | TypeScript type check
- Command: `npx tsc --noEmit`
- Result: PASS — 0 errors across all web/src files.

F-B2 | PASS | Vite production build
- Command: `npm run build`
- Result: PASS — built in ~2.5s, 0 errors, 0 warnings. Largest bundles: vendor-charts (411kB), index (409kB), vendor-codemirror (346kB). No regressions vs prior baseline.

F-B3 | PASS | Go compile + vet
- `go build ./...`: PASS (0 errors).
- `go vet ./...`: PASS (0 findings).

F-B4 | PASS | Go test suite
- Command: `go test ./...`
- Result: 3531 tests passed across 109 packages. No regressions from FIX-222 (no Go files were modified in this story).

F-B5 | PASS | UI enforcement greps
- Hex colors in FIX-222 files: 0
- Raw `<button>` in FIX-222 files: 0
- `text-[Npx]`/`max-w-[Npx]` hits in new files: present but consistent with project-wide typography convention (kpi-card.tsx was extracted as-is from dashboard; project tolerates `text-[10/11/12/28px]` arbitrary values). Not a violation under current FRONTEND.md spec.

F-B6 | PASS | Unit test files exist
- `web/src/components/ui/__tests__/info-tooltip.test.tsx` (1.6K): type-level smoke tests validating all 9 glossary terms are non-empty strings.
- `web/src/hooks/__tests__/use-tab-url-sync.test.tsx` (2.8K): type-level tests validating alias targets resolve to valid tabs; no alias chains introduced.
- Note: Project has no vitest runner configured — `tsc --noEmit` is the test runner per developer note in step log (STEP_2 W3 T8). These files compile cleanly as part of the tsc sweep.

F-B7 | PASS | Post-fix re-verification (after F-A1 fix)
- After adding isError/refetch to EsimProfilesTab: tsc PASS, vite build PASS (2.49s).

</SCOUT-TESTBUILD-FINDINGS>
