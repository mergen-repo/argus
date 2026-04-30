export interface Session {
  id: string
  sim_id: string
  tenant_id: string
  operator_id: string
  operator_name?: string
  operator_code?: string
  apn_id?: string
  apn_name?: string
  iccid?: string
  imsi: string
  msisdn?: string
  policy_name?: string
  policy_version_number?: number
  acct_session_id: string
  nas_ip: string
  framed_ip?: string
  rat_type?: string
  state: string
  bytes_in: number
  bytes_out: number
  duration_sec: number
  ip_address?: string
  started_at: string
}

export interface TopOperator {
  id: string
  name: string
  code: string
  count: number
}

export interface SessionStats {
  total_active: number
  by_operator: Record<string, number>
  by_apn: Record<string, number>
  by_rat_type: Record<string, number>
  avg_duration_sec: number
  avg_bytes: number
  top_operator?: TopOperator | null
}

export interface SoRDecision {
  chosen_operator_id: string
  scoring: Array<{ operator_id: string; score: number; reason: string }>
  decided_at: string
}

export interface PolicyApplied {
  policy_id: string
  policy_name: string
  policy_version_id: string
  version_number: number
  coa_status: string
  coa_sent_at: string | null
  coa_failure_reason: string | null
  matched_rules: number[]
}

export interface QuotaUsage {
  limit_bytes: number
  used_bytes: number
  pct_used: number
  reset_at: string | null
}

export interface CoaEntry {
  at: string
  reason: string
  policy_version_id: string | null
  status: string
}

export interface SessionDetail extends Session {
  sor_decision?: SoRDecision | null
  policy_applied?: PolicyApplied | null
  quota_usage?: QuotaUsage | null
  coa_history: CoaEntry[]
}

export interface SessionStartedEvent {
  session_id: string
  sim_id: string
  iccid: string
  imsi: string
  msisdn?: string
  operator_id: string
  operator_name: string
  apn_id?: string
  apn_name: string
  rat_type?: string
  ip_address?: string
  nas_ip: string
  started_at: string
}

export interface SessionEndedEvent {
  session_id: string
  sim_id: string
  iccid: string
  imsi: string
  operator_name: string
  apn_name: string
  duration_sec: number
  bytes_in: number
  bytes_out: number
  terminate_cause: string
  ip_address?: string
  started_at: string
  ended_at: string
}
