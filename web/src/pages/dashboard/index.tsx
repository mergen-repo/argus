import { useNavigate } from 'react-router-dom'
import { AlertTriangle, Activity, Cpu, DollarSign, RefreshCw, AlertCircle, Info } from 'lucide-react'
import {
  PieChart, Pie, Cell, ResponsiveContainer,
  BarChart, Bar, XAxis, YAxis, Tooltip,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useDashboard, useRealtimeAuthPerSec, useRealtimeAlerts } from '@/hooks/use-dashboard'
import type { DashboardAlert, OperatorHealth, TopAPN, SIMByState } from '@/types/dashboard'

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

function formatCurrency(n: number): string {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: 0 })}`
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

function Sparkline({ color }: { color: string }) {
  const bars = Array.from({ length: 12 }, () => 20 + Math.random() * 80)
  const max = Math.max(...bars)
  return (
    <div className="flex items-end gap-[2px] h-6">
      {bars.map((v, i) => (
        <div
          key={i}
          className="w-[4px] rounded-t-[2px]"
          style={{
            height: `${(v / max) * 100}%`,
            backgroundColor: color,
            opacity: 0.3 + (v / max) * 0.7,
          }}
        />
      ))}
    </div>
  )
}

interface MetricCardProps {
  title: string
  value: string
  icon: React.ReactNode
  color: string
  sparkColor: string
  live?: boolean
  onClick?: () => void
}

function MetricCard({ title, value, icon, color, sparkColor, live, onClick }: MetricCardProps) {
  return (
    <Card
      className="card-hover cursor-pointer relative overflow-hidden"
      onClick={onClick}
    >
      <div className="absolute bottom-0 left-0 right-0 h-[2px]" style={{ backgroundColor: color }} />
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
          {title}
        </span>
        <div className="flex items-center gap-2">
          {live && (
            <span className="flex items-center gap-1">
              <span className="h-1.5 w-1.5 rounded-full pulse-dot" style={{ backgroundColor: color, boxShadow: `0 0 6px ${color}60` }} />
              <span className="text-[10px] text-text-tertiary">LIVE</span>
            </span>
          )}
          <span style={{ color }} className="opacity-70">{icon}</span>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="font-mono text-[28px] font-bold text-text-primary leading-none mb-3">
          {value}
        </div>
        <Sparkline color={sparkColor} />
      </CardContent>
    </Card>
  )
}

function MetricCardSkeleton() {
  return (
    <Card className="relative overflow-hidden">
      <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-bg-hover" />
      <CardHeader className="pb-2">
        <Skeleton className="h-3 w-20" />
      </CardHeader>
      <CardContent className="pt-0">
        <Skeleton className="h-8 w-28 mb-3" />
        <Skeleton className="h-6 w-full" />
      </CardContent>
    </Card>
  )
}

const STATE_COLORS: Record<string, string> = {
  active: 'var(--color-success)',
  suspended: 'var(--color-warning)',
  ordered: 'var(--color-accent)',
  terminated: 'var(--color-danger)',
  lost: 'var(--color-purple)',
}

function SIMDistributionChart({ data }: { data: SIMByState[] }) {
  const chartData = data.map((d) => ({
    name: d.state.charAt(0).toUpperCase() + d.state.slice(1),
    value: d.count,
    fill: STATE_COLORS[d.state] ?? 'var(--color-text-tertiary)',
  }))

  return (
    <Card className="card-hover">
      <CardHeader>
        <CardTitle>SIM Distribution</CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[200px] text-text-tertiary text-sm">
            No SIM data available
          </div>
        ) : (
          <div className="flex items-center gap-6">
            <div className="w-[180px] h-[180px]">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={chartData}
                    dataKey="value"
                    cx="50%"
                    cy="50%"
                    innerRadius={50}
                    outerRadius={80}
                    paddingAngle={2}
                    strokeWidth={0}
                  >
                    {chartData.map((entry, idx) => (
                      <Cell key={idx} fill={entry.fill} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      backgroundColor: 'var(--color-bg-elevated)',
                      border: '1px solid var(--color-border)',
                      borderRadius: 'var(--radius-sm)',
                      color: 'var(--color-text-primary)',
                      fontSize: '12px',
                    }}
                    formatter={(value) => [formatNumber(Number(value)), 'Count']}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="flex flex-col gap-2 flex-1">
              {chartData.map((entry, idx) => (
                <div key={idx} className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: entry.fill }} />
                    <span className="text-text-secondary">{entry.name}</span>
                  </div>
                  <span className="font-mono text-xs text-text-primary">{formatNumber(entry.value)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function OperatorHealthBars({ data }: { data: OperatorHealth[] }) {
  const statusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'var(--color-success)'
      case 'degraded': return 'var(--color-warning)'
      case 'down': return 'var(--color-danger)'
      default: return 'var(--color-text-tertiary)'
    }
  }

  const statusGlow = (status: string) => {
    switch (status) {
      case 'healthy': return '0 0 6px rgba(0,255,136,0.4)'
      case 'degraded': return '0 0 6px rgba(255,184,0,0.4)'
      case 'down': return '0 0 6px rgba(255,68,102,0.4)'
      default: return 'none'
    }
  }

  const navigate = useNavigate()

  return (
    <Card className="card-hover">
      <CardHeader>
        <CardTitle>Operator Health</CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No operators configured
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {data.map((op) => (
              <div
                key={op.id}
                className="flex items-center gap-3 cursor-pointer hover:bg-bg-hover rounded-[var(--radius-sm)] p-2 -mx-2 transition-colors"
                onClick={() => navigate(`/operators/${op.id}`)}
              >
                <span
                  className="h-2 w-2 rounded-full flex-shrink-0 pulse-dot"
                  style={{
                    backgroundColor: statusColor(op.status),
                    boxShadow: statusGlow(op.status),
                  }}
                />
                <span className="text-sm text-text-primary flex-1 truncate">{op.name}</span>
                <span className="font-mono text-xs text-text-secondary w-12 text-right">
                  {op.health_pct.toFixed(1)}%
                </span>
                <div className="w-24 h-2 bg-bg-hover rounded-full overflow-hidden">
                  <div
                    className="h-full rounded-full transition-all"
                    style={{
                      width: `${op.health_pct}%`,
                      backgroundColor: statusColor(op.status),
                    }}
                  />
                </div>
              </div>
            ))}
            <p className="text-[10px] text-text-tertiary mt-1">Last 24h uptime</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function APNTrafficBars({ data }: { data: TopAPN[] }) {
  const chartData = data.map((d) => ({
    name: d.name === 'none' ? 'No APN' : d.name,
    sessions: d.session_count,
  }))

  return (
    <Card className="card-hover">
      <CardHeader>
        <CardTitle>Top 5 APNs by Traffic</CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No active sessions
          </div>
        ) : (
          <div className="h-[200px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart
                data={chartData}
                layout="vertical"
                margin={{ left: 0, right: 16, top: 0, bottom: 0 }}
              >
                <XAxis type="number" hide />
                <YAxis
                  type="category"
                  dataKey="name"
                  width={100}
                  tick={{ fill: 'var(--color-text-secondary)', fontSize: 12, fontFamily: 'var(--font-mono)' }}
                  tickLine={false}
                  axisLine={false}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--color-bg-elevated)',
                    border: '1px solid var(--color-border)',
                    borderRadius: 'var(--radius-sm)',
                    color: 'var(--color-text-primary)',
                    fontSize: '12px',
                  }}
                  formatter={(value) => [formatNumber(Number(value)), 'Sessions']}
                />
                <Bar
                  dataKey="sessions"
                  fill="var(--color-accent)"
                  radius={[0, 4, 4, 0]}
                  barSize={16}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function severityIcon(severity: string) {
  switch (severity) {
    case 'critical':
      return <AlertCircle className="h-4 w-4 text-danger flex-shrink-0" />
    case 'warning':
      return <AlertTriangle className="h-4 w-4 text-warning flex-shrink-0" />
    default:
      return <Info className="h-4 w-4 text-info flex-shrink-0" />
  }
}

function severityVariant(severity: string): 'danger' | 'warning' | 'default' {
  switch (severity) {
    case 'critical': return 'danger'
    case 'warning': return 'warning'
    default: return 'default'
  }
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function AlertFeed({ alerts }: { alerts: DashboardAlert[] }) {
  const navigate = useNavigate()

  return (
    <Card className="card-hover">
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>Alert Feed</CardTitle>
        <span className="flex items-center gap-1">
          <span className="h-1.5 w-1.5 rounded-full bg-danger pulse-dot" style={{ boxShadow: '0 0 6px rgba(255,68,102,0.4)' }} />
          <span className="text-[10px] text-text-tertiary">LIVE</span>
        </span>
      </CardHeader>
      <CardContent>
        {alerts.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No recent alerts
          </div>
        ) : (
          <div className="flex flex-col gap-1 max-h-[280px] overflow-y-auto">
            {alerts.map((alert, idx) => (
              <div
                key={alert.id || idx}
                className="flex items-start gap-3 p-2 rounded-[var(--radius-sm)] hover:bg-bg-hover cursor-pointer transition-colors"
                style={{ animationDelay: `${idx * 50}ms` }}
                onClick={() => navigate(`/analytics/anomalies`)}
              >
                {severityIcon(alert.severity)}
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text-primary truncate">{alert.message}</p>
                  <p className="text-[11px] text-text-tertiary mt-0.5">{timeAgo(alert.detected_at)}</p>
                </div>
                <Badge variant={severityVariant(alert.severity)} className="text-[10px] flex-shrink-0">
                  {alert.severity}
                </Badge>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load dashboard</h2>
        <p className="text-sm text-text-secondary mb-4">Unable to fetch dashboard data. Please try again.</p>
        <Button onClick={onRetry} variant="outline" className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    </div>
  )
}

function DashboardSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <MetricCardSkeleton key={i} />
        ))}
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i}>
            <CardHeader>
              <Skeleton className="h-4 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[180px] w-full" />
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

export default function DashboardPage() {
  const { data, isLoading, isError, refetch } = useDashboard()
  useRealtimeAuthPerSec()
  useRealtimeAlerts()

  const navigate = useNavigate()

  if (isLoading) return <DashboardSkeleton />
  if (isError || !data) return <ErrorState onRetry={() => refetch()} />

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Dashboard</h1>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="Total SIMs"
          value={formatNumber(data.total_sims)}
          icon={<Cpu className="h-4 w-4" />}
          color="var(--color-accent)"
          sparkColor="var(--color-accent)"
          onClick={() => navigate('/sims')}
        />
        <MetricCard
          title="Active Sessions"
          value={formatNumber(data.active_sessions)}
          icon={<Activity className="h-4 w-4" />}
          color="var(--color-success)"
          sparkColor="var(--color-success)"
          onClick={() => navigate('/sessions')}
        />
        <MetricCard
          title="Auth/s"
          value={formatNumber(Math.round(data.auth_per_sec))}
          icon={<Activity className="h-4 w-4" />}
          color="var(--color-purple)"
          sparkColor="var(--color-purple)"
          live
          onClick={() => navigate('/system/health')}
        />
        <MetricCard
          title="Monthly Cost"
          value={formatCurrency(data.monthly_cost)}
          icon={<DollarSign className="h-4 w-4" />}
          color="var(--color-warning)"
          sparkColor="var(--color-warning)"
          onClick={() => navigate('/analytics/cost')}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <SIMDistributionChart data={data.sim_by_state} />
        <OperatorHealthBars data={data.operator_health} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <APNTrafficBars data={data.top_apns} />
        <AlertFeed alerts={data.recent_alerts} />
      </div>
    </div>
  )
}
