// IMEI Pool DTOs — mirror internal/api/imei_pool/handler.go exactly.

export type IMEIPool = 'whitelist' | 'greylist' | 'blacklist'
export type IMEIEntryKind = 'full_imei' | 'tac_range'
export type IMEIImportedFrom = 'manual' | 'gsma_ceir' | 'operator_eir'

export const IMEI_POOLS: ReadonlyArray<IMEIPool> = ['whitelist', 'greylist', 'blacklist']
export const IMEI_ENTRY_KINDS: ReadonlyArray<IMEIEntryKind> = ['full_imei', 'tac_range']
export const IMEI_IMPORTED_FROM: ReadonlyArray<IMEIImportedFrom> = ['manual', 'gsma_ceir', 'operator_eir']

export interface IMEIPoolEntry {
  id: string
  tenant_id: string
  pool: IMEIPool
  kind: IMEIEntryKind
  imei_or_tac: string
  device_model: string | null
  description: string | null
  quarantine_reason?: string | null
  block_reason?: string | null
  imported_from?: IMEIImportedFrom | null
  created_at: string
  updated_at: string
  bound_sims_count: number
}

export interface IMEIPoolListFilters {
  pool: IMEIPool
  kind?: IMEIEntryKind | ''
  tac?: string
  device_model?: string
  q?: string
  include_bound_count?: boolean
}

export interface IMEIPoolAddPayload {
  kind: IMEIEntryKind
  imei_or_tac: string
  device_model?: string | null
  description?: string | null
  quarantine_reason?: string | null
  block_reason?: string | null
  imported_from?: IMEIImportedFrom | null
}

export interface IMEIPoolBulkImportResponse {
  job_id: string
  status: string
}

// Validators ─────────────────────────────────────────────────────────────────

const DIGITS_RE = /^\d+$/

export function isValidIMEI(value: string): boolean {
  return DIGITS_RE.test(value) && value.length === 15
}

export function isValidTAC(value: string): boolean {
  return DIGITS_RE.test(value) && value.length === 8
}

export function validateIMEIOrTAC(kind: IMEIEntryKind, value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return 'IMEI / TAC is required'
  if (kind === 'full_imei' && !isValidIMEI(trimmed)) {
    return 'Full IMEI must be exactly 15 digits'
  }
  if (kind === 'tac_range' && !isValidTAC(trimmed)) {
    return 'TAC range must be exactly 8 digits'
  }
  return null
}

// CSV-injection guard mirrors STORY-095 compliance rule (line 628 of plan):
// REJECT (not sanitize) any string that, after trim, starts with =, +, -, @, or tab.
const CSV_INJECTION_FIRST_CHARS = new Set(['=', '+', '-', '@', '\t'])

export function hasCSVInjection(value: string | null | undefined): boolean {
  if (!value) return false
  const trimmed = value.trim()
  if (!trimmed) return false
  return CSV_INJECTION_FIRST_CHARS.has(trimmed[0])
}

export interface AddEntryFormState {
  kind: IMEIEntryKind
  imei_or_tac: string
  device_model: string
  description: string
  quarantine_reason: string
  block_reason: string
  imported_from: IMEIImportedFrom
}

export const INITIAL_ADD_ENTRY_FORM: AddEntryFormState = {
  kind: 'full_imei',
  imei_or_tac: '',
  device_model: '',
  description: '',
  quarantine_reason: '',
  block_reason: '',
  imported_from: 'manual',
}

export function validateAddEntry(
  pool: IMEIPool,
  form: AddEntryFormState,
): Record<string, string> {
  const errors: Record<string, string> = {}

  const imeiErr = validateIMEIOrTAC(form.kind, form.imei_or_tac)
  if (imeiErr) errors.imei_or_tac = imeiErr

  if (form.device_model.length > 64) {
    errors.device_model = 'Device model must be at most 64 characters'
  }
  if (hasCSVInjection(form.device_model)) {
    errors.device_model = 'Cannot start with =, +, -, @ or tab'
  }
  if (hasCSVInjection(form.description)) {
    errors.description = 'Cannot start with =, +, -, @ or tab'
  }

  if (pool === 'greylist') {
    if (!form.quarantine_reason.trim()) {
      errors.quarantine_reason = 'Quarantine reason is required for greylist'
    } else if (hasCSVInjection(form.quarantine_reason)) {
      errors.quarantine_reason = 'Cannot start with =, +, -, @ or tab'
    }
  }
  if (pool === 'blacklist') {
    if (!form.block_reason.trim()) {
      errors.block_reason = 'Block reason is required for blacklist'
    } else if (hasCSVInjection(form.block_reason)) {
      errors.block_reason = 'Cannot start with =, +, -, @ or tab'
    }
    if (!IMEI_IMPORTED_FROM.includes(form.imported_from)) {
      errors.imported_from = 'Source is required for blacklist'
    }
  }

  return errors
}

export const POOL_LABEL: Record<IMEIPool, string> = {
  whitelist: 'White List',
  greylist: 'Grey List',
  blacklist: 'Black List',
}

export const ENTRY_KIND_LABEL: Record<IMEIEntryKind, string> = {
  full_imei: 'Full IMEI',
  tac_range: 'TAC range',
}

export const IMPORTED_FROM_LABEL: Record<IMEIImportedFrom, string> = {
  manual: 'Manual',
  gsma_ceir: 'GSMA CEIR',
  operator_eir: 'Operator EIR',
}
