// STORY-097 — SCR-021f Device Binding tab DTOs.
// Backend handler: internal/api/sim/device_binding_handler.go (API-327, API-329, API-330).

// Canonical six binding modes per ADR-004 §"Binding modes" plus 'disabled'
// for the NULL/disabled state. Backend store enums are defined in
// internal/store/sim.go ValidBindingModes.
export type BindingMode =
  | 'strict'
  | 'allowlist'
  | 'first-use'
  | 'tac-lock'
  | 'grace-period'
  | 'soft'
  | 'disabled'

export type BindingStatus =
  | 'verified'
  | 'pending'
  | 'mismatch'
  | 'disabled'

export type CaptureProtocol = 'radius' | 'diameter_s6a' | '5g_sba'

export interface DeviceBinding {
  bound_imei: string | null
  binding_mode: BindingMode | null
  binding_status: BindingStatus | null
  binding_verified_at: string | null
  last_imei_seen_at: string | null
  binding_grace_expires_at: string | null
  history_count: number
}

export interface IMEIHistoryRow {
  id: string
  sim_id: string
  tenant_id: string
  observed_imei: string
  observed_software_version: string | null
  observed_at: string
  capture_protocol: CaptureProtocol
  nas_ip_address: string | null
  was_mismatch: boolean
  alarm_raised: boolean
}

export interface IMEIHistoryMeta {
  next_cursor: string
  limit: number
  has_more: boolean
}

export interface IMEIHistoryListResponse {
  status: string
  data: IMEIHistoryRow[]
  meta: IMEIHistoryMeta
}

export interface IMEIHistoryFilters {
  protocol?: CaptureProtocol | ''
  since?: string
}

export const CAPTURE_PROTOCOLS: ReadonlyArray<CaptureProtocol> = [
  'radius',
  'diameter_s6a',
  '5g_sba',
] as const

export const CAPTURE_PROTOCOL_LABEL: Record<CaptureProtocol, string> = {
  radius: 'RADIUS',
  diameter_s6a: 'Diameter S6a',
  '5g_sba': '5G SBA',
}

export const BINDING_MODE_LABEL: Record<BindingMode, string> = {
  strict: 'Strict',
  allowlist: 'Allowlist',
  'first-use': 'First Use',
  'tac-lock': 'TAC Lock',
  'grace-period': 'Grace Period',
  soft: 'Soft',
  disabled: 'Disabled',
}

export const BINDING_STATUS_LABEL: Record<BindingStatus, string> = {
  verified: 'Verified',
  pending: 'Pending',
  mismatch: 'Mismatch',
  disabled: 'Disabled',
}

// Maps binding_status to the project Badge variant.
export function bindingStatusVariant(
  status: BindingStatus | null,
): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'verified':
      return 'success'
    case 'pending':
      return 'warning'
    case 'mismatch':
      return 'danger'
    case 'disabled':
    default:
      return 'secondary'
  }
}

// Computes time-until-expiry tone. Used by the grace-period badge.
// 'caution' (formerly 'safe') makes the visual warning-hue intent explicit:
// the SIM is still in the grace window but operators should treat it as a
// transient state rather than a stable verified pairing.
//   >= 24h  → 'caution' (warning hue, calm)
//   < 24h   → 'urgent' (danger hue)
//   <= 0    → 'expired' (muted)
export type GraceTone = 'caution' | 'urgent' | 'expired'

export function graceToneFor(expiresAtISO: string, nowMs: number): GraceTone {
  const t = new Date(expiresAtISO).getTime()
  if (!Number.isFinite(t)) return 'expired'
  const diffMs = t - nowMs
  if (diffMs <= 0) return 'expired'
  if (diffMs < 24 * 60 * 60 * 1000) return 'urgent'
  return 'caution'
}

// Renders "Grace expires in 14 h" / "36 m" / "Grace expired" given a future ISO.
export function formatGraceCountdown(expiresAtISO: string, nowMs: number): string {
  const t = new Date(expiresAtISO).getTime()
  if (!Number.isFinite(t)) return ''
  const diffMs = t - nowMs
  if (diffMs <= 0) return 'Grace expired'
  const diffMins = Math.floor(diffMs / 60_000)
  if (diffMins < 60) return `Grace expires in ${diffMins} m`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 48) return `Grace expires in ${diffHours} h`
  const diffDays = Math.floor(diffHours / 24)
  return `Grace expires in ${diffDays} d`
}
