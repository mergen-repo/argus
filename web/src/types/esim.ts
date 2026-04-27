export interface ESimProfile {
  id: string
  sim_id: string
  eid: string
  sm_dp_plus_id?: string
  profile_id?: string
  operator_id: string
  operator_name?: string
  operator_code?: string
  profile_state: ESimProfileState
  iccid_on_profile?: string
  last_provisioned_at?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export type ESimProfileState = 'available' | 'enabled' | 'disabled' | 'deleted'

export interface ESimCreateRequest {
  sim_id: string
  eid: string
  operator_id: string
  iccid_on_profile: string
  profile_id: string
}

export interface ESimSwitchResult {
  sim_id: string
  old_profile: ESimProfile
  new_profile: ESimProfile
  new_operator_id: string
  ip_released?: boolean
  policy_cleared?: boolean
}

export type OTAStatus = 'queued' | 'sent' | 'acked' | 'failed' | 'timeout'

export type OTACommandType = 'enable' | 'disable' | 'switch' | 'delete'

export interface OTACommand {
  id: string
  eid: string
  profile_id: string
  command_type: OTACommandType
  target_operator_id: string
  status: OTAStatus
  retry_count: number
  created_at: string
  sent_at?: string
  acked_at?: string
  error_message?: string
  smsr_command_id?: string
}

export interface StockSummaryEntry {
  operator_id: string
  operator_name: string
  total: number
  allocated: number
  available: number
}

export type BulkSwitchRequest = (
  | { filter: Record<string, string | number | boolean>; eids?: never; sim_ids?: never }
  | { eids: string[]; filter?: never; sim_ids?: never }
  | { sim_ids: string[]; filter?: never; eids?: never }
) & {
  target_operator_id: string
  reason?: string
}

export interface BulkSwitchResponse {
  job_id: string
  affected_count: number
  mode: 'ota'
}
