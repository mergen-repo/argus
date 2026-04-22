import { useState, useMemo, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  RefreshCw,
  AlertCircle,
  Activity,
  BarChart3,
  Shield,
  Settings,
  Zap,
  Loader2,
  CheckCircle2,
  XOctagon,
  Clock,
  Pencil,
  Trash2,
  Signal,
  Handshake,
  Plus,
  Layers,
  Bell,
  Wifi,
  Radio,
} from 'lucide-react'
import {
  AreaChart,
  Area,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import {
  useOperator,
  useOperatorHealth,
  useTestConnection,
  useRealtimeOperatorHealth,
  useUpdateOperator,
} from '@/hooks/use-operators'
import { useOperatorHealthHistory, useOperatorMetrics, useOperatorSessions, useOperatorTraffic } from '@/hooks/use-operator-detail'
import { useOperatorRoamingAgreements } from '@/hooks/use-roaming-agreements'
import { formatBytes } from '@/lib/format'
import type { RoamingAgreement, AgreementState, AgreementType } from '@/types/roaming'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'
import { RAT_DISPLAY } from '@/lib/constants'
import { api } from '@/lib/api'
import { InfoRow } from '@/components/ui/info-row'
import { RelatedAuditTab, RelatedNotificationsPanel, RelatedAlertsPanel, RelatedViolationsTab, EntityLink, FavoriteToggle, EmptyState } from '@/components/shared'
import { RowQuickPeek } from '@/components/shared/row-quick-peek'
import { useSIMList } from '@/hooks/use-sims'
import { stateVariant, stateLabel } from '@/lib/sim-utils'
import { RATBadge } from '@/components/ui/rat-badge'
import { ProtocolsPanel } from '@/components/operators/ProtocolsPanel'
import { toast } from 'sonner'
import { useUpdateOperatorSLA } from '@/hooks/use-sla'

const ADAPTER_DISPLAY: Record<string, string> = {
  mock: 'Mock',
  radius: 'RADIUS',
  diameter: 'Diameter',
  sba: '5G SBA',
  http: 'HTTP',
}

// STORY-090 Wave 3 Task 7c: primaryProtocol is the first protocol in
// canonical order (server-computed). Empty list → '' → "—" label.
function primaryProtocol(op: { enabled_protocols?: string[] }): string {
  return op.enabled_protocols?.[0] ?? ''
}

const FAILOVER_DISPLAY: Record<string, string> = {
  reject: 'Reject',
  fallback_to_next: 'Fallback to Next',
  queue_with_timeout: 'Queue with Timeout',
}

function healthColor(status: string) {
  switch (status) {
    case 'healthy': return 'var(--color-success)'
    case 'degraded': return 'var(--color-warning)'
    case 'down': return 'var(--color-danger)'
    default: return 'var(--color-text-tertiary)'
  }
}

function healthGlow(status: string) {
  switch (status) {
    case 'healthy': return '0 0 8px rgba(0,255,136,0.4)'
    case 'degraded': return '0 0 8px rgba(255,184,0,0.4)'
    case 'down': return '0 0 8px rgba(255,68,102,0.4)'
    default: return 'none'
  }
}

function healthVariant(status: string): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'down': return 'danger'
    default: return 'secondary'
  }
}

function circuitColor(state: string) {
  switch (state) {
    case 'closed': return 'text-success'
    case 'half_open': return 'text-warning'
    case 'open': return 'text-danger'
    default: return 'text-text-tertiary'
  }
}

function circuitBg(state: string) {
  switch (state) {
    case 'closed': return 'bg-success-dim border-success/30'
    case 'half_open': return 'bg-warning-dim border-warning/30'
    case 'open': return 'bg-danger-dim border-danger/30'
    default: return 'bg-bg-elevated border-border'
  }
}

function circuitIcon(state: string) {
  switch (state) {
    case 'closed': return <CheckCircle2 className="h-6 w-6 text-success" />
    case 'half_open': return <Clock className="h-6 w-6 text-warning" />
    case 'open': return <XOctagon className="h-6 w-6 text-danger" />
    default: return <Shield className="h-6 w-6 text-text-tertiary" />
  }
}

