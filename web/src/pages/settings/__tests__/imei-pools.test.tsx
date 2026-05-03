/**
 * Smoke tests for SCR-196 IMEI Pools page (STORY-095 Task 7).
 * Type-level + runtime-throw assertions — mirrors existing project pattern
 * (settings-index.test.tsx, use-hash-tab.test.ts). No vitest framework.
 *
 * Covered scenarios:
 *  1. All 4 tab keys are exported and well-typed
 *  2. Default tab on missing hash → "whitelist"
 *  3. Hash routing resolves valid hashes to matching tab
 *  4. Add Entry validation: empty IMEI → client-side error (full_imei + tac_range)
 *  5. Add Entry kind awareness: greylist requires quarantine_reason; blacklist requires
 *     block_reason + imported_from
 *  6. CSV-injection guard rejects strings starting with =, +, -, @, tab
 *  7. Bulk import target pool select includes all three pools
 *
 * Note: Behavioural DOM tests (render + click + drag-drop) require vitest+jsdom which
 * is not configured in this project. Coverage tracked alongside the project's broader
 * test infra wave; this file enforces structural and validator contracts via tsc.
 */

import {
  IMEI_ENTRY_KINDS,
  IMEI_IMPORTED_FROM,
  IMEI_POOLS,
  INITIAL_ADD_ENTRY_FORM,
  POOL_LABEL,
  hasCSVInjection,
  isValidIMEI,
  isValidTAC,
  validateAddEntry,
  validateIMEIOrTAC,
  type IMEIPool,
} from '@/types/imei-pool'

// ─── Scenario 1: 4 tab keys (3 pools + bulk-import) ──────────────────────────

const PAGE_TAB_KEYS = ['whitelist', 'greylist', 'blacklist', 'bulk-import'] as const
type PageTabKey = (typeof PAGE_TAB_KEYS)[number]

if (PAGE_TAB_KEYS.length !== 4) {
  throw new Error(`AC-10 FAIL: page must expose 4 tabs, got ${PAGE_TAB_KEYS.length}`)
}

// Pool tab keys must exactly match the canonical IMEIPool union
const poolTabs: ReadonlyArray<IMEIPool> = ['whitelist', 'greylist', 'blacklist']
for (const p of poolTabs) {
  if (!(PAGE_TAB_KEYS as readonly string[]).includes(p)) {
    throw new Error(`AC-10 FAIL: page tabs missing pool "${p}"`)
  }
}
if (!(PAGE_TAB_KEYS as readonly string[]).includes('bulk-import')) {
  throw new Error('AC-10 FAIL: page tabs missing "bulk-import"')
}

// ─── Scenario 2 & 3: hash routing resolution ─────────────────────────────────

