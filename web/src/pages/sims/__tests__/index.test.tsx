/**
 * Type-check smoke tests for SimListPage — FIX-233 (AC-7, AC-8, AC-9).
 *
 * NOTE: Full vitest + @testing-library/react rendering tests are NOT configured
 * in this project (no vitest config, no @testing-library/react dependency).
 * These tests validate TypeScript types and import contracts via `tsc --noEmit`,
 * following the same pattern as web/src/__tests__/admin.smoke.test.tsx and
 * web/src/components/policy/__tests__/rollout-active-panel.test.tsx (FIX-232).
 *
 * When vitest + RTL are added, replace this file with rendering tests:
 *
 * SCENARIO 1 — URL ingestion (AC-7):
 *   Mount <SimListPage /> with MemoryRouter initialEntries=['/sims?rollout_id=11111111-...&rollout_stage_pct=1'].
 *   Assert: useSIMList called with filters containing rollout_id + rollout_stage_pct=1.
 *   Assert: screen contains chip text "Cohort:" + "stage 1%".
 *
 * SCENARIO 2 — WS refetch on matching event (AC-8):
 *   Set filters.rollout_id = X via URL. Capture the wsClient.on('policy.rollout_progress', cb) callback.
 *   Fire cb({ rollout_id: X, ... }).
 *   Assert: queryClient.invalidateQueries({ queryKey: ['sims'] }) was called once.
 *
 * SCENARIO 3 — WS NO refetch on non-matching event (AC-8):
 *   Set filters.rollout_id = X via URL. Fire wsClient event with rollout_id = Y (different).
 *   Assert: queryClient.invalidateQueries({ queryKey: ['sims'] }) was NOT called.
 *
 * SCENARIO 4 — Policy chip click → URL update (AC-9):
 *   Mock usePolicyList to return one policy { id: 'POLICY_ID', name: 'Demo Premium', ... }.
 *   Click the "Policy" filter chip trigger, then click "Demo Premium" in dropdown.
 *   Assert: URL ?policy_id=POLICY_ID is set (useSearchParams updated).
 */

import SimListPage from '@/pages/sims'
import type { SIMListFilters } from '@/types/sim'
import type { RolloutSummary, PolicyListItem } from '@/types/policy'

const ROLLOUT_ID = '11111111-1111-1111-1111-111111111111'
const POLICY_ID = '22222222-2222-2222-2222-222222222222'
const POLICY_VERSION_ID = '33333333-3333-3333-3333-333333333333'

const _scenario1Filters: SIMListFilters = {
  rollout_id: ROLLOUT_ID,
  rollout_stage_pct: 1,
}

const _scenario2MatchingEvent: { rollout_id: string } = {
  rollout_id: ROLLOUT_ID,
}

const _scenario3NonMatchingEvent: { rollout_id: string } = {
  rollout_id: 'ffffffff-ffff-ffff-ffff-ffffffffffff',
}

const _scenario4Policy: PolicyListItem = {
  id: POLICY_ID,
  name: 'Demo Premium',
  description: 'Demo policy for type check',
  scope: 'tenant',
  sim_count: 100,
  current_version_id: POLICY_VERSION_ID,
  state: 'active',
  updated_at: '2026-01-01T00:00:00Z',
}

const _scenario4RolloutSummary: RolloutSummary = {
  id: ROLLOUT_ID,
  policy_id: POLICY_ID,
  policy_version_id: POLICY_VERSION_ID,
  policy_name: 'Demo Premium',
  policy_version_number: 2,
  state: 'in_progress',
  current_stage: 50,
  started_at: '2026-01-01T00:00:00Z',
  total_sims: 1000,
  migrated_sims: 500,
  created_at: '2026-01-01T00:00:00Z',
}

const _scenario4ExpectedParams = new URLSearchParams({ policy_id: POLICY_ID })

void [
  SimListPage,
  _scenario1Filters,
  _scenario2MatchingEvent,
  _scenario3NonMatchingEvent,
  _scenario4Policy,
  _scenario4RolloutSummary,
  _scenario4ExpectedParams,
]

export {}
