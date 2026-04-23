export interface TimeSeriesPoint {
  ts: string
  total_bytes: number
  sessions: number
  auths: number
  unique_sims: number
  group_key?: string
}

export interface UsageTotals {
  total_bytes: number
  total_sessions: number
  total_auths: number
  unique_sims: number
}

export interface UsageBreakdown {
  key: string
  total_bytes: number
  sessions: number
  auths: number
  percentage: number
}

export interface TopConsumer {
  sim_id: string
  iccid?: string
  operator_name?: string
  apn_name?: string
  ip_address?: string
  total_bytes: number
  sessions: number
}

export interface UsageComparison {
  previous_totals: UsageTotals
  bytes_delta_pct: number
  sessions_delta_pct: number
  auths_delta_pct: number
  sims_delta_pct: number
}

export interface UsageResponse {
  period: string
  from: string
  to: string
  bucket_size: string
  time_series: TimeSeriesPoint[]
  totals: UsageTotals
  breakdowns: Record<string, UsageBreakdown[]>
  top_consumers: TopConsumer[]
  comparison?: UsageComparison
}

export interface OperatorCost {
  operator_id: string
  operator_name?: string | null
  total_usage_cost: number
  total_carrier_cost: number
  total_bytes: number
  cdr_count: number
  percentage: number
}

export interface CostPerMB {
  operator_id: string
  operator_name?: string | null
  rat_type: string
  avg_cost_per_mb: number
  total_cost: number
  total_mb: number
}

export interface TopExpensiveSIM {
  sim_id: string
  total_usage_cost: number
  total_bytes: number
  cdr_count: number
  operator_id: string
}

export interface CostTrendPoint {
  ts: string
  total_usage_cost: number
  total_carrier_cost: number
  total_bytes: number
  active_sims: number
}

export interface CostComparison {
  previous_total_cost: number
  cost_delta_pct: number
  previous_bytes: number
  bytes_delta_pct: number
  previous_sims: number
  sims_delta_pct: number
}

export interface CostSuggestion {
  type: string
  description: string
  affected_sim_count: number
  potential_savings: number
  action: string
}

export interface CostResponse {
  total_cost: number
  currency: string
  by_operator: OperatorCost[]
  cost_per_mb: CostPerMB[]
  top_expensive_sims: TopExpensiveSIM[]
  trend: CostTrendPoint[]
  comparison?: CostComparison
  suggestions: CostSuggestion[]
}

import { type Severity } from '@/lib/severity'
import type { AlertSource, AlertState } from '@/lib/alerts'

export interface Anomaly {
  id: string
  tenant_id: string
  sim_id?: string
  sim_iccid?: string
  type: string
  severity: Severity
  state: 'open' | 'acknowledged' | 'resolved' | 'false_positive'
  details: Record<string, unknown>
  source?: string
  detected_at: string
  acknowledged_at?: string
  resolved_at?: string
}

export interface Alert {
  id: string
  tenant_id: string
  type: string
  severity: Severity
  source: AlertSource
  state: AlertState
  title: string
  description: string
  meta: Record<string, unknown>
  sim_id: string | null
  sim_iccid?: string | null
  operator_id: string | null
  apn_id: string | null
  dedup_key: string | null
  fired_at: string
  acknowledged_at: string | null
  acknowledged_by: string | null
  resolved_at: string | null
  occurrence_count: number
  first_seen_at: string
  last_seen_at: string
  cooldown_until: string | null
}

export type UsagePeriod = '1h' | '24h' | '7d' | '30d' | 'custom'
export type UsageGroupBy = '' | 'operator' | 'apn' | 'rat_type'
export type UsageMetric = 'total_bytes' | 'sessions' | 'auths'
export type AnomalyState = '' | 'open' | 'acknowledged' | 'resolved' | 'false_positive'
export type AnomalySeverity = '' | Severity
