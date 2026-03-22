export interface ESimProfile {
  id: string
  sim_id: string
  eid: string
  sm_dp_plus_id?: string
  operator_id: string
  profile_state: ESimProfileState
  iccid_on_profile?: string
  last_provisioned_at?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export type ESimProfileState = 'available' | 'enabled' | 'disabled' | 'deleted'

export interface ESimSwitchResult {
  sim_id: string
  old_profile: ESimProfile
  new_profile: ESimProfile
  new_operator_id: string
}
