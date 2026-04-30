/**
 * Type-check smoke tests for RolloutActivePanel — FIX-232.
 *
 * NOTE: Full vitest + @testing-library/react rendering tests are NOT configured
 * in this project (no vitest config, no @testing-library/react dependency).
 * These tests validate TypeScript types and prop contracts via `tsc --noEmit`
 * (run as part of `npm run build`), following the same pattern as
 * web/src/__tests__/admin.smoke.test.tsx.
 *
 * When vitest + RTL are added, replace this file with rendering tests:
 *   - renders state badge for each state (6 states: pending, in_progress, completed, aborted, rolled_back, failed)
 *   - Advance button hidden when current stage status != completed
 *   - Abort/Rollback hidden in terminal states (completed, aborted, rolled_back)
 *   - aria-valuenow on progress bar matches progress_pct
 */

import type { RolloutActivePanelProps, RolloutCoaCounts } from '@/components/policy/rollout-active-panel'
import type { PolicyRollout } from '@/types/policy'

const baseRollout: PolicyRollout = {
  id: '00000000-0000-0000-0000-000000000001',
  policy_version_id: '00000000-0000-0000-0000-000000000002',
  strategy: 'staged',
  stages: [],
  current_stage: 0,
  total_sims: 100,
  migrated_sims: 50,
  state: 'in_progress',
  created_at: '2026-01-01T00:00:00Z',
}

const baseCoaCounts: RolloutCoaCounts = {
  pending: 5,
  queued: 3,
  acked: 40,
  failed: 2,
  no_session: 1,
  skipped: 0,
}

const _propsMinimal: RolloutActivePanelProps = {
  rollout: baseRollout,
  onAdvance: () => {},
  onRollback: () => {},
  onAbort: () => {},
}

const _propsFull: RolloutActivePanelProps = {
  rollout: { ...baseRollout, state: 'aborted' },
  onAdvance: () => {},
  onRollback: () => {},
  onAbort: () => {},
  onRetryFailed: () => {},
  onOpenExpanded: () => {},
  coaCounts: baseCoaCounts,
}

const _terminalStates: PolicyRollout['state'][] = [
  'completed',
  'aborted',
  'rolled_back',
]

const _activeStates: PolicyRollout['state'][] = [
  'pending',
  'in_progress',
]

export {}
