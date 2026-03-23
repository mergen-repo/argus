import {
  AlertCircle,
  RefreshCw,
  Database,
  HardDrive,
  Radio,
  Shield,
  Activity,
} from 'lucide-react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { useSystemMetrics, useHealthCheck, useRealtimeMetrics } from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { useState, useEffect, useRef } from 'react'

const SERVICE_ICONS: Record<string, React.ElementType> = {
  postgres: Database,
  postgresql: Database,
  db: Database,
  redis: HardDrive,
  nats: Radio,
  aaa: Shield,
}

function serviceIcon(name: string): React.ElementType {
  const key = name.toLowerCase()
  for (const [k, v] of Object.entries(SERVICE_ICONS)) {
    if (key.includes(k)) return v
  }
  return Activity
}

function statusColor(status: string) {
  switch (status) {
    case 'healthy': return 'var(--color-success)'
    case 'degraded': return 'var(--color-warning)'
    case 'down': return 'var(--color-danger)'
    default: return 'var(--color-text-tertiary)'
  }
}

function statusGlow(status: string) {
  switch (status) {
    case 'healthy': return '0 0 12px rgba(0,255,136,0.3)'
    case 'degraded': return '0 0 12px rgba(255,184,0,0.3)'
    case 'down': return '0 0 12px rgba(255,68,102,0.3)'
    default: return 'none'
  }
}

function GaugeChart({ value, max, label, unit, color }: { value: number; max: number; label: string; unit: string; color: string }) {
  const pct = Math.min((value / max) * 100, 100)
  const circumference = 2 * Math.PI * 45
  const strokeDashoffset = circumference - (pct / 100) * circumference * 0.75

  return (
    <div className="flex flex-col items-center">
      <div className="relative w-32 h-32">
        <svg viewBox="0 0 100 100" className="w-full h-full -rotate-135">
          <circle
            cx="50"
            cy="50"
            r="45"
            fill="none"
            stroke="var(--color-bg-hover)"
            strokeWidth="6"
            strokeDasharray={`${circumference * 0.75} ${circumference * 0.25}`}
            strokeLinecap="round"
          />
          <circle
            cx="50"
            cy="50"
            r="45"
            fill="none"
            stroke={color}
            strokeWidth="6"
            strokeDasharray={`${circumference * 0.75} ${circumference * 0.25}`}
            strokeDashoffset={strokeDashoffset}
            strokeLinecap="round"
            className="transition-all duration-500"
            style={{ filter: `drop-shadow(0 0 4px ${color}40)` }}
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="font-mono text-xl font-bold text-text-primary">{value.toLocaleString()}</span>
          <span className="text-[10px] text-text-tertiary">{unit}</span>
        </div>
      </div>
      <span className="text-xs text-text-secondary mt-1">{label}</span>
    </div>
  )
}

interface LatencyPoint {
  time: string
  p50: number
  p95: number
  p99: number
}

