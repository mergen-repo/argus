export type SLAOperatorMonthAgg = {
  operator_id: string
  operator_name: string
  operator_code: string
  uptime_pct: number
  incident_count: number
  breach_minutes: number
  latency_p95_ms: number
  mttr_sec: number
  sessions_total: number
  sla_uptime_target: number
  report_id: string | null
}

export type SLAMonthSummary = {
  year: number
  month: number
  overall: SLAOperatorMonthAgg
  operators: SLAOperatorMonthAgg[]
}

export type SLABreach = {
  started_at: string
  ended_at: string
  duration_sec: number
  cause: 'down' | 'latency' | 'mixed'
  samples_count: number
  affected_sessions_est: number
}

export type SLABreachTotals = {
  breaches_count: number
  downtime_seconds: number
  affected_sessions_est: number
}

export type SLABreachesData = {
  breaches: SLABreach[]
  totals: SLABreachTotals
}

export type SLABreachesResponse = {
  data: SLABreachesData
  meta: { breach_source: 'live' | 'persisted'; affected_sessions_est_note?: string }
}
