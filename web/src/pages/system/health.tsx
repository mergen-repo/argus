import {
  AlertCircle,
  RefreshCw,
  Database,
  HardDrive,
  Radio,
  Shield,
  Activity,
  Heart,
  CheckCircle2,
  Zap,
  ChevronRight,
  Archive,
  ServerCrash,
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
import { Badge } from '@/components/ui/badge'
import {
  useSystemMetrics,
  useHealthCheck,
  useRealtimeMetrics,
  useHealthLive,
  useHealthReady,
  useBackupStatus,
} from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'

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
    case 'healthy':
    case 'ok':
    case 'alive':
      return 'var(--color-success)'
    case 'degraded':
      return 'var(--color-warning)'
    case 'down':
    case 'unhealthy':
    case 'error':
      return 'var(--color-danger)'
    default:
      return 'var(--color-text-tertiary)'
  }
}

function statusGlow(status: string) {
  switch (status) {
    case 'healthy':
    case 'ok':
    case 'alive':
      return '0 0 12px rgba(0,255,136,0.3)'
    case 'degraded':
      return '0 0 12px rgba(255,184,0,0.3)'
    case 'down':
    case 'unhealthy':
      return '0 0 12px rgba(255,68,102,0.3)'
    default:
      return 'none'
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

function ProbeCards() {
  const { data: liveData, isLoading: liveLoading } = useHealthLive()
  const { data: readyData, isLoading: readyLoading } = useHealthReady()

  const depTotal = readyData
    ? [readyData.db, readyData.redis, readyData.nats].length
    : 3
  const depOk = readyData
    ? [readyData.db, readyData.redis, readyData.nats].filter((d) => d.status === 'ok').length
    : 0

  const readyState = readyData?.state ?? 'unknown'
  const uptime = liveData?.uptime ?? readyData?.uptime ?? '–'
  const goroutines = liveData?.goroutines ?? 0

  return (
    <div>
      <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
        Health Probes
      </h2>
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {/* Liveness */}
        <Card className="relative overflow-hidden">
          <div className="absolute top-0 left-0 right-0 h-[2px]" style={{ backgroundColor: 'var(--color-success)' }} />
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-3">
              <Heart className="h-3.5 w-3.5" style={{ color: 'var(--color-success)' }} />
              <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Liveness</span>
            </div>
            {liveLoading ? (
              <Skeleton className="h-8 w-24" />
            ) : (
              <>
                <div className="flex items-center gap-2 mb-1">
                  <span
                    className="h-2 w-2 rounded-full pulse-dot flex-shrink-0"
                    style={{ backgroundColor: 'var(--color-success)', boxShadow: statusGlow('ok') }}
                  />
                  <span className="font-mono text-sm font-semibold" style={{ color: 'var(--color-success)' }}>alive</span>
                </div>
                <div className="font-mono text-[11px] text-text-tertiary">
                  {goroutines.toLocaleString()} goroutines
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Readiness */}
        <Card className="relative overflow-hidden">
          <div
            className="absolute top-0 left-0 right-0 h-[2px]"
            style={{ backgroundColor: statusColor(readyState) }}
          />
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-3">
              <CheckCircle2 className="h-3.5 w-3.5 text-text-tertiary" />
              <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Readiness</span>
            </div>
            {readyLoading ? (
              <Skeleton className="h-8 w-24" />
            ) : (
              <>
                <div className="flex items-center gap-2 mb-1">
                  <span
                    className="h-2 w-2 rounded-full pulse-dot flex-shrink-0"
                    style={{ backgroundColor: statusColor(readyState), boxShadow: statusGlow(readyState) }}
                  />
                  <span
                    className="font-mono text-sm font-semibold capitalize"
                    style={{ color: statusColor(readyState) }}
                  >
                    {readyState}
                  </span>
                </div>
                <div className="font-mono text-[11px] text-text-tertiary">
                  {depOk}/{depTotal} deps ok
                </div>
                {readyData && (
                  <div className="mt-2 space-y-0.5">
                    {[
                      { name: 'DB', probe: readyData.db },
                      { name: 'Redis', probe: readyData.redis },
                      { name: 'NATS', probe: readyData.nats },
                    ].map(({ name, probe }) => (
                      <div key={name} className="flex items-center justify-between">
                        <span className="text-[10px] text-text-tertiary">{name}</span>
                        <div className="flex items-center gap-1.5">
                          <span
                            className="h-1.5 w-1.5 rounded-full flex-shrink-0"
                            style={{ backgroundColor: statusColor(probe.status) }}
                          />
                          <span className="font-mono text-[10px] text-text-tertiary">{probe.latency_ms}ms</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>

        {/* Startup */}
        <Card className="relative overflow-hidden">
          <div className="absolute top-0 left-0 right-0 h-[2px]" style={{ backgroundColor: 'var(--color-accent)' }} />
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-3">
              <Zap className="h-3.5 w-3.5 text-text-tertiary" />
              <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Startup</span>
            </div>
            {liveLoading ? (
              <Skeleton className="h-8 w-24" />
            ) : (
              <>
                <div className="flex items-center gap-2 mb-1">
                  <span
                    className="h-2 w-2 rounded-full flex-shrink-0"
                    style={{ backgroundColor: 'var(--color-accent)' }}
                  />
                  <span className="font-mono text-sm font-semibold" style={{ color: 'var(--color-accent)' }}>
                    started
                  </span>
                </div>
                <div className="font-mono text-[11px] text-text-tertiary">
                  uptime {uptime}
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Disk */}
        <Card className="relative overflow-hidden">
          <div
            className="absolute top-0 left-0 right-0 h-[2px]"
            style={{
              backgroundColor: readyData?.disks?.some((d) => d.status === 'unhealthy')
                ? 'var(--color-danger)'
                : readyData?.disks?.some((d) => d.status === 'degraded')
                ? 'var(--color-warning)'
                : 'var(--color-success)',
            }}
          />
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-3">
              <HardDrive className="h-3.5 w-3.5 text-text-tertiary" />
              <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Disk</span>
            </div>
            {readyLoading ? (
              <Skeleton className="h-12 w-full" />
            ) : !readyData?.disks || readyData.disks.length === 0 ? (
              <div className="text-[11px] text-text-tertiary font-mono">No mounts configured</div>
            ) : (
              <div className="space-y-1.5">
                {readyData.disks.map((d) => (
                  <div key={d.mount}>
                    <div className="flex items-center justify-between mb-0.5">
                      <span className="font-mono text-[10px] text-text-secondary truncate max-w-[80px]">{d.mount}</span>
                      <span
                        className="font-mono text-[10px] font-medium"
                        style={{ color: statusColor(d.status) }}
                      >
                        {d.used_pct.toFixed(0)}%
                      </span>
                    </div>
                    <div className="h-1 rounded-full overflow-hidden" style={{ backgroundColor: 'var(--color-bg-hover)' }}>
                      <div
                        className="h-full rounded-full transition-all duration-500"
                        style={{
                          width: `${Math.min(d.used_pct, 100)}%`,
                          backgroundColor: statusColor(d.status),
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function BackupStatusCard() {
  const navigate = useNavigate()
  const { data: backup, isLoading } = useBackupStatus()

  const rows = [
    { label: 'daily', run: backup?.last_daily },
    { label: 'weekly', run: backup?.last_weekly },
    { label: 'monthly', run: backup?.last_monthly },
  ]

  function relativeTime(ts?: string): string {
    if (!ts) return '–'
    const diff = Date.now() - new Date(ts).getTime()
    const mins = Math.floor(diff / 60000)
    const hours = Math.floor(mins / 60)
    const days = Math.floor(hours / 24)
    if (days > 0) return `${days}d ago`
    if (hours > 0) return `${hours}h ago`
    if (mins > 0) return `${mins}m ago`
    return 'just now'
  }

  return (
    <div>
      <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
        Backup Status
      </h2>
      <Card>
        <CardHeader className="pb-0 pt-4 px-4 flex flex-row items-center justify-between">
          <CardTitle className="text-sm font-medium text-text-primary flex items-center gap-2">
            <Archive className="h-4 w-4 text-text-tertiary" />
            Scheduled Backups
          </CardTitle>
          <Button
            variant="ghost"
            size="sm"
            className="gap-1.5 text-xs text-text-secondary hover:text-text-primary h-7"
            onClick={() => navigate('/settings#reliability')}
          >
            View History
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </CardHeader>
        <CardContent className="px-4 pb-4 pt-3">
          {isLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-6 w-full" />
              ))}
            </div>
          ) : (
            <div className="space-y-0">
              {rows.map(({ label, run }, idx) => (
                <div
                  key={label}
                  className={cn(
                    'flex items-center gap-4 py-2.5',
                    idx < rows.length - 1 && 'border-b border-border-subtle',
                  )}
                >
                  <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary w-14 flex-shrink-0">
                    {label}
                  </span>
                  {run ? (
                    <>
                      <div className="flex items-center gap-1.5 flex-shrink-0">
                        <span
                          className="h-2 w-2 rounded-full flex-shrink-0"
                          style={{
                            backgroundColor: statusColor(run.status === 'succeeded' ? 'ok' : run.status),
                            boxShadow: statusGlow(run.status === 'succeeded' ? 'ok' : run.status),
                          }}
                        />
                        <Badge
                          variant="outline"
                          className={cn('text-[10px] h-4 px-1.5', run.status === 'succeeded' ? 'border-success/30 text-success' : run.status === 'failed' ? 'border-danger/30 text-danger' : 'border-border')}
                        >
                          {run.status}
                        </Badge>
                      </div>
                      <span className="text-[11px] text-text-secondary flex-shrink-0">
                        {relativeTime(run.finished_at ?? run.started_at)}
                      </span>
                      <span className="text-[11px] text-text-tertiary font-mono flex-shrink-0">
                        {run.size_mb > 0 ? `${run.size_mb.toFixed(1)} MB` : '–'}
                      </span>
                    </>
                  ) : (
                    <span className="text-[11px] text-text-tertiary">No backup yet</span>
                  )}
                </div>
              ))}
            </div>
          )}
          {!isLoading && !backup?.last_daily && !backup?.last_weekly && !backup?.last_monthly && (
            <div className="mt-2 pt-2 border-t border-border-subtle">
              <p className="text-[11px] text-text-tertiary text-center">
                No backups yet — backups start after{' '}
                <span className="font-mono text-text-secondary">BACKUP_ENABLED=true</span>{' '}
                and first @daily cron fires.
              </p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
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
      <div className="space-y-4">
        <Skeleton className="h-6 w-40" />
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4"><Skeleton className="h-20 w-full" /></CardContent>
            </Card>
          ))}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardHeader className="pb-2"><Skeleton className="h-4 w-20" /></CardHeader>
              <CardContent className="pt-0"><Skeleton className="h-24 w-full" /></CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-3">
          <h1 className="text-[16px] font-semibold text-text-primary">System Health</h1>
          <span className="flex items-center gap-1">
            <span className="h-1.5 w-1.5 rounded-full bg-accent pulse-dot" style={{ boxShadow: '0 0 6px rgba(0,212,255,0.4)' }} />
            <span className="text-[10px] text-text-tertiary">LIVE</span>
          </span>
        </div>
      </div>

      {/* Health Probe Cards */}
      <ProbeCards />

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
          {services.length === 0 && (
            <Card className="col-span-full">
              <CardContent className="p-6 flex items-center justify-center gap-2 text-text-tertiary">
                <ServerCrash className="h-4 w-4" />
                <span className="text-sm">No service data available</span>
              </CardContent>
            </Card>
          )}
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

      {/* Backup Status */}
      <BackupStatusCard />
    </div>
  )
}
