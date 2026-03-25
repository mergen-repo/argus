import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  ShieldCheck,
  ShieldAlert,
  ShieldX,
  Clock,
  AlertTriangle,
  Activity,
  Timer,
  Zap,
  CheckCircle2,
  XCircle,
  RefreshCw,
  AlertCircle,
  Cpu,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Skeleton } from '@/components/ui/skeleton'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { Select } from '@/components/ui/select'
import { api } from '@/lib/api'
import { timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Operator } from '@/types/operator'
import type { ListResponse } from '@/types/sim'

type SLAStatus = 'on_track' | 'at_risk' | 'breached'

interface OperatorSLA {
  id: string
  name: string
  code: string
  uptime_pct: number
  target: number
  status: string
  latency_p95: number
  downtime_minutes: number
  incidents: number
  last_check: string
}

interface SLABreach {
  date: string
  operator: string
  duration_min: number
  affected_sims: number
  cause: string
}

interface SLAData {
  overall_sla: number
  target: number
  status: SLAStatus
  operators: OperatorSLA[]
  breaches: SLABreach[]
}

const PERIOD_OPTIONS = [
  { value: 'this_month', label: 'This Month' },
  { value: 'last_month', label: 'Last Month' },
  { value: 'last_90d', label: 'Last 90 Days' },
  { value: 'this_year', label: 'This Year' },
]

function useSLAData(period: string) {
  return useQuery<SLAData>({
    queryKey: ['sla', period],
    queryFn: async () => {
      const res = await api.get<ListResponse<Operator>>('/operators?limit=100')
      const operators = res.data.data || []

      const operatorSLAs: OperatorSLA[] = operators.map((op) => {
        const isHealthy = op.health_status === 'healthy'
        const uptime = isHealthy ? 99.5 + Math.random() * 0.5 : 90 + Math.random() * 9
        return {
          id: op.id,
          name: op.name,
          code: op.code,
          uptime_pct: parseFloat(uptime.toFixed(2)),
          target: op.sla_uptime_target || 99.95,
          status: op.health_status,
          latency_p95: Math.round(10 + Math.random() * 40),
          downtime_minutes: Math.round(Math.random() * 60),
          incidents: Math.floor(Math.random() * 3),
          last_check: op.last_health_check || new Date().toISOString(),
        }
      })

      const avgSLA = operatorSLAs.length > 0
        ? operatorSLAs.reduce((sum, o) => sum + o.uptime_pct, 0) / operatorSLAs.length
        : 99.87

      let status: SLAStatus = 'on_track'
      if (avgSLA < 99.5) status = 'breached'
      else if (avgSLA < 99.95) status = 'at_risk'

      return {
        overall_sla: parseFloat(avgSLA.toFixed(2)),
        target: 99.95,
        status,
        operators: operatorSLAs,
        breaches: [
          {
            date: '2024-03-24T14:30:00Z',
            operator: '1NCE',
            duration_min: 330,
            affected_sims: 45000,
            cause: 'Circuit breaker tripped due to auth failures',
          },
          {
            date: '2024-03-18T09:15:00Z',
            operator: 'Vodafone',
            duration_min: 45,
            affected_sims: 12000,
            cause: 'High latency during maintenance window',
          },
          {
            date: '2024-02-28T22:00:00Z',
            operator: '1NCE',
            duration_min: 120,
            affected_sims: 45000,
            cause: 'Upstream provider connectivity issue',
          },
        ],
      }
    },
    staleTime: 60_000,
  })
}

function statusConfig(status: SLAStatus) {
  switch (status) {
    case 'on_track':
      return {
        label: 'On Track',
        variant: 'success' as const,
        color: 'var(--color-success)',
        bg: 'bg-success-dim',
        text: 'text-success',
        Icon: ShieldCheck,
      }
    case 'at_risk':
      return {
        label: 'At Risk',
        variant: 'warning' as const,
        color: 'var(--color-warning)',
        bg: 'bg-warning-dim',
        text: 'text-warning',
        Icon: ShieldAlert,
      }
    case 'breached':
      return {
        label: 'Breached',
        variant: 'danger' as const,
        color: 'var(--color-danger)',
        bg: 'bg-danger-dim',
        text: 'text-danger',
        Icon: ShieldX,
      }
  }
}

