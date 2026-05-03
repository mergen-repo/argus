export type IMEIPoolKind = 'whitelist' | 'greylist' | 'blacklist'

export type IMEIMatchedVia = 'exact' | 'tac_range'

export type IMEIBindingMode = 'fixed' | 'allowlist' | 'open' | 'open-with-attest' | string

export type IMEIBindingStatus = 'verified' | 'pending' | 'mismatch' | 'unbound' | string

export interface IMEILookupListEntry {
  kind: IMEIPoolKind
  entry_id: string
  matched_via: IMEIMatchedVia
}

export interface IMEILookupBoundSim {
  sim_id: string
  iccid: string
  binding_mode: IMEIBindingMode
  binding_status: IMEIBindingStatus
}

export interface IMEILookupHistoryEntry {
  id?: string | number
  sim_id?: string
  observed_imei?: string
  observed_at: string
  capture_protocol?: string
  was_mismatch?: boolean
  alarm_raised?: boolean
}

export interface IMEILookupResult {
  lists: IMEILookupListEntry[]
  bound_sims: IMEILookupBoundSim[]
  history: IMEILookupHistoryEntry[]
}

export interface IMEILookupApiEnvelope {
  status: 'success'
  data: IMEILookupResult
}

export interface IMEILookupApiError {
  status: 'error'
  error: {
    code: string
    message: string
    details?: Array<Record<string, unknown>>
  }
}

export const IMEI_LENGTH = 15

export function isValidIMEI(value: string): boolean {
  return /^\d{15}$/.test(value)
}

export function tacFromIMEI(imei: string): string | null {
  if (!isValidIMEI(imei)) return null
  return imei.slice(0, 8)
}
