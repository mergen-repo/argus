// FIX-244 DEV-524: canonical PolicyViolation shape + lifecycle status helpers.
//
// Lifted from web/src/pages/violations/index.tsx and merged with the duplicate
// in web/src/components/shared/related-violations-tab.tsx. Both files import
// from here to remove the divergence (each had its own copy of the interface
// with subtly different optional fields, causing churn whenever the wire
// shape changed).
//
// Status is derived FE-side from existing backend fields. Source of truth:
// `acknowledged_at` plus `details.remediation` (written by handler.Remediate
// and store.SetRemediationKind in DEV-520). No new server column.

export interface PolicyViolation {
  id: string
  tenant_id: string
  sim_id: string
  iccid?: string | null
  imsi?: string | null
  msisdn?: string | null
  /** Legacy alias used by older endpoints — prefer `iccid`. */
  sim_iccid?: string
  policy_id: string
  policy_name?: string | null
  policy_version_number?: number | null
  version_id: string
  rule_index: number
  violation_type: string
  action_taken: string
  details?: Record<string, unknown>
  session_id?: string | null
  operator_id?: string | null
  operator_name?: string | null
  operator_code?: string | null
  apn_id?: string | null
  apn_name?: string | null
  severity: string
  created_at: string
  acknowledged_at?: string | null
  acknowledged_by?: string | null
  acknowledgment_note?: string | null
}

export type ViolationStatus =
  | 'open'
  | 'acknowledged'
  | 'remediated'
  | 'dismissed'
  | 'escalated'

/** Pure derivation — no async, no toast, no side effects. */
export function deriveStatus(v: PolicyViolation): ViolationStatus {
  const det = (v.details ?? {}) as Record<string, unknown>
  const remediation = typeof det.remediation === 'string' ? det.remediation : undefined
  if (remediation === 'suspend_sim') return 'remediated'
  if (remediation === 'dismiss') return 'dismissed'
  if (remediation === 'escalate') return 'escalated'
  if (v.acknowledged_at) return 'acknowledged'
  return 'open'
}

export interface FilterOption<V extends string = string> {
  value: V
  label: string
}

/** Backend `violation_type` whitelist — kept in sync with policy DSL emit set. */
export const VIOLATION_TYPE_FILTER_OPTIONS: ReadonlyArray<FilterOption> = [
  { value: '', label: 'All Types' },
  { value: 'bandwidth_exceeded', label: 'Bandwidth Exceeded' },
  { value: 'session_limit', label: 'Session Limit' },
  { value: 'quota_exceeded', label: 'Quota Exceeded' },
  { value: 'time_restriction', label: 'Time Restriction' },
  { value: 'geo_blocked', label: 'Geo Blocked' },
]

/** Backend `action_taken` whitelist — what the policy did when the rule fired. */
export const ACTION_TAKEN_FILTER_OPTIONS: ReadonlyArray<FilterOption> = [
  { value: '', label: 'All Actions' },
  { value: 'block', label: 'Block' },
  { value: 'disconnect', label: 'Disconnect' },
  { value: 'suspend', label: 'Suspend' },
  { value: 'throttle', label: 'Throttle' },
  { value: 'policy_notify', label: 'Notify' },
  { value: 'policy_log', label: 'Log' },
  { value: 'policy_tag', label: 'Tag' },
]

export const STATUS_FILTER_OPTIONS: ReadonlyArray<FilterOption<'' | ViolationStatus>> = [
  { value: '', label: 'All Statuses' },
  { value: 'open', label: 'Open' },
  { value: 'acknowledged', label: 'Acknowledged' },
  { value: 'remediated', label: 'Remediated' },
  { value: 'dismissed', label: 'Dismissed' },
  { value: 'escalated', label: 'Escalated' },
]