export default function SystemHealthPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: metrics, isLoading, isError, refetch } = useSystemMetrics()
  const { data: healthData } = useHealthCheck()
  useRealtimeMetrics()

  const services = metrics?.services ?? (() => {
    if (!healthData) return []
    const h = healthData as Record<string, unknown>
    const result: { name: string; status: string; latency_ms: number }[] = []
    for (const [key, val] of Object.entries(h)) {
      if (key === 'uptime' || key === 'services') continue
      if (typeof val === 'string') {
        result.push({ name: key, status: val === 'ok' ? 'healthy' : 'down', latency_ms: 0 })
      } else if (typeof val === 'object' && val !== null) {
        const obj = val as Record<string, unknown>
        const allOk = Object.values(obj).every((v) => typeof v !== 'string' || v === 'ok')
        result.push({ name: key, status: allOk ? 'healthy' : 'degraded', latency_ms: 0 })
      }
    }
    return result
  })()

  const [latencyHistory, setLatencyHistory] = useState<LatencyPoint[]>([])
  const historyRef = useRef(latencyHistory)
  historyRef.current = latencyHistory

  useEffect(() => {
    if (!metrics?.latency) return
    const now = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    const newPoint: LatencyPoint = {
      time: now,
      p50: metrics.latency.p50,
      p95: metrics.latency.p95,
      p99: metrics.latency.p99,
    }
    setLatencyHistory((prev) => [...prev.slice(-29), newPoint])
  }, [metrics?.latency])

  if (!isSuperAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <Shield className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Access Denied</h2>
          <p className="text-sm text-text-secondary">You need super_admin role to view system health.</p>
        </div>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load metrics</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch system health data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  if (isLoading || !metrics) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-6 w-40" />
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardHeader className="pb-2"><Skeleton className="h-4 w-20" /></CardHeader>
              <CardContent className="pt-0"><Skeleton className="h-24 w-full" /></CardContent>
            </Card>
          ))}
        </div>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card><CardContent className="p-4"><Skeleton className="h-48 w-full" /></CardContent></Card>
          <Card><CardContent className="p-4"><Skeleton className="h-48 w-full" /></CardContent></Card>
        </div>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-3">
          <h1 className="text-[16px] font-semibold text-text-primary">System Health</h1>
          <span className="flex items-center gap-1">
            <span className="h-1.5 w-1.5 rounded-full bg-accent pulse-dot" style={{ boxShadow: '0 0 6px rgba(0,212,255,0.4)' }} />
            <span className="text-[10px] text-text-tertiary">LIVE</span>
          </span>
        </div>
      </div>

      {/* Service Status Cards */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Service Status
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          {services.map((svc) => {
            const Icon = serviceIcon(svc.name)
            return (
              <Card key={svc.name} className="relative overflow-hidden">
                <div
                  className="absolute bottom-0 left-0 right-0 h-[2px]"
                  style={{ backgroundColor: statusColor(svc.status) }}
                />
                <CardContent className="p-4 flex items-center gap-4">
                  <div
                    className="h-10 w-10 rounded-[var(--radius-sm)] flex items-center justify-center flex-shrink-0"
                    style={{
                      backgroundColor: `${statusColor(svc.status)}15`,
                      color: statusColor(svc.status),
                    }}
                  >
                    <Icon className="h-5 w-5" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium text-text-primary block capitalize">{svc.name}</span>
                    <div className="flex items-center gap-2 mt-0.5">
                      <span
                        className="h-2 w-2 rounded-full pulse-dot"
                        style={{
                          backgroundColor: statusColor(svc.status),
                          boxShadow: statusGlow(svc.status),
                        }}
                      />
                      <span
                        className="text-xs font-medium capitalize"
                        style={{ color: statusColor(svc.status) }}
                      >
                        {svc.status}
                      </span>
                    </div>
                  </div>
                  <div className="text-right">
                    <span className="font-mono text-xs text-text-secondary">{svc.latency_ms}ms</span>
                  </div>
                </CardContent>
              </Card>
            )
          })}
        </div>
      </div>

      {/* Gauges */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Real-Time Metrics
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <Card>
            <CardContent className="p-6 flex justify-center">
              <GaugeChart
                value={Math.round(metrics.auth_per_sec)}
                max={1000}
                label="Auth/s"
                unit="req/s"
                color="var(--color-accent)"
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent className="p-6 flex justify-center">
              <GaugeChart
                value={metrics.active_sessions}
                max={100000}
                label="Active Sessions"
                unit="sessions"
                color="var(--color-success)"
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent className="p-6 flex justify-center">
              <GaugeChart
                value={parseFloat((metrics.error_rate ?? metrics.auth_error_rate ?? 0).toFixed(2))}
                max={10}
                label="Error Rate"
                unit="%"
                color={(metrics.error_rate ?? metrics.auth_error_rate ?? 0) > 5 ? 'var(--color-danger)' : (metrics.error_rate ?? metrics.auth_error_rate ?? 0) > 2 ? 'var(--color-warning)' : 'var(--color-success)'}
              />
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Latency Chart */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Latency (ms)
        </h2>
        <Card>
          <CardContent className="p-4">
            {latencyHistory.length < 2 ? (
              <div className="flex items-center justify-center h-[240px] text-text-tertiary text-sm">
                Collecting latency data...
              </div>
            ) : (
              <div className="h-[240px]">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={latencyHistory}>
                    <XAxis
                      dataKey="time"
                      tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
                      tickLine={false}
                      axisLine={{ stroke: 'var(--color-border)' }}
                    />
                    <YAxis
                      tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
                      tickLine={false}
                      axisLine={{ stroke: 'var(--color-border)' }}
                      width={40}
                    />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: 'var(--color-bg-elevated)',
                        border: '1px solid var(--color-border)',
                        borderRadius: 'var(--radius-sm)',
                        color: 'var(--color-text-primary)',
                        fontSize: '12px',
                      }}
                      formatter={(value) => [`${value}ms`]}
                    />
                    <Legend
                      wrapperStyle={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}
                    />
                    <Line
                      type="monotone"
                      dataKey="p50"
                      stroke="var(--color-success)"
                      strokeWidth={2}
                      dot={false}
                      name="p50"
                    />
                    <Line
                      type="monotone"
                      dataKey="p95"
                      stroke="var(--color-warning)"
                      strokeWidth={2}
                      dot={false}
                      name="p95"
                    />
                    <Line
                      type="monotone"
                      dataKey="p99"
                      stroke="var(--color-danger)"
                      strokeWidth={2}
                      dot={false}
                      name="p99"
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
