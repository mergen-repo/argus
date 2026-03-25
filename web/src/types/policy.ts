export interface Policy {
  id: string
  name: string
  description?: string
  scope: string
  scope_ref_id?: string
  current_version_id?: string
  state: string
  created_at: string
  updated_at: string
  versions?: PolicyVersion[]
}

export interface PolicyListItem {
  id: string
  name: string
  description?: string
  scope: string
  active_version?: number
  sim_count: number
  current_version_id?: string
  state: string
  updated_at: string
}

export interface PolicyVersion {
  id: string
  policy_id: string
  version: number
  dsl_content?: string
  compiled_rules?: unknown
  state: string
  affected_sim_count?: number
  activated_at?: string
  created_at: string
}

export type VersionState = 'draft' | 'active' | 'superseded' | 'archived'

export interface DryRunResult {
  version_id: string
  total_affected: number
  by_operator: Record<string, number>
  by_apn: Record<string, number>
  by_rat: Record<string, number>
  behavioral_changes: BehavioralChange[]
  sample_sims: SampleSIM[]
  evaluated_at: string
}

export interface BehavioralChange {
  type: string
  description: string
  affected_count: number
  field: string
  old_value: unknown
  new_value: unknown
}

export interface SampleSIM {
  sim_id: string
  iccid: string
  ip_address?: string
  operator: string
  apn: string
  rat_type: string
  before: PolicyResult | null
  after: PolicyResult | null
}

export interface PolicyResult {
  bandwidth_down?: number
  bandwidth_up?: number
  session_timeout?: number
  idle_timeout?: number
  max_sessions?: number
  qos_class?: number
  priority?: number
}

export interface DiffLine {
  type: 'added' | 'removed' | 'unchanged'
  content: string
  line_num?: number
}

export interface DiffResponse {
  version_1: number
  version_2: number
  lines: DiffLine[]
}

export interface RolloutStage {
  pct: number
  status: string
  sim_count?: number
  migrated?: number
}

export interface PolicyRollout {
  id: string
  policy_version_id: string
  previous_version_id?: string
  strategy: string
  stages: RolloutStage[]
  current_stage: number
  total_sims: number
  migrated_sims: number
  state: string
  started_at?: string
  completed_at?: string
  rolled_back_at?: string
  created_at: string
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
