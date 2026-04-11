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
  useOperator,
  useOperatorHealth,
  useTestConnection,
  useRealtimeOperatorHealth,
  useUpdateOperator,
} from '@/hooks/use-operators'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'
import { RAT_DISPLAY } from '@/lib/constants'
import { api } from '@/lib/api'
import { InfoRow } from '@/components/ui/info-row'

const ADAPTER_DISPLAY: Record<string, string> = {
  mock: 'Mock',
  radius: 'RADIUS',
  diameter: 'Diameter',
  sba: '5G SBA',
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
            <InfoRow label="Protocol" value={ADAPTER_DISPLAY[operator.adapter_type] ?? operator.adapter_type} />
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

function HealthTimelineTab() {
  const mockTimeline = useMemo(() => {
    const entries = []
    const statuses = ['healthy', 'degraded', 'down', 'healthy']
    const now = Date.now()
    for (let i = 0; i < 20; i++) {
      const status = statuses[Math.floor(Math.random() * (i < 15 ? 1 : statuses.length))]
      entries.push({
        id: i,
        status,
        latency_ms: status === 'down' ? null : Math.floor(Math.random() * 200) + 10,
        circuit_state: status === 'down' ? 'open' : status === 'degraded' ? 'half_open' : 'closed',
        checked_at: new Date(now - i * 300_000).toISOString(),
        error: status === 'down' ? 'Connection timeout' : undefined,
      })
    }
    return entries
  }, [])

  return (
    <Card>
      <CardHeader>
        <CardTitle>Health Check History</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="relative pl-6">
          <div className="absolute left-[11px] top-0 bottom-0 w-px bg-border" />
          {mockTimeline.map((entry) => (
            <div key={entry.id} className="relative pb-4 last:pb-0">
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
                {entry.error && (
                  <p className="text-xs text-danger mt-1">{entry.error}</p>
                )}
              </div>
            </div>
          ))}
        </div>
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

function TrafficTab() {
  const mockAuthData = useMemo(() => {
    return Array.from({ length: 24 }, (_, i) => ({
      hour: `${String(i).padStart(2, '0')}:00`,
      auth_rate: Math.floor(Math.random() * 500) + 50,
      error_rate: Math.floor(Math.random() * 20),
    }))
  }, [])

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Authentication Rate (24h)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[280px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={mockAuthData}>
                <defs>
                  <linearGradient id="opGradAuth" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis
                  dataKey="hour"
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
                  interval={3}
                />
                <YAxis
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
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
                  formatter={(value, name) => [value, name === 'auth_rate' ? 'Auth/s' : 'Errors/s']}
                />
                <Area
                  type="monotone"
                  dataKey="auth_rate"
                  stroke="var(--color-accent)"
                  fill="url(#opGradAuth)"
                  strokeWidth={2}
                  name="auth_rate"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Error Rate (24h)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[200px]">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={mockAuthData}>
                <XAxis
                  dataKey="hour"
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
                  interval={3}
                />
                <YAxis
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
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
                  formatter={(value) => [value, 'Errors/s']}
                />
                <Line
                  type="monotone"
                  dataKey="error_rate"
                  stroke="var(--color-danger)"
                  strokeWidth={2}
                  dot={false}
                  name="error_rate"
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="font-mono text-xl font-bold text-accent">
              {Math.round(mockAuthData.reduce((a, d) => a + d.auth_rate, 0) / mockAuthData.length)}/s
            </div>
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Avg Auth Rate</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="font-mono text-xl font-bold text-danger">
              {Math.round(mockAuthData.reduce((a, d) => a + d.error_rate, 0) / mockAuthData.length)}/s
            </div>
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Avg Error Rate</div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

const ADAPTER_OPTIONS = [
  { value: 'mock', label: 'Mock' },
  { value: 'radius', label: 'RADIUS' },
  { value: 'diameter', label: 'Diameter' },
  { value: 'sba', label: '5G SBA' },
]

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
    adapter_type: operator.adapter_type,
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
        adapter_type: form.adapter_type,
        supported_rat_types: form.supported_rat_types,
      })
      onSuccess()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      setError(msg ?? 'Failed to update operator')
    }
  }

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
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Adapter Type *</label>
          <Select value={form.adapter_type} onChange={(e) => setForm((f) => ({ ...f, adapter_type: e.target.value }))} className="h-8 text-sm" options={ADAPTER_OPTIONS} />
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
            <Badge variant={healthVariant(operator.health_status)} className="gap-1 flex-shrink-0">
              {operator.health_status.toUpperCase()}
            </Badge>
            <Badge variant="outline" className="text-[10px] flex-shrink-0">
              {ADAPTER_DISPLAY[operator.adapter_type] ?? operator.adapter_type}
            </Badge>
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
        <TabsContent value="health">
          <HealthTimelineTab />
        </TabsContent>
        <TabsContent value="circuit">
          <CircuitBreakerTab operator={operator} health={health} />
        </TabsContent>
        <TabsContent value="traffic">
          <TrafficTab />
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
