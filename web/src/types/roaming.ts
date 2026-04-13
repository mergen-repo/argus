export interface SLATerms {
  uptime_pct: number
  latency_p95_ms: number
  max_incidents: number
}

export interface VolumeTier {
  threshold_mb: number
  cost_per_mb: number
}

export interface CostTerms {
  cost_per_mb: number
  currency: string
  volume_tiers?: VolumeTier[]
  settlement_period: 'monthly' | 'quarterly' | 'annual'
}

export type AgreementType = 'national' | 'international' | 'MVNO'
export type AgreementState = 'draft' | 'active' | 'expired' | 'terminated'

export interface RoamingAgreement {
  id: string
  tenant_id: string
  operator_id: string
  partner_operator_name: string
  agreement_type: AgreementType
  sla_terms: SLATerms
  cost_terms: CostTerms
  start_date: string
  end_date: string
  auto_renew: boolean
  state: AgreementState
  notes?: string
  terminated_at?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export interface CreateRoamingAgreementRequest {
  operator_id: string
  partner_operator_name: string
  agreement_type: AgreementType
  sla_terms: SLATerms
  cost_terms: CostTerms
  start_date: string
  end_date: string
  auto_renew: boolean
  state?: AgreementState
  notes?: string
}

export interface UpdateRoamingAgreementRequest {
  partner_operator_name?: string
  agreement_type?: AgreementType
  sla_terms?: SLATerms
  cost_terms?: CostTerms
  start_date?: string
  end_date?: string
  auto_renew?: boolean
  state?: AgreementState
  notes?: string
}

export interface RoamingAgreementListMeta {
  cursor: string
  has_more: boolean
  limit: number
}