function OverviewTab({
  operator,
  health,
  onTest,
  testResult,
  isTesting,
}: {
  operator: NonNullable<ReturnType<typeof useOperator>['data']>
  health: NonNullable<ReturnType<typeof useOperatorHealth>['data']> | undefined
  onTest: () => void
  testResult: { success: boolean; latency_ms: number; error?: string } | null
  isTesting: boolean
}) {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-4 gap-4">
        <Card>
          <CardContent className="pt-4">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Health Status</div>
            <div className="flex items-center gap-2">
              <span
                className="h-2.5 w-2.5 rounded-full pulse-dot"
                style={{
                  backgroundColor: healthColor(operator.health_status),
                  boxShadow: healthGlow(operator.health_status),
                }}
              />
              <span className={cn('font-mono text-lg font-bold', `text-${healthVariant(operator.health_status) === 'secondary' ? 'text-primary' : healthVariant(operator.health_status)}`)}>
                {operator.health_status.toUpperCase()}
              </span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Uptime (24h)</div>
            <div className="font-mono text-lg font-bold text-text-primary">
              {health ? `${health.uptime_24h.toFixed(1)}%` : '-'}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Latency</div>
            <div className="font-mono text-lg font-bold text-accent">
              {health?.latency_ms != null ? `${health.latency_ms}ms` : '-'}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Failures (24h)</div>
            <div className={cn(
              'font-mono text-lg font-bold',
              health && health.failure_count > 0 ? 'text-danger' : 'text-success',
            )}>
              {health?.failure_count ?? 0}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Configuration</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <InfoRow label="Name" value={operator.name} />
            <InfoRow label="Code" value={operator.code} mono />
            <InfoRow label="MCC / MNC" value={`${operator.mcc} / ${operator.mnc}`} mono />
            <InfoRow label="Primary Protocol" value={ADAPTER_DISPLAY[primaryProtocol(operator)] ?? primaryProtocol(operator) ?? '—'} />
            <InfoRow label="State" value={operator.state.toUpperCase()} />
            <InfoRow label="Health Check Interval" value={`${operator.health_check_interval_sec}s`} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Test Connection</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-col items-center py-4 gap-4">
              <Button
                onClick={onTest}
                disabled={isTesting}
                size="sm"
                className="gap-2"
              >
                {isTesting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Zap className="h-4 w-4" />
                )}
                {isTesting ? 'Testing...' : 'Test Connection'}
              </Button>

              {testResult && (
                <div className={cn(
                  'w-full rounded-[var(--radius-sm)] border p-4',
                  testResult.success
                    ? 'bg-success-dim border-success/30'
                    : 'bg-danger-dim border-danger/30',
                )}>
                  <div className="flex items-center gap-2 mb-2">
                    {testResult.success ? (
                      <CheckCircle2 className="h-4 w-4 text-success" />
                    ) : (
                      <XOctagon className="h-4 w-4 text-danger" />
                    )}
                    <span className={cn('text-sm font-medium', testResult.success ? 'text-success' : 'text-danger')}>
                      {testResult.success ? 'Connection Successful' : 'Connection Failed'}
                    </span>
                  </div>
                  <div className="font-mono text-xs text-text-secondary">
                    Latency: {testResult.latency_ms}ms
                  </div>
                  {testResult.error && (
                    <div className="text-xs text-danger mt-1">{testResult.error}</div>
                  )}
                </div>
              )}

              {!testResult && !isTesting && (
                <p className="text-xs text-text-tertiary text-center">
                  Send a test health check to verify the operator connection.
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

const HOURS_OPTIONS = [
  { value: '6', label: 'Last 6h' },
  { value: '24', label: 'Last 24h' },
  { value: '72', label: 'Last 3d' },
  { value: '168', label: 'Last 7d' },
]

function HealthTimelineTab({ operatorId }: { operatorId: string }) {
  const [hours, setHours] = useState(24)
  const { data: history, isLoading, isError, refetch } = useOperatorHealthHistory(operatorId, hours)

  if (isLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <div className="space-y-3">
            {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-14 w-full" />)}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (isError) {
    return (
      <Card>
        <CardContent className="pt-6">
          <div className="rounded-lg border border-danger/30 bg-danger-dim p-6 text-center">
            <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
            <p className="text-sm text-danger mb-3">Failed to load health history.</p>
            <Button size="sm" variant="outline" onClick={() => refetch()}>Retry</Button>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>Health Check History</CardTitle>
          <Select
            options={HOURS_OPTIONS}
            value={String(hours)}
            onChange={(e) => setHours(Number(e.target.value))}
            className="w-32 h-7 text-xs"
          />
        </div>
      </CardHeader>
      <CardContent>
        {(!history || history.length === 0) ? (
          <div className="flex flex-col items-center justify-center py-12 gap-3">
            <div className="h-12 w-12 rounded-xl bg-bg-hover border border-border flex items-center justify-center">
              <Activity className="h-6 w-6 text-text-tertiary" />
            </div>
            <p className="text-sm text-text-secondary">No health checks recorded for this window.</p>
          </div>
        ) : (
          <div className="relative pl-6">
            <div className="absolute left-[11px] top-0 bottom-0 w-px bg-border" />
            {history.map((entry, i) => (
              <div key={i} className="relative pb-4 last:pb-0">
                <div
                  className="absolute left-[-13px] top-1 h-3 w-3 rounded-full border-2 border-bg-surface"
                  style={{ backgroundColor: healthColor(entry.status) }}
                />
                <div className="ml-4">
                  <div className="flex items-center gap-2 mb-0.5">
                    <Badge variant={healthVariant(entry.status)} className="text-[10px]">
                      {entry.status.toUpperCase()}
                    </Badge>
                    {entry.latency_ms != null && (
                      <span className="font-mono text-[10px] text-text-tertiary">{entry.latency_ms}ms</span>
                    )}
                    <span className={cn('text-[10px]', circuitColor(entry.circuit_state))}>
                      CB: {entry.circuit_state.replace('_', '-')}
                    </span>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-text-secondary">
                    <span>{new Date(entry.checked_at).toLocaleString()}</span>
                  </div>
                  {entry.error_message && (
                    <p className="text-xs text-danger mt-1">{entry.error_message}</p>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function CircuitBreakerTab({
  operator,
  health,
}: {
  operator: NonNullable<ReturnType<typeof useOperator>['data']>
  health: NonNullable<ReturnType<typeof useOperatorHealth>['data']> | undefined
}) {
  const circuitState = health?.circuit_state ?? 'closed'

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Circuit Breaker State</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-6">
            <div className={cn(
              'flex flex-col items-center gap-3 p-8 rounded-xl border-2',
              circuitBg(circuitState),
            )}>
              {circuitIcon(circuitState)}
              <span className={cn('text-xl font-bold font-mono uppercase', circuitColor(circuitState))}>
                {circuitState.replace('_', ' ')}
              </span>
              <p className="text-xs text-text-secondary text-center max-w-xs">
                {circuitState === 'closed' && 'All requests are flowing normally through this operator.'}
                {circuitState === 'half_open' && 'Testing with limited requests to check if the operator has recovered.'}
                {circuitState === 'open' && 'Requests are blocked. The circuit will try again after the recovery period.'}
              </p>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4 mt-4">
            <div className="text-center p-3 rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Threshold</div>
              <div className="font-mono text-sm font-semibold text-text-primary">
                {operator.circuit_breaker_threshold} failures
              </div>
            </div>
            <div className="text-center p-3 rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Recovery</div>
              <div className="font-mono text-sm font-semibold text-text-primary">
                {operator.circuit_breaker_recovery_sec}s
              </div>
            </div>
            <div className="text-center p-3 rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Failures (24h)</div>
              <div className={cn(
                'font-mono text-sm font-semibold',
                health && health.failure_count > 0 ? 'text-danger' : 'text-success',
              )}>
                {health?.failure_count ?? 0}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Failover Policy</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <InfoRow label="Policy" value={FAILOVER_DISPLAY[operator.failover_policy] ?? operator.failover_policy} />
            <InfoRow label="Timeout" value={`${operator.failover_timeout_ms}ms`} />
            <div className="mt-3 p-3 rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              <p className="text-xs text-text-secondary">
                {operator.failover_policy === 'reject' && 'When this operator is down, requests will be immediately rejected.'}
                {operator.failover_policy === 'fallback_to_next' && 'When this operator is down, requests will be routed to the next available operator.'}
                {operator.failover_policy === 'queue_with_timeout' && `When this operator is down, requests will be queued for up to ${operator.failover_timeout_ms}ms before timing out.`}
              </p>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>RAT Types & SoR</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <span className="text-xs text-text-secondary block mb-2">Supported RAT Types</span>
              <div className="flex flex-wrap gap-1.5">
                {operator.supported_rat_types.map((rat) => (
                  <Badge key={rat} variant="outline" className="text-[10px]">
                    {RAT_DISPLAY[rat] ?? rat}
                  </Badge>
                ))}
                {operator.supported_rat_types.length === 0 && (
                  <span className="text-xs text-text-tertiary">None configured</span>
                )}
              </div>
            </div>
            {operator.sla_uptime_target != null && (
              <InfoRow label="SLA Uptime Target" value={`${operator.sla_uptime_target}%`} />
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

const WINDOW_OPTIONS = [
  { value: '15m', label: 'Last 15m' },
  { value: '1h', label: 'Last 1h' },
  { value: '6h', label: 'Last 6h' },
  { value: '24h', label: 'Last 24h' },
]

const tooltipStyle = {
  backgroundColor: 'var(--color-bg-elevated)',
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--color-text-primary)',
  fontSize: '12px',
}

function TrafficTab({ operatorId }: { operatorId: string }) {
  const [period, setPeriod] = useState('24h')
  const { data: trafficSeries = [], isLoading: trafficLoading, isError: trafficError } = useOperatorTraffic(operatorId, period)
  const { data: metricsData, isError: metricsError } = useOperatorMetrics(operatorId, period === '7d' || period === '30d' ? '24h' : period)

  const series = useMemo(() => trafficSeries.map((b) => ({
    label: new Date(b.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' }),
    ts: b.ts,
    bytes_in: b.bytes_in,
    bytes_out: b.bytes_out,
    total: b.bytes_in + b.bytes_out,
    auth_count: b.auth_count,
  })), [trafficSeries])

  const totals = useMemo(() => {
    const tIn = series.reduce((a, b) => a + b.bytes_in, 0)
    const tOut = series.reduce((a, b) => a + b.bytes_out, 0)
    const tRec = series.reduce((a, b) => a + b.auth_count, 0)
    return { tIn, tOut, tTotal: tIn + tOut, tRec }
  }, [series])

  const errorPct = useMemo(() => {
    if (!metricsData?.buckets || metricsData.buckets.length === 0) return null
    const totalAuth = metricsData.buckets.reduce((a, b) => a + b.auth_rate_per_sec, 0)
    const totalErr = metricsData.buckets.reduce((a, b) => a + b.error_rate_per_sec, 0)
    if (totalAuth === 0) return 0
    return Math.round((totalErr / totalAuth) * 1000) / 10
  }, [metricsData])

  if (trafficError && metricsError) {
    return (
      <div className="rounded-lg border border-danger/30 bg-danger-dim p-6 text-center">
        <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
        <p className="text-sm text-danger">Failed to load traffic metrics.</p>
      </div>
    )
  }

  const periodOptions = [
    { value: '1h', label: 'Last 1 hour' },
    { value: '6h', label: 'Last 6 hours' },
    { value: '24h', label: 'Last 24 hours' },
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
  ]

  return (
    <div className="space-y-4">
      {/* Header with period selector */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-text-primary">Traffic Overview</h3>
          <p className="text-[11px] text-text-tertiary mt-0.5">Aggregated from CDR hourly rollups</p>
        </div>
        <Select
          options={periodOptions}
          value={period}
          onChange={(e) => setPeriod(e.target.value)}
          className="w-40 h-8 text-xs"
        />
      </div>

      {/* KPI strip */}
      <div className="grid grid-cols-4 gap-3">
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <div className="h-2 w-2 rounded-full bg-accent" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Bytes In</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tIn)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <div className="h-2 w-2 rounded-full bg-success" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Bytes Out</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tOut)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <BarChart3 className="h-3 w-3 text-text-tertiary" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Total Volume</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tTotal)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <Signal className="h-3 w-3 text-text-tertiary" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">CDR Records</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{totals.tRec.toLocaleString()}</div>
            {errorPct !== null && (
              <div className={`text-[10px] mt-1 ${errorPct > 1 ? 'text-warning' : 'text-text-tertiary'}`}>
                {errorPct}% error rate
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Main traffic chart — bytes in/out stacked area */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Bytes Over Time</CardTitle>
            <div className="flex items-center gap-3 text-[10px]">
              <div className="flex items-center gap-1.5">
                <div className="h-2 w-2 rounded-full bg-accent" /><span className="text-text-tertiary">In</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="h-2 w-2 rounded-full bg-success" /><span className="text-text-tertiary">Out</span>
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {trafficLoading ? (
            <Skeleton className="h-[280px] w-full" />
          ) : series.length === 0 ? (
            <div className="h-[280px] flex flex-col items-center justify-center gap-2">
              <BarChart3 className="h-8 w-8 text-text-tertiary opacity-40" />
              <p className="text-[12px] text-text-secondary">No traffic in this period</p>
              <p className="text-[10px] text-text-tertiary">CDR rollups refresh every 30 minutes</p>
            </div>
          ) : (
            <div className="h-[280px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={series}>
                  <defs>
                    <linearGradient id="opBytesInHero" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0.02} />
                    </linearGradient>
                    <linearGradient id="opBytesOutHero" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-success)" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="var(--color-success)" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <XAxis
                    dataKey="label"
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    interval={Math.max(0, Math.floor(series.length / 8) - 1)}
                  />
                  <YAxis
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(v) => formatBytes(v)}
                    width={70}
                  />
                  <Tooltip contentStyle={tooltipStyle} formatter={(value, name) => [formatBytes(Number(value)), name]} />
                  <Area type="monotone" dataKey="bytes_in" stackId="1" stroke="var(--color-accent)" fill="url(#opBytesInHero)" strokeWidth={2} name="In" />
                  <Area type="monotone" dataKey="bytes_out" stackId="1" stroke="var(--color-success)" fill="url(#opBytesOutHero)" strokeWidth={2} name="Out" />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Record count chart */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>CDR Records Per Bucket</CardTitle>
            <span className="text-[10px] text-text-tertiary">Auth + Accounting events</span>
          </div>
        </CardHeader>
        <CardContent>
          {trafficLoading ? (
            <Skeleton className="h-[180px] w-full" />
          ) : series.length === 0 ? (
            <div className="h-[180px] flex items-center justify-center text-[12px] text-text-tertiary">
              No records yet
            </div>
          ) : (
            <div className="h-[180px]">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={series}>
                  <XAxis
                    dataKey="label"
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    interval={Math.max(0, Math.floor(series.length / 8) - 1)}
                  />
                  <YAxis
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    width={40}
                  />
                  <Tooltip contentStyle={tooltipStyle} formatter={(value) => [value, 'Records']} />
                  <Line type="monotone" dataKey="auth_count" stroke="var(--color-accent)" strokeWidth={2} dot={false} name="Records" />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function formatDuration(sec: number): string {
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60)
  const s = sec % 60
  if (m < 60) return `${m}m ${s}s`
  const h = Math.floor(m / 60)
  const mm = m % 60
  return `${h}h ${mm}m`
}

function SessionsTab({ operatorId }: { operatorId: string }) {
  const navigate = useNavigate()
  const { data: sessions = [], isLoading, isError } = useOperatorSessions(operatorId, 50)

  if (isError) {
    return (
      <div className="rounded-lg border border-danger/30 bg-danger-dim p-6 text-center mt-4">
        <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
        <p className="text-sm text-danger">Failed to load active sessions</p>
      </div>
    )
  }

  return (
    <div className="mt-4 space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">Active Sessions</p>
        <span className="text-[11px] text-text-tertiary">{sessions.length} active</span>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>ICCID</TableHead>
                <TableHead>IMSI</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>IP Address</TableHead>
                <TableHead>State</TableHead>
                <TableHead className="text-right">Bytes In</TableHead>
                <TableHead className="text-right">Bytes Out</TableHead>
                <TableHead>Duration</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading && Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-28" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                </TableRow>
              ))}

              {!isLoading && sessions.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8}>
                    <EmptyState
                      icon={Wifi}
                      title="No active sessions"
                      description="No sessions are currently active for this operator."
                    />
                  </TableCell>
                </TableRow>
              )}

              {sessions.map((s, idx) => (
                <TableRow
                  key={s.id}
                  data-row-index={idx}
                  data-href={`/sessions/${s.id}`}
                  className="cursor-pointer"
                  onClick={() => navigate(`/sessions/${s.id}`)}
                >
                  <TableCell>
                    <span className="font-mono text-xs text-accent">{s.iccid || '—'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{s.imsi || '—'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{s.msisdn || '—'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{s.framed_ip || '—'}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={s.session_state === 'active' ? 'default' : 'default'} className="gap-1">
                      {s.session_state === 'active' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {s.session_state}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <span className="font-mono text-xs text-accent">{formatBytes(s.bytes_in || 0)}</span>
                  </TableCell>
                  <TableCell className="text-right">
                    <span className="font-mono text-xs text-success">{formatBytes(s.bytes_out || 0)}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{formatDuration(s.duration_sec || 0)}</span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </div>
  )
}

const RAT_TYPE_OPTIONS = ['nb_iot', 'lte_m', 'lte', 'nr_5g']

function EditOperatorDialog({
  open,
  onClose,
  operator,
  onSuccess,
}: {
  open: boolean
  onClose: () => void
  operator: NonNullable<ReturnType<typeof useOperator>['data']>
  onSuccess: () => void
}) {
  const [form, setForm] = useState({
    name: operator.name,
    code: operator.code,
    mcc: operator.mcc,
    mnc: operator.mnc,
    supported_rat_types: [...operator.supported_rat_types],
  })
  const [error, setError] = useState<string | null>(null)
  const updateMutation = useUpdateOperator(operator.id)

  const toggleRat = (rat: string) => {
    setForm((f) => ({
      ...f,
      supported_rat_types: f.supported_rat_types.includes(rat)
        ? f.supported_rat_types.filter((r) => r !== rat)
        : [...f.supported_rat_types, rat],
    }))
  }

  const handleSubmit = async () => {
    setError(null)
    if (!form.name.trim()) { setError('Operator name is required'); return }
    try {
      await updateMutation.mutateAsync({
        name: form.name.trim(),
        code: form.code.trim(),
        mcc: form.mcc.trim(),
        mnc: form.mnc.trim(),
        supported_rat_types: form.supported_rat_types,
      })
      onSuccess()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      setError(msg ?? 'Failed to update operator')
    }
  }

  const primary = primaryProtocol(operator)

  return (
    <SlidePanel open={open} onOpenChange={(v) => { if (!v) onClose() }} title="Edit Operator" description="Update operator configuration." width="md">
      <div className="space-y-4">
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Name *</label>
          <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="h-8 text-sm" />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Code *</label>
          <Input value={form.code} onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))} className="h-8 text-sm" />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">MCC *</label>
            <Input value={form.mcc} onChange={(e) => setForm((f) => ({ ...f, mcc: e.target.value }))} className="h-8 text-sm" />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">MNC *</label>
            <Input value={form.mnc} onChange={(e) => setForm((f) => ({ ...f, mnc: e.target.value }))} className="h-8 text-sm" />
          </div>
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Primary Protocol</label>
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="font-mono text-[10px]">
              {primary ? ADAPTER_DISPLAY[primary] ?? primary : '—'}
            </Badge>
            {operator.enabled_protocols && operator.enabled_protocols.length > 1 && (
              <span className="text-xs text-text-tertiary">
                +{operator.enabled_protocols.length - 1} more
              </span>
            )}
          </div>
          <p className="text-[11px] text-text-tertiary mt-1">
            Configure protocols from the Protocols tab.
          </p>
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Supported RAT Types</label>
          <div className="flex flex-wrap gap-2">
            {RAT_TYPE_OPTIONS.map((rat) => (
              <Button
                key={rat}
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => toggleRat(rat)}
                className={cn(
                  'px-2.5 py-1 rounded text-xs font-mono border transition-colors h-auto',
                  form.supported_rat_types.includes(rat)
                    ? 'border-accent bg-accent-dim text-accent'
                    : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
                )}
              >
                {RAT_DISPLAY[rat] ?? rat}
              </Button>
            ))}
          </div>
        </div>
        {error && <p className="text-xs text-danger">{error}</p>}
      </div>
      <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
        <Button variant="outline" size="sm" onClick={onClose} disabled={updateMutation.isPending}>Cancel</Button>
        <Button size="sm" onClick={handleSubmit} disabled={updateMutation.isPending} className="gap-1.5">
          {updateMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Save Changes
        </Button>
      </div>
    </SlidePanel>
  )
}

function agreementStateBadge(state: AgreementState) {
  switch (state) {
    case 'active': return <Badge variant="success">active</Badge>
    case 'draft': return <Badge variant="warning">draft</Badge>
    case 'expired': return <Badge variant="danger">expired</Badge>
    case 'terminated': return <Badge variant="secondary">terminated</Badge>
    default: return <Badge variant="secondary">{state}</Badge>
  }
}

function typeBadge(type: AgreementType) {
  switch (type) {
    case 'international': return <Badge variant="default">international</Badge>
    case 'national': return <Badge variant="secondary">national</Badge>
    case 'MVNO': return <Badge className="bg-purple-dim text-purple border-transparent">MVNO</Badge>
    default: return <Badge variant="outline">{type}</Badge>
  }
}

function AgreementsTab({ operatorId }: { operatorId: string }) {
  const navigate = useNavigate()
  const { data, isLoading, isError } = useOperatorRoamingAgreements(operatorId)
  const agreements = data?.data ?? []

  if (isLoading) return <div className="py-4 space-y-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}</div>
  if (isError) return <div className="py-4 text-sm text-danger flex items-center gap-2"><AlertCircle className="h-4 w-4" />Failed to load agreements.</div>

  return (
    <div className="space-y-3 py-2">
      <div className="flex justify-end">
        <Button size="sm" variant="outline" onClick={() => navigate(`/roaming-agreements?operator_id=${operatorId}`)}>
          <Plus className="h-4 w-4 mr-1" />
          New Agreement
        </Button>
      </div>
      {agreements.length === 0 ? (
        <EmptyState
          icon={Handshake}
          title="No roaming agreements"
          description="No roaming agreements have been created for this operator."
        />
      ) : (
        <Card className="overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="border-b border-border-subtle hover:bg-transparent">
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Partner</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Type</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">State</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">End Date</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {agreements.map((ag: RoamingAgreement) => (
                <TableRow
                  key={ag.id}
                  className="cursor-pointer border-b border-border-subtle hover:bg-bg-hover transition-colors"
                  onClick={() => navigate(`/roaming-agreements/${ag.id}`)}
                >
                  <TableCell className="py-2">{ag.partner_operator_name}</TableCell>
                  <TableCell className="py-2">{typeBadge(ag.agreement_type)}</TableCell>
                  <TableCell className="py-2">{agreementStateBadge(ag.state)}</TableCell>
                  <TableCell className="py-2 text-text-secondary">{ag.end_date}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </div>
  )
}

function OperatorSimsTab({ operatorId }: { operatorId: string }) {
  const navigate = useNavigate()
  const { data, isLoading, hasNextPage, fetchNextPage, isFetchingNextPage } = useSIMList({ operator_id: operatorId })
  const sims = data?.pages.flatMap((p) => p.data) ?? []

  return (
    <div className="mt-4 space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">
          Connected SIMs — {sims.length}{hasNextPage ? '+' : ''} total
        </p>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>ICCID</TableHead>
                <TableHead>IMSI</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>IP Address</TableHead>
                <TableHead>State</TableHead>
                <TableHead>APN</TableHead>
                <TableHead>RAT</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-28" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-14" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  </TableRow>
                ))}

              {!isLoading && sims.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8}>
                    <EmptyState
                      icon={Wifi}
                      title="No SIMs connected"
                      description="No SIM cards are connected to this operator."
                    />
                  </TableCell>
                </TableRow>
              )}

              {sims.map((sim, idx) => (
                <TableRow
                  key={sim.id}
                  data-row-index={idx}
                  data-href={`/sims/${sim.id}`}
                  className="cursor-pointer"
                  onClick={() => navigate(`/sims/${sim.id}`)}
                >
                  <TableCell>
                    <RowQuickPeek
                      title={sim.iccid}
                      fields={[
                        { label: 'IMSI', value: sim.imsi },
                        { label: 'State', value: sim.state },
                        { label: 'APN', value: sim.apn_name || '—' },
                        { label: 'Created', value: new Date(sim.created_at).toLocaleDateString() },
                      ]}
                    >
                      <span className="font-mono text-xs text-accent hover:underline">{sim.iccid}</span>
                    </RowQuickPeek>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{sim.imsi}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{sim.msisdn ?? '—'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{sim.ip_address || '—'}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(sim.state)} className="gap-1">
                      {sim.state === 'active' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {stateLabel(sim.state)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary truncate max-w-[120px] block">
                      {sim.apn_name || <span className="text-text-tertiary">—</span>}
                    </span>
                  </TableCell>
                  <TableCell>
                    <RATBadge ratType={sim.rat_type} />
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(sim.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>

      {hasNextPage && (
        <div className="text-center">
          <Button variant="ghost" size="sm" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
            {isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        </div>
      )}
    </div>
  )
}

export default function OperatorDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState('overview')
  const [testResult, setTestResult] = useState<{ success: boolean; latency_ms: number; error?: string } | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const { data: operator, isLoading, isError, refetch } = useOperator(id ?? '')
  const { data: health } = useOperatorHealth(id ?? '')
  const testMutation = useTestConnection(id ?? '')
  useRealtimeOperatorHealth()
  const addRecentItem = useUIStore((s) => s.addRecentItem)

  useEffect(() => {
    if (operator && id) {
      addRecentItem({ type: 'operator', id, label: `Op: ${operator.name}`, path: `/operators/${id}` })
    }
  }, [operator, id, addRecentItem])

  const handleTest = async () => {
    setTestResult(null)
    try {
      const result = await testMutation.mutateAsync()
      setTestResult(result)
    } catch {
      setTestResult({ success: false, latency_ms: 0, error: 'Request failed' })
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-48" />
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4">
                <Skeleton className="h-16 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  if (isError || !operator) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Operator not found</h2>
          <p className="text-sm text-text-secondary mb-4">The requested operator could not be loaded.</p>
          <div className="flex gap-2 justify-center">
            <Button onClick={() => navigate('/operators')} variant="outline" className="gap-2">
              <ArrowLeft className="h-4 w-4" />
              Back to Operators
            </Button>
            <Button onClick={() => refetch()} variant="outline" className="gap-2">
              <RefreshCw className="h-4 w-4" />
              Retry
            </Button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <Breadcrumb
        items={[
          { label: 'Dashboard', href: '/' },
          { label: 'Operators', href: '/operators' },
          { label: operator.name },
        ]}
        className="mb-1"
      />
      <div className="flex items-center gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <span
              className="h-3 w-3 rounded-full flex-shrink-0 pulse-dot"
              style={{
                backgroundColor: healthColor(operator.health_status),
                boxShadow: healthGlow(operator.health_status),
              }}
            />
            <h1 className="text-[16px] font-semibold text-text-primary truncate">
              {operator.name}
            </h1>
            <FavoriteToggle
              type="operator"
              id={id ?? ''}
              label={`Op: ${operator.name}`}
              path={`/operators/${id}`}
            />
            <Badge variant={healthVariant(operator.health_status)} className="gap-1 flex-shrink-0">
              {operator.health_status.toUpperCase()}
            </Badge>
            <div className="flex items-center gap-1 flex-shrink-0">
              {operator.enabled_protocols && operator.enabled_protocols.length > 0 ? (
                operator.enabled_protocols.map((p) => (
                  <Badge key={p} variant="outline" className="text-[10px]">
                    {ADAPTER_DISPLAY[p] ?? p}
                  </Badge>
                ))
              ) : (
                <Badge variant="outline" className="text-[10px]">—</Badge>
              )}
            </div>
          </div>
          <div className="flex items-center gap-4 mt-1">
            <span className="font-mono text-xs text-text-secondary">{operator.code}</span>
            <span className="font-mono text-xs text-text-tertiary">MCC {operator.mcc} / MNC {operator.mnc}</span>
            {operator.supported_rat_types.slice(0, 4).map((rat) => (
              <span
                key={rat}
                className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary"
              >
                {RAT_DISPLAY[rat] ?? rat}
              </span>
            ))}
          </div>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setEditOpen(true)}>
            <Pencil className="h-3.5 w-3.5" />
            Edit
          </Button>
          <Button variant="outline" size="sm" className="gap-1.5 border-danger/30 text-danger hover:bg-danger-dim" onClick={() => setDeleteOpen(true)}>
            <Trash2 className="h-3.5 w-3.5" />
            Delete
          </Button>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <Settings className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="protocols" className="gap-1.5">
            <Radio className="h-3.5 w-3.5" />
            Protocols
          </TabsTrigger>
          <TabsTrigger value="health" className="gap-1.5">
            <Activity className="h-3.5 w-3.5" />
            Health History
          </TabsTrigger>
          <TabsTrigger value="circuit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Circuit Breaker
          </TabsTrigger>
          <TabsTrigger value="traffic" className="gap-1.5">
            <BarChart3 className="h-3.5 w-3.5" />
            Traffic
          </TabsTrigger>
          <TabsTrigger value="sessions" className="gap-1.5">
            <Radio className="h-3.5 w-3.5" />
            Sessions
          </TabsTrigger>
          <TabsTrigger value="agreements" className="gap-1.5">
            <Handshake className="h-3.5 w-3.5" />
            Agreements
          </TabsTrigger>
          <TabsTrigger value="audit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
          <TabsTrigger value="alerts" className="gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            Alerts
          </TabsTrigger>
          <TabsTrigger value="notifications" className="gap-1.5">
            <Bell className="h-3.5 w-3.5" />
            Notifications
          </TabsTrigger>
          <TabsTrigger value="sims" className="gap-1.5">
            <Wifi className="h-3.5 w-3.5" />
            SIMs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <OverviewTab
            operator={operator}
            health={health}
            onTest={handleTest}
            testResult={testResult}
            isTesting={testMutation.isPending}
          />
        </TabsContent>
        <TabsContent value="protocols">
          <ProtocolsPanel operator={operator} />
          <SLATargetsSection operator={operator} />
        </TabsContent>
        <TabsContent value="health">
          <HealthTimelineTab operatorId={operator.id} />
        </TabsContent>
        <TabsContent value="circuit">
          <CircuitBreakerTab operator={operator} health={health} />
        </TabsContent>
        <TabsContent value="traffic">
          <TrafficTab operatorId={operator.id} />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsTab operatorId={operator.id} />
        </TabsContent>
        <TabsContent value="agreements">
          <AgreementsTab operatorId={operator.id} />
        </TabsContent>
        <TabsContent value="audit">
          <div className="mt-4">
            <RelatedAuditTab entityId={operator.id} entityType="operator" />
          </div>
        </TabsContent>
        <TabsContent value="alerts">
          <div className="mt-4">
            <RelatedAlertsPanel entityId={operator.id} entityType="operator" />
          </div>
        </TabsContent>
        <TabsContent value="notifications">
          <div className="mt-4">
            <RelatedNotificationsPanel entityId={operator.id} />
          </div>
        </TabsContent>
        <TabsContent value="sims">
          <OperatorSimsTab operatorId={operator.id} />
        </TabsContent>
      </Tabs>

      {operator && <EditOperatorDialog open={editOpen} onClose={() => setEditOpen(false)} operator={operator} onSuccess={() => { setEditOpen(false); refetch() }} />}

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent onClose={() => setDeleteOpen(false)}>
          <DialogHeader>
            <DialogTitle>Delete Operator?</DialogTitle>
            <DialogDescription>
              This will permanently remove operator "{operator?.name}". This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button
              variant="destructive"
              onClick={async () => {
                try {
                  await api.delete(`/operators/${id}`)
                  navigate('/operators')
                } catch {}
              }}
            >
              Delete Operator
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function SLATargetsSection({ operator }: { operator: NonNullable<ReturnType<typeof useOperator>['data']> }) {
  const [uptime, setUptime] = useState(String(operator.sla_uptime_target ?? 99.9))
  const [latency, setLatency] = useState(String(operator.sla_latency_threshold_ms ?? 500))
  const [errors, setErrors] = useState<{ uptime?: string; latency?: string }>({})
  const mutation = useUpdateOperatorSLA()

  const uptimeNum = Number(uptime)
  const latencyNum = Number(latency)

  const validateUptime = () => {
    if (Number.isNaN(uptimeNum) || uptimeNum < 50 || uptimeNum > 100) {
      setErrors(e => ({ ...e, uptime: 'Must be between 50 and 100' }))
    } else {
      setErrors(e => ({ ...e, uptime: undefined }))
    }
  }
  const validateLatency = () => {
    if (!Number.isInteger(latencyNum) || latencyNum < 50 || latencyNum > 60000) {
      setErrors(e => ({ ...e, latency: 'Must be integer 50–60000' }))
    } else {
      setErrors(e => ({ ...e, latency: undefined }))
    }
  }

  const isDirty = uptimeNum !== operator.sla_uptime_target || latencyNum !== (operator.sla_latency_threshold_ms ?? 500)
  const hasErrors = !!errors.uptime || !!errors.latency

  const handleSave = async () => {
    validateUptime()
    validateLatency()
    if (hasErrors) return
    try {
      await mutation.mutateAsync({
        id: operator.id,
        sla_uptime_target: uptimeNum,
        sla_latency_threshold_ms: latencyNum,
      })
      toast.success('SLA targets saved', { description: 'Applied to operator.' })
    } catch (err) {
      toast.error('Save failed', { description: String(err) })
    }
  }

  return (
    <div className="mt-6 rounded-[var(--radius-md)] border border-border bg-bg-surface p-6">
      <div className="flex items-center gap-2 mb-1">
        <h3 className="text-sm font-semibold text-text-primary">SLA Targets</h3>
        {isDirty && <span className="text-accent animate-pulse" aria-label="unsaved changes">●</span>}
      </div>
      <p className="text-xs text-text-secondary mb-4">Set uptime and latency SLOs for this operator.</p>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label htmlFor="sla-uptime" className="text-xs text-text-secondary">Uptime Target (%)</label>
          <Input
            id="sla-uptime"
            type="number"
            step="0.01"
            min={50}
            max={100}
            value={uptime}
            onChange={e => setUptime(e.target.value)}
            onBlur={validateUptime}
            aria-invalid={!!errors.uptime}
            className="h-8 text-sm font-mono mt-1"
          />
          <p className="text-[11px] text-text-tertiary mt-0.5">50 – 100%</p>
          {errors.uptime && <p role="alert" className="text-xs text-danger mt-1">{errors.uptime}</p>}
        </div>
        <div>
          <label htmlFor="sla-latency" className="text-xs text-text-secondary">Latency Threshold (ms)</label>
          <Input
            id="sla-latency"
            type="number"
            step={1}
            min={50}
            max={60000}
            value={latency}
            onChange={e => setLatency(e.target.value)}
            onBlur={validateLatency}
            aria-invalid={!!errors.latency}
            className="h-8 text-sm font-mono mt-1"
          />
          <p className="text-[11px] text-text-tertiary mt-0.5">50 – 60,000 ms</p>
          {errors.latency && <p role="alert" className="text-xs text-danger mt-1">{errors.latency}</p>}
        </div>
      </div>
      <div className="flex justify-end mt-4">
        <Button size="sm" onClick={handleSave} disabled={!isDirty || mutation.isPending || hasErrors}>
          {mutation.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}
