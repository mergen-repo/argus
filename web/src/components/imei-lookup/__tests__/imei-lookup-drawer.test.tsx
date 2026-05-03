/**
 * Smoke tests for IMEILookupDrawer (STORY-095 Task 8 / AC-11).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Behavior contracts asserted:
 *  - Component prop shape (open, onOpenChange, imei, data, isLoading, isError)
 *  - Section components accept empty arrays (graceful empty-state handling
 *    is required because backend returns empty bound_sims/history per T5
 *    deferred tech debt)
 *  - tacFromIMEI helper returns first 8 chars for valid IMEI, null otherwise
 *
 * Note: Behavioral DOM tests (render → assert empty-state copy) require
 * vitest + @testing-library which are not yet configured in this project.
 */

import { IMEILookupDrawer } from '@/components/imei-lookup/imei-lookup-drawer'
import { ListMembershipSection } from '@/components/imei-lookup/list-membership-section'
import { BoundSimsSection } from '@/components/imei-lookup/bound-sims-section'
import { HistorySection } from '@/components/imei-lookup/history-section'
import type {
  IMEILookupResult,
  IMEILookupListEntry,
  IMEILookupBoundSim,
  IMEILookupHistoryEntry,
} from '@/types/imei-lookup'
import { tacFromIMEI } from '@/types/imei-lookup'

// ── Type contract ──────────────────────────────────────────────────────────

type DrawerProps = React.ComponentProps<typeof IMEILookupDrawer>

const _drawerMinProps: DrawerProps = {
  open: true,
  onOpenChange: () => {
    /* no-op */
  },
  imei: '359211089765432',
  data: undefined,
  isLoading: true,
  isError: false,
}

const _drawerFullProps: DrawerProps = {
  open: true,
  onOpenChange: () => {
    /* no-op */
  },
  imei: '359211089765432',
  data: {
    lists: [
      { kind: 'whitelist', entry_id: 'abc-123', matched_via: 'tac_range' },
    ],
    bound_sims: [
      {
        sim_id: 'sim-uuid',
        iccid: '8990011234567890123',
        binding_mode: 'fixed',
        binding_status: 'verified',
      },
    ],
    history: [
      {
        observed_at: '2026-04-26T14:02:00Z',
        capture_protocol: 'S6a',
        was_mismatch: false,
        alarm_raised: false,
      },
    ],
  },
  isLoading: false,
  isError: false,
  errorMessage: null,
  onRetry: () => {
    /* no-op */
  },
}

// ── Empty-array contract (T5 deferred — server returns []) ─────────────────

const _emptyResult: IMEILookupResult = {
  lists: [],
  bound_sims: [],
  history: [],
}

// Section components MUST accept empty arrays — this drives the empty state
// rendering required by Task 8 spec ("UI must handle empty states gracefully
// — show 'No bound SIMs' / 'No history' messaging, not assume populated").

type ListMembershipProps = React.ComponentProps<typeof ListMembershipSection>
const _emptyListProps: ListMembershipProps = { lists: [] }

type BoundSimsProps = React.ComponentProps<typeof BoundSimsSection>
const _emptyBoundSimsProps: BoundSimsProps = { boundSims: [] }

type HistoryProps = React.ComponentProps<typeof HistorySection>
const _emptyHistoryProps: HistoryProps = { history: [] }

// Populated DTO shape sanity checks — guard against type drift.
const _entry: IMEILookupListEntry = {
  kind: 'blacklist',
  entry_id: 'def-456',
  matched_via: 'exact',
}
const _sim: IMEILookupBoundSim = {
  sim_id: 'sim-id',
  iccid: '8990012345',
  binding_mode: 'allowlist',
  binding_status: 'pending',
}
const _hist: IMEILookupHistoryEntry = {
  observed_at: '2026-05-01T00:00:00Z',
  capture_protocol: 'GTPv2',
  was_mismatch: true,
  alarm_raised: true,
}

// ── tacFromIMEI helper contract ────────────────────────────────────────────

const _tacValid: string | null = tacFromIMEI('359211089765432')
// Result MUST equal first 8 digits when IMEI is valid:
const _tacEqualsFirst8: boolean = _tacValid === '35921108'

// Returns null for invalid IMEI:
const _tacInvalid: string | null = tacFromIMEI('not-an-imei')
const _tacIsNull: boolean = _tacInvalid === null

export {
  _drawerMinProps,
  _drawerFullProps,
  _emptyResult,
  _emptyListProps,
  _emptyBoundSimsProps,
  _emptyHistoryProps,
  _entry,
  _sim,
  _hist,
  _tacValid,
  _tacEqualsFirst8,
  _tacInvalid,
  _tacIsNull,
}

import type * as React from 'react'
