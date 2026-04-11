import { useState, useMemo, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Play,
  Pause,
  XCircle,
  AlertTriangle,
  Shield,
  Activity,
  BarChart3,
  Stethoscope,
  Clock,
  RefreshCw,
  AlertCircle,
  Loader2,
  CheckCircle2,
  XOctagon,
  Info,
  ChevronRight,
  Smartphone,
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
import { Input } from '@/components/ui/input'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
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
import { Spinner } from '@/components/ui/spinner'
import {
  useSIM,
  useSIMHistory,
  useSIMSessions,
  useSIMDiagnostics,
  useSIMStateAction,
  useSIMUsage,
} from '@/hooks/use-sims'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { TimeframeSelector } from '@/components/ui/timeframe-selector'
import type { SIM, SIMState, DiagnosticResult, SIMUsageData } from '@/types/sim'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'
import { formatBytes, formatDuration, timeAgo } from '@/lib/format'
import { InfoRow } from '@/components/ui/info-row'
import { RATBadge } from '@/components/ui/rat-badge'
import { stateVariant, stateLabel } from '@/lib/sim-utils'
import { ErrorBoundary } from '@/components/error-boundary'
import { ESimTab } from './esim-tab'

function allowedActions(state: SIMState): Array<{ action: string; label: string; icon: React.ElementType; variant: 'default' | 'destructive' | 'outline' }> {
  switch (state) {
    case 'ordered':
      return [
        { action: 'activate', label: 'Activate', icon: Play, variant: 'default' },
      ]
    case 'active':
      return [
        { action: 'suspend', label: 'Suspend', icon: Pause, variant: 'outline' },
        { action: 'terminate', label: 'Terminate', icon: XCircle, variant: 'destructive' },
        { action: 'report-lost', label: 'Report Lost', icon: AlertTriangle, variant: 'destructive' },
      ]
    case 'suspended':
      return [
        { action: 'resume', label: 'Resume', icon: Play, variant: 'default' },
        { action: 'terminate', label: 'Terminate', icon: XCircle, variant: 'destructive' },
      ]
    default:
      return []
  }
}

function OverviewTab({ sim }: { sim: SIM }) {
  const [reserveOpen, setReserveOpen] = useState(false)

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card>
        <CardHeader>
          <CardTitle>Identification</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="ICCID" value={sim.iccid} mono />
          <InfoRow label="IMSI" value={sim.imsi} mono />
          <InfoRow label="MSISDN" value={sim.msisdn ?? 'Not assigned'} mono={!!sim.msisdn} />
          <InfoRow label="Type" value={sim.sim_type === 'esim' ? 'eSIM' : 'Physical'} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Operator" value={sim.operator_name || sim.operator_id} mono={!sim.operator_name} />
          <InfoRow label="APN" value={sim.apn_name || sim.apn_id || 'Not assigned'} mono={!sim.apn_name && !!sim.apn_id} />
          <InfoRow label="RAT Type" value={sim.rat_type ? <RATBadge ratType={sim.rat_type} /> : 'Not set'} />
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-secondary">IP Address</span>
            <div className="flex items-center gap-2">
              <span className={cn('text-sm text-text-primary', sim.ip_address && 'font-mono text-xs')}>{sim.ip_address || 'Not allocated'}</span>
              {!sim.ip_address && sim.apn_id && sim.state === 'active' && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setReserveOpen(true)}
                  className="text-[10px] text-accent hover:underline h-auto py-0 px-1"
                >
                  Reserve Static IP
                </Button>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Policy & Session</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Policy" value={sim.policy_name || sim.policy_version_id || 'None'} mono={!sim.policy_name && !!sim.policy_version_id} />
          <InfoRow label="eSIM Profile" value={sim.esim_profile_id ?? 'N/A'} mono={!!sim.esim_profile_id} />
          <InfoRow label="Max Concurrent Sessions" value={String(sim.max_concurrent_sessions)} />
          <InfoRow label="Idle Timeout" value={formatDuration(sim.session_idle_timeout_sec)} />
          <InfoRow label="Hard Timeout" value={formatDuration(sim.session_hard_timeout_sec)} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Timeline</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Created" value={new Date(sim.created_at).toLocaleString()} />
          <InfoRow label="Last Updated" value={new Date(sim.updated_at).toLocaleString()} />
          {sim.activated_at && (
            <InfoRow label="Activated" value={new Date(sim.activated_at).toLocaleString()} />
          )}
          {sim.suspended_at && (
            <InfoRow label="Suspended" value={new Date(sim.suspended_at).toLocaleString()} />
          )}
          {sim.terminated_at && (
            <InfoRow label="Terminated" value={new Date(sim.terminated_at).toLocaleString()} />
          )}
          {sim.purge_at && (
            <InfoRow label="Purge Scheduled" value={new Date(sim.purge_at).toLocaleString()} />
          )}
        </CardContent>
      </Card>

      <SlidePanel open={reserveOpen} onOpenChange={setReserveOpen} title="Reserve Static IP" description="Reserve a static IP address for this SIM" width="sm">
        <div className="space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-text-secondary">SIM</span>
            <span className="font-mono text-xs text-accent">{sim.iccid}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-text-secondary">APN</span>
            <span className="text-text-primary">{sim.apn_name || sim.apn_id || '-'}</span>
          </div>
        </div>
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
          <Button variant="outline" onClick={() => setReserveOpen(false)}>Cancel</Button>
          <Button
            onClick={async () => {
              if (!sim.apn_id) return
              try {
                const poolsRes = await api.get<{ data: { id: string }[] }>(`/ip-pools?apn_id=${sim.apn_id}&limit=1`)
                const pool = poolsRes.data.data?.[0]
                if (!pool) return
                await api.post(`/ip-pools/${pool.id}/addresses/reserve`, { sim_id: sim.id })
                setReserveOpen(false)
                window.location.reload()
              } catch {
                // handled by api interceptor
              }
            }}
            className="gap-2"
          >
            Reserve IP
          </Button>
        </div>
      </SlidePanel>
    </div>
  )
}

