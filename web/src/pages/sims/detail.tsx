import { useState, useMemo } from 'react'
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
import { Spinner } from '@/components/ui/spinner'
import {
  useSIM,
  useSIMHistory,
  useSIMSessions,
  useSIMDiagnostics,
  useSIMStateAction,
} from '@/hooks/use-sims'
import { Skeleton } from '@/components/ui/skeleton'
import type { SIM, SIMState, DiagnosticResult } from '@/types/sim'
import { cn } from '@/lib/utils'
import { RAT_DISPLAY } from '@/lib/constants'
import { formatBytes, formatDuration, timeAgo } from '@/lib/format'
import { stateVariant, stateLabel } from '@/lib/sim-utils'

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
          <InfoRow label="Operator ID" value={sim.operator_id} mono />
          <InfoRow label="APN ID" value={sim.apn_id ?? 'Not assigned'} mono={!!sim.apn_id} />
          <InfoRow label="RAT Type" value={sim.rat_type ? (RAT_DISPLAY[sim.rat_type] ?? sim.rat_type) : 'Not set'} />
          <InfoRow label="IP Address ID" value={sim.ip_address_id ?? 'Not allocated'} mono={!!sim.ip_address_id} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Policy & Session</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Policy Version" value={sim.policy_version_id ?? 'None'} mono={!!sim.policy_version_id} />
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
    </div>
  )
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-xs text-text-secondary">{label}</span>
      <span className={cn('text-sm text-text-primary', mono && 'font-mono text-xs')}>
        {value}
      </span>
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
                <span className="font-mono text-xs text-text-secondary">{session.acct_session_id.slice(0, 12)}...</span>
              </TableCell>
              <TableCell>
                <Badge variant={session.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">
                  {session.state.toUpperCase()}
                </Badge>
              </TableCell>
              <TableCell><span className="font-mono text-xs text-text-secondary">{session.nas_ip}</span></TableCell>
              <TableCell><span className="font-mono text-xs text-text-secondary">{session.framed_ip || '-'}</span></TableCell>
              <TableCell>
                {session.rat_type ? (
                  <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary">
                    {RAT_DISPLAY[session.rat_type] ?? session.rat_type}
                  </span>
                ) : '-'}
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
          <button
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1 flex items-center justify-center gap-2"
          >
            {isFetchingNextPage && <Spinner className="h-3 w-3" />}
            {isFetchingNextPage ? 'Loading...' : 'Load more sessions'}
          </button>
        </div>
      )}
    </Card>
  )
}

function UsageTab({ simId }: { simId: string }) {
  const mockUsageData = useMemo(() => {
    return Array.from({ length: 30 }, (_, i) => ({
      day: `Day ${i + 1}`,
      bytes_in: Math.floor(Math.random() * 100_000_000),
      bytes_out: Math.floor(Math.random() * 50_000_000),
    }))
  }, [])

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>30-Day Usage Trend</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[260px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={mockUsageData}>
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
                  dataKey="day"
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
                  interval={4}
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
                {formatBytes(mockUsageData.reduce((a, d) => a + d.bytes_in, 0))}
              </div>
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total In</div>
            </div>
            <div className="text-center">
              <div className="font-mono text-xl font-bold text-purple">
                {formatBytes(mockUsageData.reduce((a, d) => a + d.bytes_out, 0))}
              </div>
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total Out</div>
            </div>
            <div className="text-center">
              <div className="font-mono text-xl font-bold text-text-primary">
                {formatBytes(mockUsageData.reduce((a, d) => a + d.bytes_in + d.bytes_out, 0))}
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
            <button
              onClick={() => fetchNextPage()}
              disabled={isFetchingNextPage}
              className="text-xs text-text-tertiary hover:text-accent transition-colors flex items-center justify-center gap-2 mx-auto"
            >
              {isFetchingNextPage && <Spinner className="h-3 w-3" />}
              {isFetchingNextPage ? 'Loading...' : 'Load more history'}
            </button>
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
      <div className="p-6 space-y-4">
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
    <div className="p-6 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="icon" onClick={() => navigate('/sims')}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
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
            {sim.rat_type && (
              <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary">
                {RAT_DISPLAY[sim.rat_type] ?? sim.rat_type}
              </span>
            )}
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
          <OverviewTab sim={sim} />
        </TabsContent>

        <TabsContent value="sessions">
          <SessionsTab simId={sim.id} />
        </TabsContent>

        <TabsContent value="usage">
          <UsageTab simId={sim.id} />
        </TabsContent>

        <TabsContent value="diagnostics">
          <DiagnosticsTab simId={sim.id} />
        </TabsContent>

        <TabsContent value="history">
          <HistoryTab simId={sim.id} />
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
