import type { SIM } from './sim'

export interface APN {
  id: string
  tenant_id: string
  operator_id: string
  name: string
  display_name?: string
  apn_type: string
  supported_rat_types: string[]
  default_policy_id?: string
  state: string
  settings: Record<string, unknown>
  created_at: string
  updated_at: string
  created_by?: string
  updated_by?: string
  sim_count?: number | null
  traffic_24h_bytes?: number | null
  pool_used?: number | null
  pool_total?: number | null
}

export interface IPPool {
  id: string
  apn_id: string
  name: string
  cidr_v4?: string
  cidr_v6?: string
  total_addresses: number
  used_addresses: number
  available_addresses: number
  utilization_pct: number
  state: string
  alert_threshold_warning: number
  reclaim_grace_period_days: number
  created_at: string
}

export type APNListFilters = {
  operator_id?: string
  state?: string
  q?: string
}
