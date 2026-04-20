export interface SIM {
  id: string
  tenant_id: string
  operator_id: string
  operator_name?: string
  operator_code?: string
  apn_id?: string
  apn_name?: string
  iccid: string
  imsi: string
  msisdn?: string
  ip_address_id?: string
  ip_address?: string
  ip_pool_name?: string
  policy_version_id?: string
  policy_name?: string
  policy_version_number?: number
  esim_profile_id?: string
  sim_type: 'physical' | 'esim'
  state: SIMState
  rat_type?: string
  max_concurrent_sessions: number
  session_idle_timeout_sec: number
  session_hard_timeout_sec: number
  metadata: Record<string, unknown>
  activated_at?: string
  suspended_at?: string
  terminated_at?: string
  purge_at?: string
  created_at: string
  updated_at: string
}

export type SIMState = 'ordered' | 'active' | 'suspended' | 'terminated' | 'stolen_lost'

export interface SIMHistoryEntry {
  id: number
  sim_id: string
  from_state?: string
  to_state: string
  reason?: string
  triggered_by: string
  user_id?: string
  job_id?: string
  created_at: string
}

export interface SIMSession {
  id: string
  sim_id: string
  operator_id: string
  apn_id?: string
  nas_ip?: string
  framed_ip?: string
  rat_type?: string
  session_state: string
  acct_session_id?: string
  started_at: string
  ended_at?: string
  bytes_in: number
  bytes_out: number
  duration_sec: number
  protocol_type: string
}

export interface SIMSegment {
  id: string
  tenant_id: string
  name: string
  filter_definition: Record<string, unknown>
  created_by?: string
  created_at: string
}

export interface SegmentCount {
  segment_id: string
  count: number
}

export interface DiagnosticStep {
  step: number
  name: string
  status: 'pass' | 'fail' | 'warn' | 'skip'
  message: string
  suggestion?: string
}

export interface DiagnosticResult {
  sim_id: string
  overall_status: 'healthy' | 'degraded' | 'critical'
  steps: DiagnosticStep[]
  diagnosed_at: string
}

export interface SIMListFilters {
  state?: string
  operator_id?: string
  apn_id?: string
  policy_version_id?: string
  rat_type?: string
  q?: string
  iccid?: string
  imsi?: string
  msisdn?: string
  ip?: string
}

export interface ListMeta {
  cursor: string
  limit: number
  has_more: boolean
}

export interface ListResponse<T> {
  status: string
  data: T[]
  meta: ListMeta
}

export interface ApiResponse<T> {
  status: string
  data: T
}

export interface SIMUsageSeriesBucket {
  bucket: string
  bytes_in: number
  bytes_out: number
  cost: number
}

export interface SIMUsageTopSession {
  session_id: string
  started_at: string
  bytes_total: number
  duration_sec: number
}

export interface SIMFieldDiff {
  field: string
  value_a: unknown
  value_b: unknown
  equal: boolean
}

export interface SIMCompareResult {
  sim_a: SIM
  sim_b: SIM
  diff: SIMFieldDiff[]
  compared_at: string
}

export interface SIMUsageData {
  sim_id: string
  period: string
  total_bytes_in: number
  total_bytes_out: number
  total_cost: number
  series: SIMUsageSeriesBucket[]
  top_sessions: SIMUsageTopSession[]
}

export interface BulkJobResponse {
  job_id: string
  total_sims: number
  status: 'queued'
}

export interface SIMCDR {
  id: number
  session_id: string
  sim_id: string
  operator_id: string
  apn_id?: string
  rat_type?: string
  record_type: string
  bytes_in: number
  bytes_out: number
  duration_sec: number
  usage_cost?: string | null
  carrier_cost?: string | null
  rate_per_mb?: string | null
  rat_multiplier?: string | null
  timestamp: string
}
