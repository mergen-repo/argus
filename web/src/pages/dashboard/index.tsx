import React, { useMemo, useState, useEffect, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AlertTriangle, Activity, Cpu, DollarSign, RefreshCw, AlertCircle, Info,
  Zap, Globe, Wifi, WifiOff,
  ShieldCheck, ShieldAlert, ShieldX, Radio, CheckCircle2, XCircle,
  ChevronRight, Gauge, TrendingUp,
} from 'lucide-react'
import {
  PieChart, Pie, Cell, ResponsiveContainer, Tooltip,
  BarChart, Bar, XAxis, YAxis,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Sparkline } from '@/components/ui/sparkline'
import { KPICard } from '@/components/shared/kpi-card'
import type { KPICardProps } from '@/components/shared/kpi-card'
import { useDashboard, useRealtimeAuthPerSec, useRealtimeAlerts, useRealtimeMetrics, useRealtimeActiveSessions, useRealtimeOperatorHealth } from '@/hooks/use-dashboard'
import type { DashboardData, DashboardAlert, OperatorHealth, TopAPN, SIMByState, TrafficHeatmapCell } from '@/types/dashboard'
import { OperatorChip } from '@/components/shared/operator-chip'
import { EntityLink } from '@/components/shared'
import { formatNumber, formatCurrency, formatBytes, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { SeverityBadge } from '@/components/shared/severity-badge'

const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
const HOURS = Array.from({ length: 24 }, (_, i) => i)

const STATE_COLORS: Record<string, string> = {
  active: 'var(--color-success)',
  suspended: 'var(--color-warning)',
  ordered: 'var(--color-accent)',
  terminated: 'var(--color-danger)',
  stolen_lost: 'var(--color-purple)',
  lost: 'var(--color-purple)',
}

import { useEventStore, type LiveEvent } from '@/stores/events'

// ─── System Status Strip ────────────────────────────────────────────────────

const SystemStatusStrip = React.memo(function SystemStatusStrip({
  status,
  alertCounts,
}: {
  status: 'operational' | 'degraded' | 'critical'
  alertCounts: { critical: number; warning: number; info: number }
}) {
  const navigate = useNavigate()

  const config = useMemo(() => {
    switch (status) {
      case 'critical':
        return {
          color: 'var(--color-danger)',
          bg: 'linear-gradient(90deg, rgba(255,68,102,0.08) 0%, rgba(255,68,102,0.03) 100%)',
          label: 'CRITICAL ISSUES',
          icon: <ShieldX className="h-3.5 w-3.5" />,
        }
      case 'degraded':
        return {
          color: 'var(--color-warning)',
          bg: 'linear-gradient(90deg, rgba(255,184,0,0.08) 0%, rgba(255,184,0,0.03) 100%)',
          label: 'DEGRADED',
          icon: <ShieldAlert className="h-3.5 w-3.5" />,
        }
      default:
        return {
          color: 'var(--color-success)',
          bg: 'linear-gradient(90deg, rgba(0,255,136,0.06) 0%, rgba(0,255,136,0.02) 100%)',
          label: 'ALL SYSTEMS OPERATIONAL',
          icon: <ShieldCheck className="h-3.5 w-3.5" />,
        }
    }
  }, [status])

  return (
    <div
      className="stagger-item flex items-center justify-between px-4 py-2 rounded-[var(--radius-md)] border border-border cursor-pointer transition-all hover:border-border-subtle"
      style={{ background: config.bg, animationDelay: '0ms' }}
      onClick={() => navigate('/analytics/anomalies')}
    >
      <div className="flex items-center gap-2.5">
        <span
          className="h-2 w-2 rounded-full pulse-dot"
          style={{ backgroundColor: config.color, boxShadow: `0 0 8px ${config.color}60` }}
        />
        <span style={{ color: config.color }} className="flex items-center gap-1.5">
          {config.icon}
        </span>
        <span className="text-[11px] font-semibold tracking-[1.5px] uppercase" style={{ color: config.color }}>
          {config.label}
        </span>
      </div>
      <div className="flex items-center gap-2">
        {alertCounts.critical > 0 && (
          <Badge variant="danger" className="text-[10px] font-mono">
            {alertCounts.critical} Critical
          </Badge>
        )}
        {alertCounts.warning > 0 && (
          <Badge variant="warning" className="text-[10px] font-mono">
            {alertCounts.warning} Warning
          </Badge>
        )}
        {alertCounts.info > 0 && (
          <Badge variant="secondary" className="text-[10px] font-mono">
            {alertCounts.info} Info
          </Badge>
        )}
        <ChevronRight className="h-3.5 w-3.5 text-text-tertiary" />
      </div>
    </div>
  )
})

// ─── KPI Metric Card ────────────────────────────────────────────────────────
// Extracted to web/src/components/shared/kpi-card.tsx — imported above.

// ─── Operator Health Matrix ─────────────────────────────────────────────────

// Stable reference so the selector below doesn't return a fresh empty
// array on every render (Zustand uses strict equality → would infinite-loop).
const EMPTY_BUCKET_ARRAY: Array<{ minute: number; count: number }> = []

// OperatorActivitySparkline — per-operator 15-minute bar-style histogram
// fed by useEventStore.operatorHistogram. Updates live as session.started/
// updated/ended events stream in. Mirrors the topbar ActivitySparkline's
// visual language (thin bars, last-minute highlighted) but scoped to a
// single operator_id.
function OperatorActivitySparkline({ operatorId }: { operatorId: string }) {
  const histogram = useEventStore((s) => s.operatorHistogram[operatorId]) ?? EMPTY_BUCKET_ARRAY

  const bars = useMemo(() => {
    const now = Math.floor(Date.now() / 60_000)
    const result: number[] = []
    for (let i = 14; i >= 0; i--) {
      const min = now - i
      const bucket = histogram.find((b) => b.minute === min)
      result.push(bucket?.count ?? 0)
    }
    return result
  }, [histogram])

  const max = Math.max(...bars, 1)
  const recent = bars.slice(-3).reduce((a, b) => a + b, 0)
  const total15m = bars.reduce((a, b) => a + b, 0)
  const hasActivity = recent > 0

  return (
    <div className="flex items-center gap-2" title={`${total15m} events in last 15min · ${recent} in last 3min`}>
      <div className="flex items-end gap-[1.5px] h-4">
        {bars.map((v, i) => (
          <div
            key={i}
            className={cn(
              'w-[3px] rounded-t-[1px] transition-all duration-300',
              i === bars.length - 1 && hasActivity ? 'bg-accent' : v > 0 ? 'bg-accent/50' : 'bg-text-tertiary/15',
            )}
            style={{ height: `${Math.max((v / max) * 100, 8)}%` }}
          />
        ))}
      </div>
      <span className={cn(
        'font-mono text-[11px] tabular-nums w-[32px] text-right',
        hasActivity ? 'text-accent' : 'text-text-tertiary/40',
      )}>
        {recent}
      </span>
    </div>
  )
}

const OperatorHealthMatrix = React.memo(function OperatorHealthMatrix({
  data,
}: {
  data: OperatorHealth[]
}) {
  const navigate = useNavigate()

  const statusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'var(--color-success)'
      case 'degraded': return 'var(--color-warning)'
      case 'down': return 'var(--color-danger)'
      default: return 'var(--color-text-tertiary)'
    }
  }

  const latencyColor = (ms: number) => {
    if (ms < 30) return 'text-success'
    if (ms < 100) return 'text-warning'
    return 'text-danger'
  }

  const uptimeColor = (pct: number) => {
    if (pct >= 99.9) return 'text-success'
    if (pct >= 99) return 'text-warning'
    return 'text-danger'
  }

  const authRateColor = (rate: number): string => {
    if (rate >= 99) return 'text-success'
    if (rate >= 95) return 'text-warning'
    return 'text-danger'
  }

  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '250ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <Globe className="h-4 w-4 text-accent" />
          Operator Health Matrix
        </CardTitle>
        <span className="text-[10px] text-text-tertiary font-mono">Last 24h</span>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No operators configured
          </div>
        ) : (
          <Table className="text-left">
              <TableHeader>
                <TableRow className="border-b border-border">
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 pr-3">Operator</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 px-3 text-center">Status</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 px-3 text-right">Uptime</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 px-3 text-right">Latency</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 px-3 text-right">Auth</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 pl-3 text-right">Activity</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((op) => (
                  <TableRow
                    key={op.id}
                    className="border-b border-border/50 last:border-0 cursor-pointer hover:bg-bg-hover transition-colors group"
                    onClick={() => navigate(`/operators/${op.id}`)}
                  >
                    <TableCell className="py-2.5 pr-3">
                      <div className="flex flex-col gap-0.5">
                        <OperatorChip
                          name={op.name}
                          code={op.code}
                          rawId={op.id}
                          className="group-hover:ring-1 group-hover:ring-accent/40 transition-all"
                        />
                        {(op.active_sessions != null || op.sla_target != null) && (
                          <div className="flex items-center gap-2 pl-0.5">
                            {op.active_sessions != null && (
                              <span className="text-[10px] font-mono text-text-tertiary">
                                {op.active_sessions.toLocaleString()} active
                              </span>
                            )}
                            {op.sla_target != null && (
                              <span className="flex items-center gap-1.5">
                                <span className="text-[10px] font-mono text-text-tertiary">
                                  SLA {op.sla_target.toFixed(2)}%
                                </span>
                                {op.latency_ms != null && op.latency_ms > (op.sla_latency_ms ?? 500) && (
                                  <Badge variant="danger" className="text-[9px]">SLA breach</Badge>
                                )}
                              </span>
                            )}
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="py-2.5 px-3 text-center">
                      <span className="inline-flex items-center gap-1.5">
                        <span
                          className="h-2 w-2 rounded-full pulse-dot"
                          style={{
                            backgroundColor: statusColor(op.status),
                            boxShadow: `0 0 6px ${statusColor(op.status)}60`,
                          }}
                        />
                        <span className="text-[11px] text-text-secondary capitalize">{op.status}</span>
                      </span>
                    </TableCell>
                    <TableCell className="py-2.5 px-3 text-right">
                      <span className={cn('font-mono text-[12px]', uptimeColor(op.health_pct))}>
                        {op.health_pct.toFixed(2)}%
                      </span>
                    </TableCell>
                    <TableCell className="py-2.5 px-3 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <span className={cn('font-mono text-[12px]', latencyColor(op.latency_ms || 0))}>
                          {op.latency_ms != null ? `${op.latency_ms.toFixed(0)}ms` : '—'}
                        </span>
                        {op.latency_sparkline && op.latency_sparkline.length > 1 && (
                          <Sparkline
                            data={op.latency_sparkline}
                            width={72}
                            height={24}
                            className="ml-2 inline-block align-middle"
                            color="var(--color-accent)"
                          />
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right px-3">
                      {op.auth_rate != null ? (
                        <span className={cn('font-mono text-[12px]', authRateColor(op.auth_rate))}>
                          {op.auth_rate.toFixed(1)}%
                        </span>
                      ) : (
                        <span className="text-text-tertiary">—</span>
                      )}
                    </TableCell>
                    <TableCell className="py-2.5 pl-3">
                      <div className="flex justify-end">
                        <OperatorActivitySparkline operatorId={op.id} />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
})

// ─── Traffic Heatmap ────────────────────────────────────────────────────────

const TrafficHeatmap = React.memo(function TrafficHeatmap({
  data,
}: {
  data: TrafficHeatmapCell[]
}) {
  const [hoveredCell, setHoveredCell] = useState<{ day: number; hour: number; value: number; rawBytes: number } | null>(null)

  const maxValue = useMemo(() => {
    if (data.length === 0) return 1
    return Math.max(...data.map((d) => d.value))
  }, [data])

  const grid = useMemo(() => {
    const map = new Map<string, { value: number; rawBytes: number }>()
    data.forEach((d) => map.set(`${d.day}-${d.hour}`, { value: d.value, rawBytes: d.raw_bytes ?? 0 }))
    return map
  }, [data])

  const cellColor = useCallback((value: number) => {
    const intensity = value / maxValue
    if (intensity < 0.15) return 'rgba(0,212,255,0.04)'
    if (intensity < 0.3) return 'rgba(0,212,255,0.12)'
    if (intensity < 0.5) return 'rgba(0,212,255,0.22)'
    if (intensity < 0.7) return 'rgba(0,212,255,0.38)'
    if (intensity < 0.85) return 'rgba(0,212,255,0.55)'
    return 'rgba(0,212,255,0.75)'
  }, [maxValue])

  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '300ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <Activity className="h-4 w-4 text-accent" />
          Traffic Pattern — Last 7 Days
        </CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[140px] text-text-tertiary text-sm">
            No traffic data available
          </div>
        ) : (
          <div className="relative">
            <div className="grid gap-[2px]" style={{ gridTemplateColumns: `32px repeat(24, 1fr)` }}>
              <div />
              {HOURS.map((h) => (
                <div key={h} className="text-center text-[9px] font-mono text-text-tertiary pb-1">
                  {h % 3 === 0 ? `${h.toString().padStart(2, '0')}` : ''}
                </div>
              ))}

              {DAYS.map((day, dayIdx) => (
                <React.Fragment key={dayIdx}>
                  <div className="text-[10px] font-mono text-text-tertiary flex items-center pr-1">{day}</div>
                  {HOURS.map((hour) => {
                    const cell = grid.get(`${dayIdx}-${hour}`) ?? { value: 0, rawBytes: 0 }
                    const isHovered = hoveredCell?.day === dayIdx && hoveredCell?.hour === hour
                    return (
                      <div
                        key={hour}
                        className="aspect-square rounded-[2px] transition-all cursor-crosshair relative"
                        style={{
                          backgroundColor: cellColor(cell.value),
                          outline: isHovered ? '1px solid var(--color-accent)' : 'none',
                          transform: isHovered ? 'scale(1.3)' : 'scale(1)',
                          zIndex: isHovered ? 10 : 0,
                        }}
                        onMouseEnter={() => setHoveredCell({ day: dayIdx, hour, value: cell.value, rawBytes: cell.rawBytes })}
                        onMouseLeave={() => setHoveredCell(null)}
                      />
                    )
                  })}
                </React.Fragment>
              ))}
            </div>

            {hoveredCell && (
              <div className="absolute top-0 right-0 bg-bg-elevated border border-border rounded-[var(--radius-sm)] px-2.5 py-1.5 text-[11px] font-mono pointer-events-none z-20 shadow-lg">
                <span className="text-accent font-semibold">{formatBytes(hoveredCell.rawBytes)}</span>
                <span className="mx-1.5 text-text-tertiary">@</span>
                <span className="text-text-secondary">{DAYS[hoveredCell.day]} {hoveredCell.hour.toString().padStart(2, '0')}:00</span>
              </div>
            )}

            <div className="flex items-center justify-end gap-1.5 mt-2">
              <span className="text-[9px] text-text-tertiary">Low</span>
              {[0.04, 0.12, 0.22, 0.38, 0.55, 0.75].map((opacity, i) => (
                <div
                  key={i}
                  className="w-3 h-3 rounded-[2px]"
                  style={{ backgroundColor: `rgba(0,212,255,${opacity})` }}
                />
              ))}
              <span className="text-[9px] text-text-tertiary">High</span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
})

// ─── SIM Distribution Donut ─────────────────────────────────────────────────

const SIMDistributionDonut = React.memo(function SIMDistributionDonut({
  data,
}: {
  data: SIMByState[]
}) {
  const totalSims = useMemo(() => data.reduce((sum, d) => sum + d.count, 0), [data])

  const chartData = useMemo(
    () =>
      data.map((d) => ({
        name: d.state.split('_').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join('/'),
        value: d.count,
        fill: STATE_COLORS[d.state] ?? 'var(--color-text-tertiary)',
      })),
    [data],
  )

  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '350ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <Cpu className="h-4 w-4 text-accent" />
          SIM Distribution
        </CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[200px] text-text-tertiary text-sm">
            No SIM data available
          </div>
        ) : (
          <div className="flex items-center gap-5">
            <div className="w-[160px] h-[160px] relative flex-shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={chartData}
                    dataKey="value"
                    cx="50%"
                    cy="50%"
                    innerRadius={48}
                    outerRadius={72}
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
                    formatter={(value, _name, props) => {
                      // Recharts passes the raw numeric value; the entry's
                      // "name" (Active / Ordered / Suspended …) is on
                      // props.payload. Substitute it for the generic
                      // "Count" label so the tooltip reads e.g. "Active: 8".
                      const label = (props?.payload as { name?: string } | undefined)?.name ?? 'Count'
                      return [formatNumber(Number(value)), label]
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
              <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                <span className="font-mono text-[18px] font-bold text-text-primary leading-none">
                  {formatNumber(totalSims)}
                </span>
                <span className="text-[9px] uppercase tracking-[1px] text-text-tertiary mt-1">Total</span>
              </div>
            </div>
            <div className="flex flex-col gap-1.5 flex-1 min-w-0">
              {chartData.map((entry, idx) => {
                const pct = totalSims > 0 ? ((entry.value / totalSims) * 100).toFixed(1) : '0'
                return (
                  <div key={idx} className="flex items-center justify-between text-[12px] group">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="h-2 w-2 rounded-full flex-shrink-0" style={{ backgroundColor: entry.fill }} />
                      <span className="text-text-secondary truncate">{entry.name}</span>
                    </div>
                    <div className="flex items-center gap-2 flex-shrink-0 pl-2">
                      <span className="font-mono text-[11px] text-text-primary">{formatNumber(entry.value)}</span>
                      <span className="font-mono text-[10px] text-text-tertiary w-[36px] text-right">{pct}%</span>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
})

// ─── Top APNs ───────────────────────────────────────────────────────────────

const TopAPNsByTraffic = React.memo(function TopAPNsByTraffic({
  data,
}: {
  data: TopAPN[]
}) {
  const navigate = useNavigate()
  const maxSessions = useMemo(() => Math.max(...data.map((d) => d.session_count), 1), [data])

  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '400ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <Radio className="h-4 w-4 text-accent" />
          Top 5 APNs by Traffic
        </CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No active sessions
          </div>
        ) : (
          <div className="flex flex-col gap-2.5">
            {data.slice(0, 5).map((apn) => {
              const pct = (apn.session_count / maxSessions) * 100
              return (
                <div
                  key={apn.id || apn.name}
                  className="group cursor-pointer"
                  onClick={() => apn.id && navigate(`/apns/${apn.id}`)}
                >
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-[12px] font-mono text-text-primary group-hover:text-accent transition-colors truncate" onClick={(e) => e.stopPropagation()}>
                      {apn.id ? <EntityLink entityType="apn" entityId={apn.id} label={apn.name === 'none' ? 'No APN' : apn.name} /> : (apn.name === 'none' ? 'No APN' : apn.name)}
                    </span>
                    <div className="flex items-center gap-3 flex-shrink-0 pl-2">
                      <span className="text-[11px] font-mono text-text-secondary">
                        {formatNumber(apn.session_count)} sess
                      </span>
                      <span className="text-[10px] font-mono text-text-tertiary">
                        {formatBytes(apn.bytes_total || 0)}
                      </span>
                    </div>
                  </div>
                  <div className="h-1.5 bg-bg-hover rounded-full overflow-hidden">
                    <div
                      className="h-full rounded-full transition-all duration-500 group-hover:brightness-125"
                      style={{
                        width: `${pct}%`,
                        background: `linear-gradient(90deg, var(--color-accent), var(--color-cyan))`,
                      }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
})

// ─── Live Event Stream ──────────────────────────────────────────────────────

// EventSourceChips renders IMSI / IP / Operator / APN / Policy / Job chips
// derived from the LiveEvent payload. Highlights SIM-level context
// (IMSI/IP) in accent colour; falls back to entity_type:entity_id when no
// richer signal is present.
function pickStr(v: unknown): string | undefined {
  return typeof v === 'string' && v ? v : undefined
}

function EventSourceChips({ event }: { event: LiveEvent }) {
  const meta = event.meta || {}
  const chips: Array<{ label: string; value: string; highlight?: boolean }> = []
  if (event.imsi) chips.push({ label: 'IMSI', value: event.imsi, highlight: true })
  if (event.framed_ip) chips.push({ label: 'IP', value: event.framed_ip, highlight: true })
  if (event.msisdn) chips.push({ label: 'MSISDN', value: event.msisdn })

  // Name-aware priority chain (FIX-219 / AC-7):
  // P1: envelope entity display_name, P2: meta name fields, P3: UUID slice
  const envEntityType = event.entity?.type
  const envDisplayName = event.entity?.display_name
  function resolveId(id: string, matchType: string, metaNameKey: string): string {
    if (envEntityType === matchType && envDisplayName) return envDisplayName
    const metaName = pickStr(meta[metaNameKey])
    if (metaName) return metaName
    return id.slice(0, 8)
  }

  if (event.operator_id && !event.imsi) chips.push({ label: 'Op', value: resolveId(event.operator_id, 'operator', 'operator_name') })
  if (event.apn_id && !event.imsi) chips.push({ label: 'APN', value: resolveId(event.apn_id, 'apn', 'apn_name') })
  if (event.policy_id) chips.push({ label: 'Policy', value: resolveId(event.policy_id, 'policy', 'policy_name') })
  if (event.job_id) chips.push({ label: 'Job', value: resolveId(event.job_id, 'job', 'job_name') })
  if (typeof event.progress_pct === 'number') chips.push({ label: '%', value: `${Math.round(event.progress_pct)}` })
  if (chips.length === 0 && event.entity_type && event.entity_id) {
    const fallbackValue = envDisplayName || event.entity_id.slice(0, 8)
    chips.push({ label: event.entity_type, value: fallbackValue })
  }
  if (chips.length === 0) return null
  return (
    <div className="flex items-center gap-1.5 mt-0.5 flex-wrap">
      {chips.map((c, i) => (
        <span key={i} className="inline-flex items-center gap-1 text-[10px] font-mono">
          <span className="text-text-tertiary opacity-60">{c.label}</span>
          <span className={c.highlight ? 'text-accent' : 'text-text-secondary'}>{c.value}</span>
        </span>
      ))}
    </div>
  )
}

function eventIcon(type: string) {
  switch (type) {
    case 'sim.activated':
    case 'sim.provisioned':
      return <Cpu className="h-3.5 w-3.5 text-success flex-shrink-0" />
    case 'session.disconnect':
    case 'session.timeout':
      return <WifiOff className="h-3.5 w-3.5 text-warning flex-shrink-0" />
    case 'session.start':
      return <Wifi className="h-3.5 w-3.5 text-cyan flex-shrink-0" />
    case 'alert.new':
    case 'alert.triggered':
      return <AlertCircle className="h-3.5 w-3.5 text-danger flex-shrink-0" />
    case 'policy.changed':
    case 'policy.applied':
      return <ShieldCheck className="h-3.5 w-3.5 text-purple flex-shrink-0" />
    case 'import.complete':
    case 'job.complete':
      return <CheckCircle2 className="h-3.5 w-3.5 text-accent flex-shrink-0" />
    default:
      return <Info className="h-3.5 w-3.5 text-info flex-shrink-0" />
  }
}

function LiveEventStream() {
  const navigate = useNavigate()
  // Shared event store — DashboardLayout's useGlobalEventListener already
  // subscribes to wsClient and enriches every event with source fields
  // (imsi, framed_ip, msisdn, operator_id, apn_id, policy_id, job_id,
  // progress_pct). Reusing the store avoids duplicate WS subscriptions
  // and keeps this inline stream consistent with the drawer.
  const events = useEventStore((s) => s.events).slice(0, 50)
  const containerRef = useRef<HTMLDivElement>(null)

  const handleEventClick = useCallback((event: LiveEvent) => {
    if (event.entity_type && event.entity_id) {
      const routes: Record<string, string> = {
        sim: '/sims',
        session: '/sessions',
        operator: '/operators',
        policy: '/policies',
        apn: '/apns',
      }
      const base = routes[event.entity_type] || '/analytics/anomalies'
      navigate(`${base}/${event.entity_id}`)
    } else {
      navigate('/analytics/anomalies')
    }
  }, [navigate])

  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '450ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-accent" />
          Live Event Stream
        </CardTitle>
        <span className="flex items-center gap-1.5">
          <span
            className="h-1.5 w-1.5 rounded-full bg-success pulse-dot"
            style={{ boxShadow: '0 0 6px rgba(0,255,136,0.4)' }}
          />
          <span className="text-[9px] font-semibold tracking-[1px] text-success">LIVE</span>
        </span>
      </CardHeader>
      <CardContent>
        <div ref={containerRef} className="flex flex-col gap-0.5 max-h-[280px] overflow-y-auto">
          {events.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-[140px] text-text-tertiary text-sm gap-2">
              <Radio className="h-5 w-5 animate-pulse" />
              <span>Waiting for events...</span>
            </div>
          ) : (
            events.map((event, idx) => {
              const ts = new Date(event.timestamp).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
              // metrics.realtime filtered out at ingest — see
              // dashboard-layout.tsx useGlobalEventListener.
              return (
                <div
                  key={event.id}
                  className={cn(
                    'flex items-start gap-2.5 py-1.5 px-2 rounded-[var(--radius-sm)] hover:bg-bg-hover cursor-pointer transition-colors',
                    idx === 0 && 'animate-slide-up-in',
                  )}
                  onClick={() => handleEventClick(event)}
                >
                  {eventIcon(event.type)}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-[10px] font-mono text-text-tertiary flex-shrink-0">{ts}</span>
                      <span className="text-[12px] text-text-primary truncate">{event.message}</span>
                    </div>
                    <EventSourceChips event={event} />
                  </div>
                  <SeverityBadge severity={event.severity} className="flex-shrink-0" />
                </div>
              )
            })
          )}
        </div>
      </CardContent>
    </Card>
  )
}

// ─── Alert Feed (from data) ─────────────────────────────────────────────────

const AlertFeed = React.memo(function AlertFeed({ alerts }: { alerts: DashboardAlert[] }) {
  const navigate = useNavigate()


  return (
    <Card className="card-hover stagger-item" style={{ animationDelay: '450ms' }}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="flex items-center gap-2">
          <AlertTriangle className="h-4 w-4 text-warning" />
          Recent Alerts
        </CardTitle>
        <span className="flex items-center gap-1.5">
          <span
            className="h-1.5 w-1.5 rounded-full bg-danger pulse-dot"
            style={{ boxShadow: '0 0 6px rgba(255,68,102,0.4)' }}
          />
          <span className="text-[9px] font-semibold tracking-[1px] text-danger">LIVE</span>
        </span>
      </CardHeader>
      <CardContent>
        {alerts.length === 0 ? (
          <div className="flex items-center justify-center h-[120px] text-text-tertiary text-sm">
            No recent alerts
          </div>
        ) : (
          <div className="flex flex-col gap-0.5 max-h-[280px] overflow-y-auto">
            {alerts.map((alert, idx) => (
              <div
                key={alert.id || idx}
                className="flex items-start gap-2.5 py-1.5 px-2 rounded-[var(--radius-sm)] hover:bg-bg-hover cursor-pointer transition-colors"
                onClick={() => {
                  if (alert.source === 'sim' && alert.meta?.anomaly_id && alert.sim_id) {
                    navigate(`/sims/${alert.sim_id}`)
                  } else {
                    navigate(`/alerts/${alert.id}`)
                  }
                }}
              >
                <SeverityBadge severity={alert.severity} iconOnly className="flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-[12px] text-text-primary truncate">{alert.message}</p>
                  <p className="text-[10px] text-text-tertiary mt-0.5">{timeAgo(alert.detected_at)}</p>
                </div>
                {alert.sim_id && (
                  <span className="flex-shrink-0" onClick={(e) => e.stopPropagation()}>
                    <EntityLink entityType="sim" entityId={alert.sim_id} truncate className="text-[10px]" />
                  </span>
                )}
                {alert.source && (
                  <span className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-text-secondary flex-shrink-0">
                    {alert.source}
                  </span>
                )}
                <SeverityBadge severity={alert.severity} className="flex-shrink-0" />
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
})

// ─── Skeleton Loading ───────────────────────────────────────────────────────

function DashboardSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-10 w-full rounded-[var(--radius-md)]" />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        {Array.from({ length: 8 }).map((_, i) => (
          <Card key={i} className="relative overflow-hidden">
            <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-bg-hover" />
            <CardHeader className="pb-1 pt-3 px-4">
              <Skeleton className="h-3 w-20" />
            </CardHeader>
            <CardContent className="pt-0 pb-3 px-4">
              <div className="flex items-end justify-between mb-2">
                <Skeleton className="h-8 w-24" />
                <Skeleton className="h-4 w-12" />
              </div>
              <Skeleton className="h-6 w-full" />
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[3fr_2fr] gap-4">
        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-40" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[200px] w-full" />
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-48" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[160px] w-full" />
            </CardContent>
          </Card>
        </div>
        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[180px] w-full" />
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-36" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[150px] w-full" />
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-[160px] w-full" />
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

// ─── Error State ────────────────────────────────────────────────────────────

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-[var(--radius-lg)] border border-danger/30 bg-danger-dim p-8 text-center max-w-md">
        <div className="rounded-full bg-danger/10 w-16 h-16 flex items-center justify-center mx-auto mb-4">
          <AlertCircle className="h-8 w-8 text-danger" />
        </div>
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load dashboard</h2>
        <p className="text-sm text-text-secondary mb-5">
          Unable to fetch dashboard data. The system may be experiencing connectivity issues.
        </p>
        <Button onClick={onRetry} variant="outline" className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    </div>
  )
}

// ─── Main Dashboard ─────────────────────────────────────────────────────────

export default function DashboardPage() {
  const { data, isLoading, isError, refetch } = useDashboard()
  useRealtimeAuthPerSec()
  useRealtimeAlerts()
  useRealtimeMetrics()
  useRealtimeActiveSessions()
  useRealtimeOperatorHealth()

  const navigate = useNavigate()

  if (isLoading) return <DashboardSkeleton />
  if (isError || !data) return <ErrorState onRetry={() => refetch()} />

  const m = data.metrics || {
    total_sims: data.total_sims,
    active_sessions: data.active_sessions,
    auth_per_sec: data.auth_per_sec,
    session_start_rate: 0,
    error_rate: 0,
    monthly_cost: data.monthly_cost,
    ip_pool_usage_pct: 0,
    sim_velocity_per_hour: 0,
  }
  const d = data.deltas || {
    total_sims_delta: 0,
    active_sessions_delta: 0,
    auth_per_sec_delta: 0,
    monthly_cost_delta: 0,
    error_rate_delta: 0,
    ip_pool_usage_delta: 0,
  }
  const sp = data.sparklines || {}

  const errorRateColor =
    m.error_rate > 1 ? 'var(--color-danger)' :
    m.error_rate > 0.5 ? 'var(--color-warning)' :
    'var(--color-success)'

  const ipPoolColor =
    m.ip_pool_usage_pct > 90 ? 'var(--color-danger)' :
    m.ip_pool_usage_pct > 70 ? 'var(--color-warning)' :
    'var(--color-accent)'

  return (
    <div className="space-y-4">
      {/* ── System Status Strip ──────────────────────────────────── */}
      <SystemStatusStrip
        status={data.system_status || 'operational'}
        alertCounts={data.alert_counts || { critical: 0, warning: 0, info: 0 }}
      />

      {/* ── KPI Strip — 8 Cards ──────────────────────────────────── */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <KPICard
          title="Total SIMs"
          value={m.total_sims}
          formatter={formatNumber}
          sparklineData={sp.total_sims || []}
          color="var(--color-accent)"
          delta={d.total_sims_delta}
          onClick={() => navigate('/sims')}
          delay={50}
        />
        <KPICard
          title="Active Sessions"
          value={m.active_sessions}
          formatter={formatNumber}
          sparklineData={sp.active_sessions || []}
          color="var(--color-success)"
          delta={d.active_sessions_delta}
          onClick={() => navigate('/sessions')}
          delay={80}
        />
        <KPICard
          title="Auth/s"
          value={Math.round(m.auth_per_sec)}
          formatter={formatNumber}
          sparklineData={sp.auth_per_sec || []}
          color="var(--color-purple)"
          delta={d.auth_per_sec_delta}
          live
          onClick={() => navigate('/system/health')}
          delay={110}
        />
        <KPICard
          title="Session Start/s"
          value={Math.round(m.session_start_rate)}
          formatter={formatNumber}
          sparklineData={sp.session_start_rate || []}
          color="var(--color-cyan)"
          delay={140}
        />
        <KPICard
          title="Error Rate"
          value={m.error_rate}
          formatter={(n) => `${n.toFixed(2)}%`}
          sparklineData={sp.error_rate || []}
          color={errorRateColor}
          delta={d.error_rate_delta}
          deltaFormat="absolute"
          suffix="%"
          delay={170}
        />
        <KPICard
          title="Monthly Cost"
          value={m.monthly_cost}
          formatter={formatCurrency}
          sparklineData={sp.monthly_cost || []}
          color="var(--color-warning)"
          delta={d.monthly_cost_delta}
          onClick={() => navigate('/analytics/cost')}
          delay={200}
        />
        <KPICard
          title="Pool Utilization (avg across all pools)"
          value={m.ip_pool_usage_pct}
          formatter={(n) => `${n.toFixed(1)}%`}
          sparklineData={sp.ip_pool_usage || []}
          color={ipPoolColor}
          delta={d.ip_pool_usage_delta}
          deltaFormat="absolute"
          subtitle={
            data.top_ip_pool
              ? `Top pool: ${data.top_ip_pool.name} ${data.top_ip_pool.usage_pct.toFixed(0)}%`
              : undefined
          }
          delay={230}
        />
        <KPICard
          title="SIM Velocity"
          value={Math.round(m.sim_velocity_per_hour)}
          formatter={(n) => `+${formatNumber(n)}`}
          sparklineData={sp.sim_velocity || []}
          color="var(--color-info)"
          suffix="/h"
          delay={260}
        />
      </div>

      {/* ── Main Content Grid ────────────────────────────────────── */}
      <div className="grid grid-cols-1 lg:grid-cols-[3fr_2fr] gap-4">
        {/* Left Column — 60% */}
        <div className="space-y-4">
          <OperatorHealthMatrix data={data.operator_health || []} />
          <TrafficHeatmap data={data.traffic_heatmap || []} />
        </div>

        {/* Right Column — 40% */}
        <div className="space-y-4">
          <SIMDistributionDonut data={data.sim_by_state || []} />
          <TopAPNsByTraffic data={data.top_apns || []} />
          {/* FIX-209 Gate (F-U1): AlertFeed was defined but never mounted — AC-5 requires
              the dashboard Recent Alerts panel to read from the unified alerts source. */}
          <AlertFeed alerts={data.recent_alerts || []} />
          <LiveEventStream />
        </div>
      </div>
    </div>
  )
}