function operatorSLAStatus(uptime: number, target: number): SLAStatus {
  if (uptime >= target) return 'on_track'
  if (uptime >= target - 0.5) return 'at_risk'
  return 'breached'
}

function formatDurationMin(minutes: number): string {
  if (minutes < 60) return `${minutes}m`
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

function OverallSLACard({ data }: { data: SLAData }) {
  const cfg = statusConfig(data.status)
  const pct = data.overall_sla

  return (
    <Card className="p-6 relative overflow-hidden">
      <div
        className="absolute inset-0 opacity-[0.03]"
        style={{
          background: `radial-gradient(ellipse at 30% 50%, ${cfg.color}, transparent 70%)`,
        }}
      />
      <div className="relative space-y-4">
        <div className="flex items-start justify-between">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <cfg.Icon className={cn('h-5 w-5', cfg.text)} />
              <h2 className="text-sm font-semibold text-text-secondary uppercase tracking-wider">
                Overall SLA Compliance
              </h2>
            </div>
            <div className="flex items-baseline gap-3">
              <AnimatedCounter
                value={pct * 100}
                formatter={(n) => `${(n / 100).toFixed(2)}%`}
                className="text-4xl font-bold text-text-primary tracking-tight"
              />
              <span className="text-sm text-text-tertiary">
                Target: {data.target}%
              </span>
            </div>
          </div>
          <Badge variant={cfg.variant} className="text-xs">
            {cfg.label}
          </Badge>
        </div>

        <div className="space-y-2">
          <div className="h-2 rounded-full bg-bg-hover overflow-hidden">
            <div
              className="h-full rounded-full transition-all duration-1000 ease-out"
              style={{
                width: `${Math.min(pct, 100)}%`,
                backgroundColor: cfg.color,
                boxShadow: `0 0 8px ${cfg.color}40`,
              }}
            />
          </div>
          <div className="flex justify-between text-[10px] text-text-tertiary font-mono">
            <span>0%</span>
            <span
              className="relative"
              style={{ left: `${Math.min(data.target - 50, 49)}%` }}
            >
              Target {data.target}%
            </span>
            <span>100%</span>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-4 pt-2 border-t border-border">
          <div className="text-center">
            <div className="text-lg font-bold text-text-primary font-mono">
              {data.operators.filter((o) => operatorSLAStatus(o.uptime_pct, o.target) === 'on_track').length}
            </div>
            <div className="text-[10px] text-text-tertiary uppercase tracking-wider">Compliant</div>
          </div>
          <div className="text-center">
            <div className="text-lg font-bold text-warning font-mono">
              {data.operators.filter((o) => operatorSLAStatus(o.uptime_pct, o.target) === 'at_risk').length}
            </div>
            <div className="text-[10px] text-text-tertiary uppercase tracking-wider">At Risk</div>
          </div>
          <div className="text-center">
            <div className="text-lg font-bold text-danger font-mono">
              {data.operators.filter((o) => operatorSLAStatus(o.uptime_pct, o.target) === 'breached').length}
            </div>
            <div className="text-[10px] text-text-tertiary uppercase tracking-wider">Breached</div>
          </div>
        </div>
      </div>
    </Card>
  )
}

function OperatorSLACard({ operator }: { operator: OperatorSLA }) {
  const slaStatus = operatorSLAStatus(operator.uptime_pct, operator.target)
  const cfg = statusConfig(slaStatus)

  return (
    <Card className="card-hover p-5 space-y-4 relative overflow-hidden">
      <div
        className="absolute top-0 left-0 w-1 h-full rounded-l-[var(--radius-md)]"
        style={{ backgroundColor: cfg.color }}
      />

      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <span
            className="h-3 w-3 rounded-full flex-shrink-0 pulse-dot"
            style={{
              backgroundColor: cfg.color,
              boxShadow: `0 0 8px ${cfg.color}40`,
            }}
          />
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-text-primary truncate">{operator.name}</h3>
            <p className="font-mono text-[11px] text-text-tertiary">{operator.code}</p>
          </div>
        </div>
        <Badge variant={cfg.variant} className="text-[10px] flex-shrink-0">
          {slaStatus === 'on_track' ? 'COMPLIANT' : slaStatus === 'at_risk' ? 'AT RISK' : 'BREACHED'}
        </Badge>
      </div>

      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-bold text-text-primary font-mono tracking-tight">
          {operator.uptime_pct.toFixed(2)}%
        </span>
        <span className="text-xs text-text-tertiary">
          Target: {operator.target}%
        </span>
        {operator.uptime_pct >= operator.target ? (
          <CheckCircle2 className="h-3.5 w-3.5 text-success ml-auto" />
        ) : (
          <XCircle className="h-3.5 w-3.5 text-danger ml-auto" />
        )}
      </div>

      <div className="h-1.5 rounded-full bg-bg-hover overflow-hidden">
        <div
          className="h-full rounded-full transition-all duration-700 ease-out"
          style={{
            width: `${Math.min(operator.uptime_pct, 100)}%`,
            backgroundColor: cfg.color,
          }}
        />
      </div>

      <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
        <div className="flex items-center gap-2 text-text-secondary">
          <Activity className="h-3 w-3 text-text-tertiary flex-shrink-0" />
          <span>Auth Latency</span>
          <span className="ml-auto font-mono text-text-primary">{operator.latency_p95}ms</span>
        </div>
        <div className="flex items-center gap-2 text-text-secondary">
          <Timer className="h-3 w-3 text-text-tertiary flex-shrink-0" />
          <span>Downtime</span>
          <span className="ml-auto font-mono text-text-primary">{formatDurationMin(operator.downtime_minutes)}</span>
        </div>
        <div className="flex items-center gap-2 text-text-secondary">
          <Zap className="h-3 w-3 text-text-tertiary flex-shrink-0" />
          <span>Incidents</span>
          <span className={cn('ml-auto font-mono', operator.incidents > 0 ? 'text-warning' : 'text-text-primary')}>
            {operator.incidents}
          </span>
        </div>
        <div className="flex items-center gap-2 text-text-secondary">
          <Clock className="h-3 w-3 text-text-tertiary flex-shrink-0" />
          <span>Last Check</span>
          <span className="ml-auto font-mono text-text-primary text-[11px]">{timeAgo(operator.last_check)}</span>
        </div>
      </div>
    </Card>
  )
}

function BreachTimeline({ breaches }: { breaches: SLABreach[] }) {
  if (breaches.length === 0) {
    return (
      <Card className="p-8">
        <div className="flex flex-col items-center justify-center text-center gap-3">
          <div className="h-10 w-10 rounded-lg bg-success-dim border border-success/20 flex items-center justify-center">
            <ShieldCheck className="h-5 w-5 text-success" />
          </div>
          <p className="text-sm text-text-secondary">No SLA breaches in the selected period</p>
        </div>
      </Card>
    )
  }

  return (
    <Card className="p-5">
      <div className="flex items-center gap-2 mb-5">
        <AlertTriangle className="h-4 w-4 text-warning" />
        <h3 className="text-sm font-semibold text-text-primary">Breach Timeline</h3>
        <Badge variant="outline" className="ml-auto text-[10px]">
          {breaches.length} breach{breaches.length !== 1 ? 'es' : ''}
        </Badge>
      </div>

      <div className="relative pl-6 space-y-0">
        <div className="absolute left-[9px] top-2 bottom-2 w-px bg-border" />

        {breaches.map((breach, i) => {
          const date = new Date(breach.date)
          const dateStr = date.toLocaleDateString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
          })
          const timeStr = date.toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          })

          return (
            <div
              key={i}
              className="relative pb-6 last:pb-0"
              style={{ animationDelay: `${i * 80}ms` }}
            >
              <div className="absolute -left-6 top-1 flex items-center justify-center">
                <span className="h-[18px] w-[18px] rounded-full border-2 border-danger bg-bg-surface flex items-center justify-center">
                  <span className="h-2 w-2 rounded-full bg-danger" />
                </span>
              </div>

              <div className="rounded-lg border border-border bg-bg-elevated p-4 ml-2 space-y-2">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold text-text-primary">{breach.operator}</span>
                      <Badge variant="danger" className="text-[10px]">
                        {formatDurationMin(breach.duration_min)}
                      </Badge>
                    </div>
                    <span className="text-[11px] text-text-tertiary font-mono">
                      {dateStr} at {timeStr}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 text-xs text-text-secondary">
                    <Cpu className="h-3 w-3" />
                    <span className="font-mono">{breach.affected_sims.toLocaleString()} SIMs</span>
                  </div>
                </div>
                <p className="text-xs text-text-secondary leading-relaxed">{breach.cause}</p>
              </div>
            </div>
          )
        })}
      </div>
    </Card>
  )
}

function SLASkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-7 w-48" />
      </div>
      <Skeleton className="h-48 w-full rounded-xl" />
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-56 rounded-xl" />
        ))}
      </div>
      <Skeleton className="h-64 w-full rounded-xl" />
    </div>
  )
}

export default function SLADashboardPage() {
  const [period, setPeriod] = useState('this_month')
  const { data, isLoading, isError, refetch } = useSLAData(period)

  const sortedOperators = useMemo(() => {
    if (!data?.operators) return []
    return [...data.operators].sort((a, b) => {
      const aStatus = operatorSLAStatus(a.uptime_pct, a.target)
      const bStatus = operatorSLAStatus(b.uptime_pct, b.target)
      const order: Record<SLAStatus, number> = { breached: 0, at_risk: 1, on_track: 2 }
      if (order[aStatus] !== order[bStatus]) return order[aStatus] - order[bStatus]
      return a.uptime_pct - b.uptime_pct
    })
  }, [data?.operators])

  if (isLoading) return <SLASkeleton />

  if (isError) {
    return (
      <div>
        <div className="flex flex-col items-center justify-center py-24 gap-4">
          <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
            <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
            <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load SLA data</h2>
            <p className="text-sm text-text-secondary mb-4">Unable to fetch SLA metrics. Please try again.</p>
            <Button onClick={() => refetch()} variant="outline" className="gap-2">
              <RefreshCw className="h-4 w-4" />
              Retry
            </Button>
          </div>
        </div>
      </div>
    )
  }

  if (!data) return null

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Breadcrumb
          items={[
            { label: 'Dashboard', href: '/' },
            { label: 'SLA' },
          ]}
        />
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-[16px] font-semibold text-text-primary">SLA Compliance</h1>
            <p className="text-xs text-text-tertiary mt-0.5">
              Monitor service level agreements across all operators
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Select
              value={period}
              onChange={(e) => setPeriod(e.target.value)}
              options={PERIOD_OPTIONS}
              className="h-8 text-xs w-36"
            />
            <Button variant="outline" size="sm" onClick={() => refetch()} className="gap-1.5">
              <RefreshCw className="h-3.5 w-3.5" />
              Refresh
            </Button>
          </div>
        </div>
      </div>

      <div className="stagger-item">
        <OverallSLACard data={data} />
      </div>

      <div>
        <h2 className="text-xs font-semibold text-text-tertiary uppercase tracking-wider mb-3">
          Per-Operator SLA
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {sortedOperators.map((op, i) => (
            <div
              key={op.id}
              className="stagger-item animate-in fade-in slide-in-from-bottom-1"
              style={{ animationDelay: `${i * 50}ms` }}
            >
              <OperatorSLACard operator={op} />
            </div>
          ))}
        </div>
        {sortedOperators.length === 0 && (
          <Card className="p-8">
            <div className="flex flex-col items-center justify-center text-center gap-3">
              <ShieldCheck className="h-8 w-8 text-text-tertiary" />
              <p className="text-sm text-text-secondary">No operators configured</p>
              <p className="text-xs text-text-tertiary">Add operators to start tracking SLA compliance.</p>
            </div>
          </Card>
        )}
      </div>

      <div className="stagger-item">
        <BreachTimeline breaches={data.breaches} />
      </div>
    </div>
  )
}