function SessionsTab({ simId }: { simId: string }) {
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useSIMSessions(simId)

  const allSessions = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((p) => p.data)
  }, [data])

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full mb-2" />
          ))}
        </CardContent>
      </Card>
    )
  }

  if (allSessions.length === 0) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-center">
          <Activity className="h-8 w-8 text-text-tertiary mb-3" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">No sessions found</h3>
          <p className="text-xs text-text-secondary">This SIM has no session history yet.</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <Table>
        <TableHeader className="bg-bg-elevated">
          <TableRow>
            <TableHead>Session ID</TableHead>
            <TableHead>State</TableHead>
            <TableHead>NAS IP</TableHead>
            <TableHead>Framed IP</TableHead>
            <TableHead>RAT</TableHead>
            <TableHead>Data In</TableHead>
            <TableHead>Data Out</TableHead>
            <TableHead>Duration</TableHead>
            <TableHead>Started</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {allSessions.map((session) => (
            <TableRow key={session.id}>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{session.acct_session_id ? session.acct_session_id.slice(0, 12) + '...' : '-'}</span>
              </TableCell>
              <TableCell>
                <Badge variant={session.session_state === 'active' ? 'success' : 'secondary'} className="text-[10px]">
                  {session.session_state.toUpperCase()}
                </Badge>
              </TableCell>
              <TableCell><span className="font-mono text-xs text-text-secondary">{session.nas_ip || '-'}</span></TableCell>
              <TableCell><span className="font-mono text-xs text-text-secondary">{session.framed_ip || '-'}</span></TableCell>
              <TableCell>
                <RATBadge ratType={session.rat_type} />
              </TableCell>
              <TableCell><span className="font-mono text-xs">{formatBytes(session.bytes_in)}</span></TableCell>
              <TableCell><span className="font-mono text-xs">{formatBytes(session.bytes_out)}</span></TableCell>
              <TableCell><span className="font-mono text-xs">{formatDuration(session.duration_sec)}</span></TableCell>
              <TableCell><span className="text-xs text-text-secondary">{timeAgo(session.started_at)}</span></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {hasNextPage && (
        <div className="px-4 py-3 border-t border-border-subtle">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1 flex items-center justify-center gap-2 h-auto"
          >
            {isFetchingNextPage && <Spinner className="h-3 w-3" />}
            {isFetchingNextPage ? 'Loading...' : 'Load more sessions'}
          </Button>
        </div>
      )}
    </Card>
  )
}

function UsageTab({ simId }: { simId: string }) {
  const [timeframe, setTimeframe] = useState('30d')
  const { data: usageData, isLoading } = useSIMUsage(simId, timeframe)
  const usage = usageData as SIMUsageData | undefined

  const chartData = useMemo(() => {
    if (!usage?.series?.length) return []
    return usage.series.map((b) => ({
      label: b.bucket,
      bytes_in: b.bytes_in,
      bytes_out: b.bytes_out,
    }))
  }, [usage])

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-text-primary">Usage Trend</span>
        <TimeframeSelector value={timeframe} onChange={setTimeframe} />
      </div>
      <Card>
        <CardContent className="pt-4">
          <div className="h-[260px]">
            {isLoading ? (
              <div className="flex h-full items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-text-tertiary" />
              </div>
            ) : chartData.length === 0 ? (
              <div className="flex h-full items-center justify-center text-sm text-text-tertiary">
                No usage data for this period
              </div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="gradIn" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="gradOut" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-purple)" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="var(--color-purple)" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis
                    dataKey="label"
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    interval={Math.max(0, Math.floor(chartData.length / 8) - 1)}
                  />
                  <YAxis
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(v) => formatBytes(v)}
                    width={60}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: 'var(--color-bg-elevated)',
                      border: '1px solid var(--color-border)',
                      borderRadius: 'var(--radius-sm)',
                      color: 'var(--color-text-primary)',
                      fontSize: '12px',
                    }}
                    formatter={(value) => [formatBytes(Number(value))]}
                  />
                  <Area
                    type="monotone"
                    dataKey="bytes_in"
                    stroke="var(--color-accent)"
                    fill="url(#gradIn)"
                    strokeWidth={2}
                    name="Data In"
                  />
                  <Area
                    type="monotone"
                    dataKey="bytes_out"
                    stroke="var(--color-purple)"
                    fill="url(#gradOut)"
                    strokeWidth={2}
                    name="Data Out"
                  />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Usage Summary</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-4">
            <div className="text-center">
              <div className="font-mono text-xl font-bold text-accent">
                {formatBytes(usage?.total_bytes_in ?? 0)}
              </div>
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total In</div>
            </div>
            <div className="text-center">
              <div className="font-mono text-xl font-bold text-purple">
                {formatBytes(usage?.total_bytes_out ?? 0)}
              </div>
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total Out</div>
            </div>
            <div className="text-center">
              <div className="font-mono text-xl font-bold text-text-primary">
                {formatBytes((usage?.total_bytes_in ?? 0) + (usage?.total_bytes_out ?? 0))}
              </div>
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total</div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function DiagnosticsTab({ simId }: { simId: string }) {
  const diagMutation = useSIMDiagnostics(simId)
  const [result, setResult] = useState<DiagnosticResult | null>(null)

  const runDiagnostics = async () => {
    const res = await diagMutation.mutateAsync(false)
    setResult(res)
  }

  const statusIcon = (status: string) => {
    switch (status) {
      case 'pass': return <CheckCircle2 className="h-4 w-4 text-success flex-shrink-0" />
      case 'fail': return <XOctagon className="h-4 w-4 text-danger flex-shrink-0" />
      case 'warn': return <AlertTriangle className="h-4 w-4 text-warning flex-shrink-0" />
      default: return <Info className="h-4 w-4 text-text-tertiary flex-shrink-0" />
    }
  }

  const overallVariant = (status: string): 'success' | 'warning' | 'danger' => {
    switch (status) {
      case 'healthy': return 'success'
      case 'degraded': return 'warning'
      case 'critical': return 'danger'
      default: return 'warning'
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Connectivity Diagnostics</CardTitle>
          <Button
            onClick={runDiagnostics}
            disabled={diagMutation.isPending}
            size="sm"
            className="gap-2"
          >
            {diagMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Stethoscope className="h-3.5 w-3.5" />
            )}
            {diagMutation.isPending ? 'Running...' : 'Run Diagnostics'}
          </Button>
        </CardHeader>
        <CardContent>
          {!result && !diagMutation.isPending && (
            <div className="flex flex-col items-center justify-center py-12 text-center">
              <Stethoscope className="h-8 w-8 text-text-tertiary mb-3" />
              <h3 className="text-sm font-semibold text-text-primary mb-1">No diagnostics run yet</h3>
              <p className="text-xs text-text-secondary">
                Click "Run Diagnostics" to check this SIM's connectivity status.
              </p>
            </div>
          )}

          {diagMutation.isPending && (
            <div className="flex flex-col items-center justify-center py-12">
              <Spinner className="h-8 w-8 text-accent mb-3" />
              <p className="text-sm text-text-secondary">Running connectivity diagnostics...</p>
            </div>
          )}

          {result && (
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <span className="text-sm text-text-secondary">Overall Status:</span>
                <Badge variant={overallVariant(result.overall_status)} className="text-xs">
                  {result.overall_status.toUpperCase()}
                </Badge>
                <span className="text-xs text-text-tertiary ml-auto">
                  {new Date(result.diagnosed_at).toLocaleString()}
                </span>
              </div>

              <div className="space-y-2">
                {result.steps.map((step) => (
                  <div
                    key={step.step}
                    className="flex items-start gap-3 p-3 rounded-[var(--radius-sm)] bg-bg-elevated border border-border"
                  >
                    {statusIcon(step.status)}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-0.5">
                        <span className="text-xs font-mono text-text-tertiary">Step {step.step}</span>
                        <span className="text-sm font-medium text-text-primary">{step.name}</span>
                      </div>
                      <p className="text-xs text-text-secondary">{step.message}</p>
                      {step.suggestion && (
                        <p className="text-xs text-warning mt-1">{step.suggestion}</p>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function HistoryTab({ simId }: { simId: string }) {
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useSIMHistory(simId)

  const allHistory = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((p) => p.data)
  }, [data])

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full mb-2" />
          ))}
        </CardContent>
      </Card>
    )
  }

  if (allHistory.length === 0) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-center">
          <Clock className="h-8 w-8 text-text-tertiary mb-3" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">No history yet</h3>
          <p className="text-xs text-text-secondary">State transition history will appear here.</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <CardTitle>State Transition Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="relative pl-6">
          <div className="absolute left-[11px] top-0 bottom-0 w-px bg-border" />
          {allHistory.map((entry, idx) => (
            <div key={entry.id} className="relative pb-6 last:pb-0">
              <div
                className={cn(
                  'absolute left-[-13px] top-1 h-3 w-3 rounded-full border-2 border-bg-surface',
                  entry.to_state === 'active' ? 'bg-success' :
                  entry.to_state === 'suspended' ? 'bg-warning' :
                  entry.to_state === 'terminated' ? 'bg-danger' :
                  entry.to_state === 'stolen_lost' ? 'bg-danger' :
                  'bg-accent',
                )}
              />
              <div className="ml-4">
                <div className="flex items-center gap-2 mb-1">
                  {entry.from_state && (
                    <>
                      <Badge variant="secondary" className="text-[10px]">
                        {stateLabel(entry.from_state)}
                      </Badge>
                      <ChevronRight className="h-3 w-3 text-text-tertiary" />
                    </>
                  )}
                  <Badge variant={stateVariant(entry.to_state as SIMState)} className="text-[10px]">
                    {stateLabel(entry.to_state)}
                  </Badge>
                </div>
                <div className="flex items-center gap-3 text-xs text-text-secondary">
                  <span>{new Date(entry.created_at).toLocaleString()}</span>
                  <span className="text-text-tertiary">by {entry.triggered_by}</span>
                </div>
                {entry.reason && (
                  <p className="text-xs text-text-tertiary mt-1">Reason: {entry.reason}</p>
                )}
              </div>
            </div>
          ))}
        </div>
        {hasNextPage && (
          <div className="mt-4 text-center">
            <Button
              variant="ghost"
              onClick={() => fetchNextPage()}
              disabled={isFetchingNextPage}
              className="text-xs text-text-tertiary hover:text-accent flex items-center justify-center gap-2 mx-auto"
            >
              {isFetchingNextPage && <Spinner className="h-3 w-3" />}
              {isFetchingNextPage ? 'Loading...' : 'Load more history'}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export default function SimDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState('overview')
  const [actionDialog, setActionDialog] = useState<{
    action: string
    label: string
    variant: 'default' | 'destructive'
  } | null>(null)
  const [actionReason, setActionReason] = useState('')

  const { data: sim, isLoading, isError, refetch } = useSIM(id ?? '')
  const stateAction = useSIMStateAction()
  const addRecentItem = useUIStore((s) => s.addRecentItem)

  useEffect(() => {
    if (sim && id) {
      addRecentItem({ type: 'sim', id, label: `SIM ${sim.iccid?.slice(-8) ?? id.slice(0, 8)}`, path: `/sims/${id}` })
    }
  }, [sim, id, addRecentItem])

  const handleStateAction = async () => {
    if (!actionDialog || !id) return
    try {
      await stateAction.mutateAsync({
        simId: id,
        action: actionDialog.action as 'activate' | 'suspend' | 'resume' | 'terminate' | 'report-lost',
        reason: actionReason || undefined,
      })
      setActionDialog(null)
      setActionReason('')
      refetch()
    } catch {
      // error handled by api interceptor
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-48" />
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4">
                <Skeleton className="h-40 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  if (isError || !sim) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">SIM not found</h2>
          <p className="text-sm text-text-secondary mb-4">The requested SIM could not be loaded.</p>
          <div className="flex gap-2 justify-center">
            <Button onClick={() => navigate('/sims')} variant="outline" className="gap-2">
              <ArrowLeft className="h-4 w-4" />
              Back to SIMs
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

  const actions = allowedActions(sim.state)

  return (
    <div className="space-y-4">
      {/* Header */}
      <Breadcrumb
        items={[
          { label: 'Dashboard', href: '/' },
          { label: 'SIM Cards', href: '/sims' },
          { label: sim.iccid?.slice(-8) ?? 'Detail' },
        ]}
        className="mb-1"
      />
      <div className="flex items-center gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-[16px] font-semibold text-text-primary truncate">
              SIM {sim.iccid}
            </h1>
            <Badge variant={stateVariant(sim.state)} className="gap-1 flex-shrink-0">
              {sim.state === 'active' && (
                <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
              )}
              {stateLabel(sim.state)}
            </Badge>
            {sim.sim_type === 'esim' && (
              <Badge variant="default" className="text-[10px] flex-shrink-0">eSIM</Badge>
            )}
          </div>
          <div className="flex items-center gap-4 mt-1">
            <span className="font-mono text-xs text-text-secondary">IMSI: {sim.imsi}</span>
            {sim.msisdn && (
              <span className="font-mono text-xs text-text-secondary">MSISDN: {sim.msisdn}</span>
            )}
            {sim.rat_type && <RATBadge ratType={sim.rat_type} />}
          </div>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          {actions.map((a) => (
            <Button
              key={a.action}
              variant={a.variant}
              size="sm"
              className="gap-1.5"
              onClick={() =>
                setActionDialog({
                  action: a.action,
                  label: a.label,
                  variant: a.variant === 'destructive' ? 'destructive' : 'default',
                })
              }
            >
              <a.icon className="h-3.5 w-3.5" />
              {a.label}
            </Button>
          ))}
        </div>
      </div>

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          {sim.sim_type === 'esim' && (
            <TabsTrigger value="esim" className="gap-1.5">
              <Smartphone className="h-3.5 w-3.5" />
              eSIM
            </TabsTrigger>
          )}
          <TabsTrigger value="sessions" className="gap-1.5">
            <Activity className="h-3.5 w-3.5" />
            Sessions
          </TabsTrigger>
          <TabsTrigger value="usage" className="gap-1.5">
            <BarChart3 className="h-3.5 w-3.5" />
            Usage
          </TabsTrigger>
          <TabsTrigger value="diagnostics" className="gap-1.5">
            <Stethoscope className="h-3.5 w-3.5" />
            Diagnostics
          </TabsTrigger>
          <TabsTrigger value="history" className="gap-1.5">
            <Clock className="h-3.5 w-3.5" />
            History
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <ErrorBoundary>
            <OverviewTab sim={sim} />
          </ErrorBoundary>
        </TabsContent>

        {sim.sim_type === 'esim' && (
          <TabsContent value="esim">
            <ErrorBoundary>
              <ESimTab simId={sim.id} />
            </ErrorBoundary>
          </TabsContent>
        )}

        <TabsContent value="sessions">
          <ErrorBoundary>
            <SessionsTab simId={sim.id} />
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="usage">
          <ErrorBoundary>
            <UsageTab simId={sim.id} />
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="diagnostics">
          <ErrorBoundary>
            <DiagnosticsTab simId={sim.id} />
          </ErrorBoundary>
        </TabsContent>

        <TabsContent value="history">
          <ErrorBoundary>
            <HistoryTab simId={sim.id} />
          </ErrorBoundary>
        </TabsContent>
      </Tabs>

      {/* State Action Dialog */}
      <Dialog open={!!actionDialog} onOpenChange={() => setActionDialog(null)}>
        <DialogContent onClose={() => setActionDialog(null)}>
          <DialogHeader>
            <DialogTitle>
              {actionDialog?.label} SIM?
            </DialogTitle>
            <DialogDescription>
              You are about to {actionDialog?.label.toLowerCase()} SIM {sim.iccid}.
              {actionDialog?.action === 'terminate' && ' This action cannot be undone.'}
            </DialogDescription>
          </DialogHeader>
          {actionDialog?.action !== 'activate' && (
            <div className="py-2">
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Reason {actionDialog?.action === 'activate' ? '(optional)' : '(optional)'}
              </label>
              <Input
                value={actionReason}
                onChange={(e) => setActionReason(e.target.value)}
                placeholder="Enter reason..."
              />
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setActionDialog(null)}>
              Cancel
            </Button>
            <Button
              variant={actionDialog?.variant === 'destructive' ? 'destructive' : 'default'}
              onClick={handleStateAction}
              disabled={stateAction.isPending}
              className="gap-2"
            >
              {stateAction.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              {actionDialog?.label}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