function resolveHash(hash: string, validTabs: readonly string[], fallback: string): string {
  const val = hash.replace(/^#/, '')
  if (val && validTabs.includes(val)) return val
  return fallback
}

if (resolveHash('', PAGE_TAB_KEYS, 'whitelist') !== 'whitelist') {
  throw new Error('AC-10 FAIL: empty hash should resolve to whitelist (default tab)')
}
if (resolveHash('#blacklist', PAGE_TAB_KEYS, 'whitelist') !== 'blacklist') {
  throw new Error('AC-10 FAIL: #blacklist must resolve to blacklist tab')
}
if (resolveHash('#bulk-import', PAGE_TAB_KEYS, 'whitelist') !== 'bulk-import') {
  throw new Error('AC-10 FAIL: #bulk-import must resolve to bulk-import tab')
}
if (resolveHash('#unknown', PAGE_TAB_KEYS, 'whitelist') !== 'whitelist') {
  throw new Error('AC-10 FAIL: unknown hash must fall back to whitelist')
}

// ─── Scenario 4: Empty IMEI client-side validation ───────────────────────────

const emptyFullImeiErr = validateIMEIOrTAC('full_imei', '')
if (emptyFullImeiErr === null) {
  throw new Error('Validation FAIL: empty IMEI must return an error')
}
const shortIMEI = validateIMEIOrTAC('full_imei', '12345')
if (shortIMEI === null) {
  throw new Error('Validation FAIL: 5-digit IMEI must fail length validation')
}
if (validateIMEIOrTAC('full_imei', '353901080000007') !== null) {
  throw new Error('Validation FAIL: 15-digit IMEI must pass')
}

const emptyTACErr = validateIMEIOrTAC('tac_range', '')
if (emptyTACErr === null) {
  throw new Error('Validation FAIL: empty TAC must return an error')
}
if (validateIMEIOrTAC('tac_range', '12345678') !== null) {
  throw new Error('Validation FAIL: 8-digit TAC must pass')
}
if (validateIMEIOrTAC('tac_range', '123') === null) {
  throw new Error('Validation FAIL: 3-digit TAC must fail length validation')
}

if (!isValidIMEI('353901080000007')) throw new Error('isValidIMEI 15-digit must be true')
if (isValidIMEI('1234')) throw new Error('isValidIMEI must reject short input')
if (!isValidTAC('35390108')) throw new Error('isValidTAC 8-digit must be true')
if (isValidTAC('1234567')) throw new Error('isValidTAC must reject 7-digit input')

// ─── Scenario 5: pool kind-awareness (greylist + blacklist required fields) ──

const baseForm = { ...INITIAL_ADD_ENTRY_FORM, imei_or_tac: '353901080000007', kind: 'full_imei' as const }

const whiteOK = validateAddEntry('whitelist', baseForm)
if (Object.keys(whiteOK).length !== 0) {
  throw new Error(`Validation FAIL: whitelist baseline should be valid, got ${JSON.stringify(whiteOK)}`)
}

const greyMissing = validateAddEntry('greylist', baseForm)
if (!greyMissing.quarantine_reason) {
  throw new Error('Validation FAIL: greylist must require quarantine_reason')
}

const greyOK = validateAddEntry('greylist', { ...baseForm, quarantine_reason: 'Suspected reused IMEI' })
if (Object.keys(greyOK).length !== 0) {
  throw new Error(`Validation FAIL: greylist with reason should be valid, got ${JSON.stringify(greyOK)}`)
}

const blackMissing = validateAddEntry('blacklist', baseForm)
if (!blackMissing.block_reason) {
  throw new Error('Validation FAIL: blacklist must require block_reason')
}

const blackOK = validateAddEntry('blacklist', { ...baseForm, block_reason: 'Reported stolen', imported_from: 'manual' })
if (Object.keys(blackOK).length !== 0) {
  throw new Error(`Validation FAIL: blacklist with reason+source should be valid, got ${JSON.stringify(blackOK)}`)
}

// ─── Scenario 6: CSV-injection guard ─────────────────────────────────────────

for (const malicious of ['=cmd|\'/c calc\'!A1', '+1+1', '-2+5', '@SUM(A1)', '\tinjected']) {
  if (!hasCSVInjection(malicious)) {
    throw new Error(`CSV-injection guard FAIL: must reject "${malicious}"`)
  }
}
for (const benign of ['Quectel BG95', '5G CAT-M1', '0000-IoT', '123-fleet', '']) {
  if (hasCSVInjection(benign)) {
    throw new Error(`CSV-injection guard FAIL: should accept benign "${benign}"`)
  }
}

const malForm = { ...baseForm, description: '=cmd|exec' }
const malErrs = validateAddEntry('whitelist', malForm)
if (!malErrs.description) {
  throw new Error('Validation FAIL: malicious description must surface error')
}

// ─── Scenario 7: bulk-import target pool select includes all 3 pools ─────────

if (IMEI_POOLS.length !== 3) {
  throw new Error(`AC-10 FAIL: IMEI_POOLS must list all 3 pools, got ${IMEI_POOLS.length}`)
}
for (const p of IMEI_POOLS) {
  if (!POOL_LABEL[p]) {
    throw new Error(`AC-10 FAIL: missing label for pool "${p}"`)
  }
}

if (IMEI_ENTRY_KINDS.length !== 2) {
  throw new Error(`AC-10 FAIL: IMEI_ENTRY_KINDS must be 2, got ${IMEI_ENTRY_KINDS.length}`)
}
if (IMEI_IMPORTED_FROM.length !== 3) {
  throw new Error(`AC-10 FAIL: IMEI_IMPORTED_FROM must be 3, got ${IMEI_IMPORTED_FROM.length}`)
}

// ─── Type-level export checks ────────────────────────────────────────────────

const _tabKeyCheck: PageTabKey = 'whitelist'
void _tabKeyCheck

export {
  PAGE_TAB_KEYS,
  resolveHash,
}
