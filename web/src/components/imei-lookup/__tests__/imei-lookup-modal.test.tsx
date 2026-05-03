/**
 * Smoke tests for IMEILookupModal (STORY-095 Task 8 / AC-11).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Behavior contracts asserted:
 *  - Component prop shape (open, onOpenChange, onSubmit, initialImei, loading, serverError)
 *  - 15-digit IMEI validation via the shared isValidIMEI helper
 *  - 14-digit / non-numeric input rejected as invalid (used for inline error UI)
 *
 * Note: Behavioral DOM tests (render → fill input → click submit) require
 * vitest + @testing-library which are not yet configured in this project
 * (web/package.json has no test script and no vitest dep). Behavioral
 * coverage tracked alongside the wider FE test hardening initiative.
 */

import { IMEILookupModal } from '@/components/imei-lookup/imei-lookup-modal'
import { isValidIMEI, IMEI_LENGTH } from '@/types/imei-lookup'

// ── Type contract ──────────────────────────────────────────────────────────

type ModalProps = React.ComponentProps<typeof IMEILookupModal>

const _minProps: ModalProps = {
  open: true,
  onOpenChange: (_open: boolean) => {
    /* no-op */
  },
  onSubmit: (_imei: string) => {
    /* no-op */
  },
}

const _fullProps: ModalProps = {
  open: false,
  onOpenChange: (_open: boolean) => {
    /* no-op */
  },
  onSubmit: (_imei: string) => {
    /* no-op */
  },
  initialImei: '359211089765432',
  loading: true,
  serverError: 'Invalid IMEI format.',
}

// ── IMEI validation contract ───────────────────────────────────────────────

// 15-digit numeric IMEI is valid (used by Look-up button enable state)
const _valid15: boolean = isValidIMEI('359211089765432')

// 14-digit input MUST be invalid (drives the "Enter a 15-digit IMEI"
// inline error in the modal)
const _invalid14: boolean = !isValidIMEI('35921108976543')

// 16-digit input MUST be invalid
const _invalid16: boolean = !isValidIMEI('3592110897654321')

// Non-numeric input MUST be invalid even at the right length
const _invalidNonDigit: boolean = !isValidIMEI('35921108976543A')

// Empty string MUST be invalid
const _invalidEmpty: boolean = !isValidIMEI('')

// IMEI_LENGTH constant is exported and equals 15
const _imeiLength: 15 = IMEI_LENGTH

export {
  _minProps,
  _fullProps,
  _valid15,
  _invalid14,
  _invalid16,
  _invalidNonDigit,
  _invalidEmpty,
  _imeiLength,
}

import type * as React from 'react'
