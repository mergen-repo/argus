import { useMemo } from 'react'
import {
  RefreshCw,
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  TrendingUp,
  Wifi,
  Shield,
  HardDrive,
  Clock,
  ArrowRight,
} from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { Skeleton } from '@/components/ui/skeleton'
import { useCapacity } from '@/hooks/use-capacity'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/format'

interface PoolForecast {
  id: string
  name: string
  cidr: string
  total_addresses: number
  used_addresses: number
  available_addresses: number
  utilization_pct: number
  allocation_rate: number
  exhaustion_hours: number | null
}

interface CapacityData {
  ip_pools: PoolForecast[]
  overall_pool_utilization: number
  total_sims: number
  active_sessions: number
  auth_per_sec: number
  sim_capacity: number
  session_capacity: number
  auth_capacity: number
  monthly_growth: number
}

function useCapacityData() {
  const query = useCapacity()
  const data = useMemo<CapacityData | undefined>(() => {
    const d = query.data
    if (!d) return undefined
    const pools: PoolForecast[] = (d.ip_pools || []).map((p) => ({
      id: p.id,
      name: p.name,
      cidr: p.cidr,
      total_addresses: p.total,
      used_addresses: p.used,
      available_addresses: p.available,
      utilization_pct: p.utilization_pct,
      allocation_rate: p.allocation_rate,
      exhaustion_hours: p.exhaustion_hours,
    }))
    const totalUsed = pools.reduce((s, p) => s + p.used_addresses, 0)
    const totalAll = pools.reduce((s, p) => s + p.total_addresses, 0)
    return {
      ip_pools: pools,
      overall_pool_utilization: totalAll > 0 ? (totalUsed / totalAll) * 100 : 0,
      total_sims: d.total_sims,
      active_sessions: d.active_sessions,
      auth_per_sec: d.auth_per_sec,
      sim_capacity: d.sim_capacity,
      session_capacity: d.session_capacity,
      auth_capacity: d.auth_capacity,
      monthly_growth: d.monthly_growth_sims,
    }
  }, [query.data])
  return { ...query, data }
}

function CapacityBar({
  value,
  max,
  className,
}: {
  value: number
  max: number
  className?: string
}) {
  const pct = max > 0 ? (value / max) * 100 : 0
  const color = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'

  return (
    <div className={cn('w-full', className)}>
      <div className="w-full h-3 bg-bg-hover rounded-full overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-700', color)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
    </div>
  )
}

function PoolBar({ pool }: { pool: PoolForecast }) {
  const pct = pool.utilization_pct
  const color = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'
  const textColor = pct > 90 ? 'text-danger' : pct > 70 ? 'text-warning' : 'text-accent'

  return (
    <div className="flex items-center gap-3">
      <span className="font-mono text-xs text-text-secondary w-28 truncate flex-shrink-0">
        {pool.name}
      </span>
      <div className="flex-1 h-2 bg-bg-hover rounded-full overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-500', color)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <span className={cn('font-mono text-xs w-12 text-right flex-shrink-0', textColor)}>
        {pct.toFixed(0)}%
      </span>
      <div className="w-16 flex-shrink-0 text-right">
        {pool.exhaustion_hours != null ? (
          <Badge variant="warning" className="text-[10px] gap-1">
            <Clock className="h-2.5 w-2.5" />
            {pool.exhaustion_hours}h
          </Badge>
        ) : (
          <Badge variant="success" className="text-[10px]">OK</Badge>
        )}
      </div>
    </div>
  )
}

