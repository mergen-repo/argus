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