function IpPoolForecastCard({ data }: { data: CapacityData }) {
  const avgRate = data.ip_pools.length > 0
    ? Math.round(data.ip_pools.reduce((s, p) => s + p.allocation_rate, 0) / data.ip_pools.length)
    : 0

  const criticalPool = data.ip_pools
    .filter((p) => p.exhaustion_hours != null)
    .sort((a, b) => (a.exhaustion_hours ?? Infinity) - (b.exhaustion_hours ?? Infinity))[0]

  return (
    <Card className="card-hover col-span-full lg:col-span-2">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <div className="h-8 w-8 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center">
            <HardDrive className="h-4 w-4 text-accent" />
          </div>
          <CardTitle>IP Pool Forecast</CardTitle>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary">Overall</span>
          <span className={cn(
            'font-mono text-lg font-bold',
            data.overall_pool_utilization > 90 ? 'text-danger' :
            data.overall_pool_utilization > 70 ? 'text-warning' : 'text-accent',
          )}>
            <AnimatedCounter value={Math.round(data.overall_pool_utilization)} />%
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <CapacityBar
          value={data.overall_pool_utilization}
          max={100}
        />

        {data.ip_pools.length > 0 ? (
          <div className="space-y-2">
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary">Per-Pool Breakdown</span>
            <div className="space-y-2">
              {data.ip_pools.map((pool) => (
                <PoolBar key={pool.id} pool={pool} />
              ))}
            </div>
          </div>
        ) : (
          <div className="text-center py-4">
            <p className="text-xs text-text-tertiary">No IP pools configured</p>
          </div>
        )}

        <div className="flex items-center gap-6 pt-2 border-t border-border-subtle">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Allocation Rate</span>
            <span className="font-mono text-sm text-text-primary">+{avgRate} IPs/hour</span>
          </div>
          {criticalPool && (
            <div>
              <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Projected Exhaustion</span>
              <span className="font-mono text-sm text-danger">
                {criticalPool.name} in {criticalPool.exhaustion_hours}h
              </span>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function SimGrowthCard({ data }: { data: CapacityData }) {
  const projectionData = useMemo(() => {
    const now = new Date()
    const points = []
    for (let i = -3; i <= 6; i++) {
      const d = new Date(now.getFullYear(), now.getMonth() + i, 1)
      const label = d.toLocaleDateString('en-US', { month: 'short', year: '2-digit' })
      const sims = data.total_sims + (i * data.monthly_growth)
      points.push({
        label,
        sims: Math.max(0, sims),
        projected: i > 0,
      })
    }
    return points
  }, [data.total_sims, data.monthly_growth])

  const pct = (data.total_sims / data.sim_capacity) * 100

  return (
    <Card className="card-hover stagger-item">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <div className="h-8 w-8 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center">
            <TrendingUp className="h-4 w-4 text-accent" />
          </div>
          <CardTitle>SIM Growth</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-end justify-between">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Current</span>
            <span className="font-mono text-2xl font-bold text-text-primary">
              <AnimatedCounter value={data.total_sims} formatter={formatNumber} />
            </span>
          </div>
          <div className="text-right">
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">30-day Trend</span>
            <span className="font-mono text-sm text-success">+{formatNumber(data.monthly_growth)}/mo</span>
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="font-mono text-[11px] text-text-secondary">
              {formatNumber(data.total_sims)} / {formatNumber(data.sim_capacity)}
            </span>
            <span className="font-mono text-[11px] text-text-tertiary">{pct.toFixed(1)}%</span>
          </div>
          <CapacityBar value={data.total_sims} max={data.sim_capacity} />
        </div>

        <div className="h-[140px]">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={projectionData}>
              <defs>
                <linearGradient id="gradSims" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <XAxis
                dataKey="label"
                tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                tickLine={false}
                axisLine={false}
                interval={1}
              />
              <YAxis
                tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                tickLine={false}
                axisLine={false}
                tickFormatter={(v) => formatNumber(v)}
                width={45}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: 'var(--color-bg-elevated)',
                  border: '1px solid var(--color-border)',
                  borderRadius: 'var(--radius-sm)',
                  color: 'var(--color-text-primary)',
                  fontSize: '12px',
                }}
                formatter={(value) => [formatNumber(Number(value)), 'SIMs']}
              />
              <Area
                type="monotone"
                dataKey="sims"
                stroke="var(--color-accent)"
                fill="url(#gradSims)"
                strokeWidth={2}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      </CardContent>
    </Card>
  )
}

function SessionCapacityCard({ data }: { data: CapacityData }) {
  const headroom = ((data.session_capacity - data.active_sessions) / data.session_capacity) * 100
  const pct = (data.active_sessions / data.session_capacity) * 100

  return (
    <Card className="card-hover stagger-item">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <div className="h-8 w-8 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center">
            <Wifi className="h-4 w-4 text-accent" />
          </div>
          <CardTitle>Session Capacity</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-3 gap-3 text-center">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Peak Concurrent</span>
            <span className="font-mono text-lg font-bold text-text-primary">
              <AnimatedCounter value={data.active_sessions} formatter={formatNumber} />
            </span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">System Max</span>
            <span className="font-mono text-lg font-bold text-text-secondary">
              {formatNumber(data.session_capacity)}
            </span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Headroom</span>
            <span className={cn(
              'font-mono text-lg font-bold',
              headroom > 50 ? 'text-success' : headroom > 25 ? 'text-warning' : 'text-danger',
            )}>
              {headroom.toFixed(0)}%
            </span>
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="font-mono text-[11px] text-text-secondary">
              {formatNumber(data.active_sessions)} / {formatNumber(data.session_capacity)}
            </span>
            <span className="font-mono text-[11px] text-text-tertiary">{pct.toFixed(1)}%</span>
          </div>
          <CapacityBar value={data.active_sessions} max={data.session_capacity} />
        </div>

        <div className="flex items-center gap-2 text-xs text-text-tertiary pt-1 border-t border-border-subtle">
          <Clock className="h-3 w-3" />
          <span>Peak observed during business hours (09:00-18:00 UTC)</span>
        </div>
      </CardContent>
    </Card>
  )
}

function AuthThroughputCard({ data }: { data: CapacityData }) {
  const headroom = ((data.auth_capacity - data.auth_per_sec) / data.auth_capacity) * 100
  const pct = (data.auth_per_sec / data.auth_capacity) * 100

  return (
    <Card className="card-hover stagger-item">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <div className="h-8 w-8 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center">
            <Shield className="h-4 w-4 text-accent" />
          </div>
          <CardTitle>Auth Throughput</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-3 gap-3 text-center">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Peak Auth/s</span>
            <span className="font-mono text-lg font-bold text-text-primary">
              <AnimatedCounter value={data.auth_per_sec} formatter={(n) => n.toLocaleString()} />
            </span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">System Max</span>
            <span className="font-mono text-lg font-bold text-text-secondary">
              {data.auth_capacity.toLocaleString()}/s
            </span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Headroom</span>
            <span className={cn(
              'font-mono text-lg font-bold',
              headroom > 50 ? 'text-success' : headroom > 25 ? 'text-warning' : 'text-danger',
            )}>
              {headroom.toFixed(0)}%
            </span>
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="font-mono text-[11px] text-text-secondary">
              {data.auth_per_sec.toLocaleString()} / {data.auth_capacity.toLocaleString()}
            </span>
            <span className="font-mono text-[11px] text-text-tertiary">{pct.toFixed(1)}%</span>
          </div>
          <CapacityBar value={data.auth_per_sec} max={data.auth_capacity} />
        </div>

        <div className="flex items-center gap-2 text-xs text-text-tertiary pt-1 border-t border-border-subtle">
          <Shield className="h-3 w-3" />
          <span>RADIUS + Diameter + 5G SBA combined</span>
        </div>
      </CardContent>
    </Card>
  )
}

interface Recommendation {
  id: string
  title: string
  description: string
  severity: 'danger' | 'warning' | 'default'
  action: string
}

function buildRecommendations(data: CapacityData): Recommendation[] {
  const recs: Recommendation[] = []

  const criticalPools = data.ip_pools
    .filter((p) => p.exhaustion_hours != null)
    .sort((a, b) => (a.exhaustion_hours ?? Infinity) - (b.exhaustion_hours ?? Infinity))

  for (const pool of criticalPools) {
    recs.push({
      id: `pool-${pool.id}`,
      title: `Expand ${pool.name} IP pool`,
      description: `Projected exhaustion in ${pool.exhaustion_hours} hours at current allocation rate of ${pool.allocation_rate} IPs/hour.`,
      severity: (pool.exhaustion_hours ?? 0) < 12 ? 'danger' : 'warning',
      action: 'Expand Pool',
    })
  }

  const sessionPct = (data.active_sessions / data.session_capacity) * 100
  if (sessionPct > 70) {
    recs.push({
      id: 'session-cap',
      title: 'Session capacity approaching threshold',
      description: `Peak concurrent sessions at ${sessionPct.toFixed(0)}% of system maximum. Consider scaling session infrastructure.`,
      severity: sessionPct > 90 ? 'danger' : 'warning',
      action: 'Review Config',
    })
  }

  const authPct = (data.auth_per_sec / data.auth_capacity) * 100
  if (authPct > 60) {
    recs.push({
      id: 'auth-cap',
      title: 'Auth throughput headroom narrowing',
      description: `Peak authentication rate at ${authPct.toFixed(0)}% of capacity. Consider adding NB-IoT capacity.`,
      severity: authPct > 80 ? 'danger' : 'warning',
      action: 'Scale AAA',
    })
  }

  const simPct = (data.total_sims / data.sim_capacity) * 100
  if (simPct > 60) {
    recs.push({
      id: 'sim-growth',
      title: 'SIM capacity planning needed',
      description: `At current growth rate of +${formatNumber(data.monthly_growth)}/month, system capacity will be reached in ~${Math.round((data.sim_capacity - data.total_sims) / data.monthly_growth)} months.`,
      severity: simPct > 85 ? 'danger' : 'warning',
      action: 'Plan Expansion',
    })
  }

  if (recs.length === 0) {
    recs.push({
      id: 'all-good',
      title: 'All systems within capacity',
      description: 'No immediate capacity concerns detected. All metrics are within healthy thresholds.',
      severity: 'default',
      action: 'View Details',
    })
  }

  return recs
}

function RecommendationsSection({ data }: { data: CapacityData }) {
  const recommendations = useMemo(() => buildRecommendations(data), [data])

  return (
    <div className="space-y-3">
      <h2 className="text-sm font-semibold text-text-primary">Recommendations</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {recommendations.map((rec) => (
          <Card
            key={rec.id}
            className={cn(
              'card-hover relative overflow-hidden',
              rec.severity === 'danger' && 'border-danger/30',
              rec.severity === 'warning' && 'border-warning/30',
            )}
          >
            <div
              className={cn(
                'absolute bottom-0 left-0 right-0 h-[2px]',
                rec.severity === 'danger' ? 'bg-danger' :
                rec.severity === 'warning' ? 'bg-warning' : 'bg-accent',
              )}
            />
            <CardContent className="p-4">
              <div className="flex items-start gap-3">
                <div className="flex-shrink-0 mt-0.5">
                  {rec.severity === 'danger' ? (
                    <AlertCircle className="h-4 w-4 text-danger" />
                  ) : rec.severity === 'warning' ? (
                    <AlertTriangle className="h-4 w-4 text-warning" />
                  ) : (
                    <CheckCircle2 className="h-4 w-4 text-success" />
                  )}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-sm font-medium text-text-primary">{rec.title}</span>
                    <Badge
                      variant={rec.severity === 'default' ? 'success' : rec.severity}
                      className="text-[10px]"
                    >
                      {rec.severity === 'danger' ? 'CRITICAL' : rec.severity === 'warning' ? 'WARNING' : 'HEALTHY'}
                    </Badge>
                  </div>
                  <p className="text-xs text-text-secondary mb-3">{rec.description}</p>
                  <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs">
                    {rec.action}
                    <ArrowRight className="h-3 w-3" />
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-4 w-40" />
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card className="col-span-full lg:col-span-2">
          <CardContent className="p-4 space-y-4">
            <Skeleton className="h-5 w-40" />
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-20 w-full" />
          </CardContent>
        </Card>
        {Array.from({ length: 3 }).map((_, i) => (
          <Card key={i}>
            <CardContent className="p-4 space-y-4">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-3 w-full" />
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

export default function CapacityPage() {
  const { data, isLoading, isError, refetch, isFetching } = useCapacityData()

  if (isLoading) return <LoadingSkeleton />

  if (isError || !data) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load capacity data</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch capacity metrics.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Breadcrumb
          items={[
            { label: 'Dashboard', href: '/' },
            { label: 'Capacity' },
          ]}
        />
        <div className="flex items-center justify-between">
          <h1 className="text-[16px] font-semibold text-text-primary">Capacity Planning</h1>
          <Button
            variant="outline"
            size="sm"
            className="gap-2"
            onClick={() => refetch()}
            disabled={isFetching}
          >
            <RefreshCw className={cn('h-3.5 w-3.5', isFetching && 'animate-spin')} />
            Refresh
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <IpPoolForecastCard data={data} />
        <SimGrowthCard data={data} />
        <SessionCapacityCard data={data} />
        <AuthThroughputCard data={data} />
      </div>

      <RecommendationsSection data={data} />
    </div>
  )
}
