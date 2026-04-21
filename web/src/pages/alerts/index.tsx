import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle, AlertTriangle, CheckCircle, Clock, Shield,
  ChevronDown, ChevronUp, Search, BellOff, ExternalLink, BookOpen,
  RefreshCw, Eye, Radio, Zap, Wifi, WifiOff, Database, Lock,
  Activity, TrendingUp, MessageSquare, Download, Loader2, Repeat,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { SEVERITY_FILTER_OPTIONS, SEVERITY_PILL_CLASSES } from '@/lib/severity'
import { ALERT_SOURCE_OPTIONS, ALERT_STATE_OPTIONS, formatOccurrence } from '@/lib/alerts'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Skeleton } from '@/components/ui/skeleton'
import { RowActionsMenu } from '@/components/shared/row-actions-menu'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import { cn } from '@/lib/utils'
import { timeAgo, formatNumber } from '@/lib/format'
import type { Alert } from '@/types/analytics'
import type { ListResponse } from '@/types/sim'
import { AlertActionButtons } from './_partials/alert-actions'
import { useExport } from '@/hooks/use-export'
import { CommentThread } from './_partials/comment-thread'

interface AlertFilters {
  severity: string
  state: string
  type: string
  source: string
  q: string
}

const RUNBOOKS: Record<string, string[]> = {
  operator_down: [
    'Check circuit breaker configuration',
    'Test operator connection via health check',
    'Review operator auth logs for failure patterns',
    'Contact operator NOC if issue persists',
  ],
  auth_spike: [
    'Verify no mass device reconnect event',
    'Check for DDoS or brute force patterns',
    'Review per-operator auth rates',
    'Enable enhanced rate limiting if needed',
  ],
  auth_flood: [
    'Verify no mass device reconnect event',
    'Check for DDoS or brute force patterns',
    'Review per-operator auth rates',
    'Enable enhanced rate limiting if needed',
  ],
  ip_pool_exhaustion: [
    'Check IP pool utilization dashboard',
    'Reclaim unused static IP reservations',
    'Expand pool CIDR range',
    'Enable dynamic IP recycling policy',
  ],
  sim_cloning: [
    'Isolate affected SIM immediately',
    'Review auth logs for dual-IMSI activity',
    'Generate compliance report',
    'Notify security team',
  ],
  data_spike: [
    'Compare usage against policy limits',
    'Check for SIM compromise',
    'Review connected device behavior',
    'Apply throttling policy if needed',
  ],
  usage_spike: [
    'Compare usage against policy limits',
    'Check for SIM compromise',
    'Review connected device behavior',
    'Apply throttling policy if needed',
  ],
  policy_violation: [
    'Review policy dry-run results',
    'Check affected SIM configurations',
    'Rollback policy if unintended',
    'Update policy rules as needed',
  ],
  velocity_anomaly: [
    'Check SIM physical location history',
    'Verify if SIM is in a moving vehicle (legitimate)',
    'Review for potential SIM swap or cloning',
    'Flag SIM for enhanced monitoring',
  ],
  location_anomaly: [
    'Compare reported location with previous sessions',
    'Check for VPN or proxy usage',
    'Verify device movement patterns',
    'Escalate if impossible travel detected',
  ],
  credential_stuffing: [
    'Block source IP ranges immediately',
    'Enable CAPTCHA or additional auth factors',
    'Review affected account credentials',
    'Notify affected users and force password reset',
  ],
  nats_consumer_lag: [
    'Rebalance consumer group assignments',
    'Check for stuck or unprocessable messages',
    'Scale workers to handle backlog',
    'Inspect dead-letter queue for errors',
  ],
  'storage.hypertable_growth': [
    'Compress older TimescaleDB chunks',
    'Add or adjust data retention policy',
    'Check TimescaleDB health and chunk sizes',
    'Review ingestion rate for unexpected spikes',
  ],
  'storage.low_compression_ratio': [
    'Investigate column cardinality causing poor compression',
    'Re-compress affected chunks with updated settings',
    'Verify TimescaleDB compression config',
    'Review schema for high-cardinality columns',
  ],
  'storage.high_connections': [
    'Tune pgbouncer pool size and mode',
    'Check for connection leaks in application code',
    'Investigate slow queries holding connections',
    'Review connection limit settings',
  ],
  anomaly_batch_crash: [
    'Check anomaly detection worker logs for panic stack',
    'Verify NATS consumer state and redelivery counts',
    'Restart the anomaly batch processor',
    'Review recent schema or config changes that may have caused crash',
  ],
  'roaming.agreement.renewal_due': [
    'Contact partner operator to initiate renewal discussions',
    'Review current agreement terms and pricing',
    'Update agreement record before expiry to avoid service disruption',
    'Escalate to commercial team if negotiation is required',
  ],
  sla_violation: [
    'Check operator performance metrics for the affected period',
    'Escalate to partner operator NOC',
    'Review recent topology or routing changes',
    'Document violation for SLA credit claim if applicable',
  ],
}

const ALERT_TYPE_OPTIONS = [
  { value: '', label: 'All Types' },
  { value: 'operator_down', label: 'Operator Down' },
  { value: 'auth_spike', label: 'Auth Spike' },
  { value: 'auth_flood', label: 'Auth Flood' },
  { value: 'ip_pool_exhaustion', label: 'IP Pool Full' },
  { value: 'sim_cloning', label: 'SIM Cloning' },
  { value: 'data_spike', label: 'Data Spike' },
  { value: 'usage_spike', label: 'Usage Spike' },
  { value: 'policy_violation', label: 'Policy Violation' },
  { value: 'velocity_anomaly', label: 'Velocity Anomaly' },
  { value: 'location_anomaly', label: 'Location Anomaly' },
  { value: 'credential_stuffing', label: 'Credential Stuffing' },
]

const severityFilterPills = SEVERITY_FILTER_OPTIONS.map((o) => ({
  value: o.value,
  label: o.value === '' ? 'All' : o.label,
})) as readonly { value: string; label: string }[]

function alertTypeIcon(type: string) {
  switch (type) {
    case 'operator_down': return <WifiOff className="h-4 w-4" />
    case 'auth_spike':
    case 'auth_flood': return <Zap className="h-4 w-4" />
    case 'ip_pool_exhaustion': return <Database className="h-4 w-4" />
    case 'sim_cloning': return <Lock className="h-4 w-4" />
    case 'data_spike':
    case 'usage_spike': return <TrendingUp className="h-4 w-4" />
    case 'policy_violation': return <Shield className="h-4 w-4" />
    case 'velocity_anomaly': return <Activity className="h-4 w-4" />
    case 'location_anomaly': return <Radio className="h-4 w-4" />
    case 'credential_stuffing': return <Lock className="h-4 w-4" />
    default: return <Wifi className="h-4 w-4" />
  }
}

function stateBadgeVariant(state: string): 'default' | 'success' | 'secondary' | 'outline' {
  switch (state) {
    case 'open': return 'default'
    case 'acknowledged': return 'secondary'
    case 'resolved': return 'success'
    case 'suppressed': return 'outline'
    default: return 'default'
  }
}

function alertDisplayTitle(alert: Alert): string {
  if (alert.title) return alert.title
  const typeLabels: Record<string, string> = {
    operator_down: 'Operator connectivity failure detected',
    auth_spike: 'Abnormal authentication rate spike',
    auth_flood: 'Authentication flood detected',
    ip_pool_exhaustion: 'IP address pool nearing exhaustion',
    sim_cloning: 'Potential SIM cloning activity detected',
    data_spike: 'Unusual data consumption spike',
    usage_spike: 'Abnormal usage pattern detected',
    policy_violation: 'Policy rule violation detected',
    velocity_anomaly: 'Impossible velocity movement detected',
    location_anomaly: 'Suspicious location change detected',
    credential_stuffing: 'Credential stuffing attack detected',
  }
  return typeLabels[alert.type] || `${alert.type.replace(/_/g, ' ')} alert`
}

function impactEstimate(alert: Alert): { sims: number; sessions: number } | null {
  if (alert.severity === 'info' || alert.severity === 'low') return null
  if (alert.source === 'sim' || alert.source === 'operator') {
    if (alert.severity === 'critical') return { sims: 45000, sessions: 12000 }
    if (alert.severity === 'high') return { sims: 2500, sessions: 800 }
    if (alert.severity === 'medium') return { sims: 800, sessions: 250 }
  }
  return null
}

function useAlerts(filters: AlertFilters) {
  return useInfiniteQuery({
    queryKey: ['alerts', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '30')
      if (filters.severity) params.set('severity', filters.severity)
      if (filters.state) params.set('state', filters.state)
      if (filters.type) params.set('type', filters.type)
      if (filters.source) params.set('source', filters.source)
      if (filters.q) params.set('q', filters.q)
      const res = await api.get<ListResponse<Alert>>(`/alerts?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.meta?.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
}

function useRealtimeAlertUpdates() {
  const qc = useQueryClient()

  useEffect(() => {
    const unsub = wsClient.on('alert.new', () => {
      qc.invalidateQueries({ queryKey: ['alerts'] })
    })
    return unsub
  }, [qc])
}

function StatCard({
  label,
  count,
  icon,
  colorClass,
  bgClass,
  pulse,
  delay,
}: {
  label: string
  count: number
  icon: React.ReactNode
  colorClass: string
  bgClass: string
  pulse?: boolean
  delay: number
}) {
  return (
    <div
      className={cn(
        'stagger-item relative overflow-hidden rounded-[var(--radius-md)] border p-4',
        bgClass,
      )}
      style={{ animationDelay: `${delay}ms` }}
    >
      {pulse && count > 0 && (
        <div className="absolute inset-0 animate-pulse opacity-20 bg-current" style={{ color: 'var(--color-danger)' }} />
      )}
      <div className="relative flex items-center justify-between">
        <div>
          <p className={cn('text-[11px] uppercase tracking-wider font-medium', colorClass)}>
            {label}
          </p>
          <p className={cn('text-2xl font-bold font-mono mt-1', colorClass)}>
            {count}
          </p>
        </div>
        <div className={cn('h-10 w-10 rounded-[var(--radius-sm)] flex items-center justify-center', colorClass, 'opacity-40')}>
          {icon}
        </div>
      </div>
    </div>
  )
}

function PillFilter<T extends string>({
  options,
  value,
  onChange,
  colorMap,
}: {
  options: readonly { value: T; label: string }[]
  value: T
  onChange: (v: T) => void
  colorMap?: Record<string, string>
}) {
  return (
    <div className="flex items-center gap-1 rounded-[var(--radius-sm)] bg-bg-elevated p-1 border border-border">
      {options.map((opt) => (
        <Button
          key={opt.value}
          variant="ghost"
          size="sm"
          onClick={() => onChange(opt.value)}
          className={cn(
            'px-3 py-1.5 h-auto text-xs font-medium rounded-[var(--radius-sm)] transition-all duration-200',
            value === opt.value
              ? cn(
                  'bg-bg-active text-text-primary shadow-sm hover:bg-bg-active',
                  colorMap?.[opt.value],
                )
              : 'text-text-tertiary hover:text-text-secondary hover:bg-bg-hover',
          )}
        >
          {opt.label}
        </Button>
      ))}
    </div>
  )
}

function AlertCardExpanded({ alert }: { alert: Alert }) {
  const navigate = useNavigate()
  const impact = impactEstimate(alert)
  const runbook = RUNBOOKS[alert.type]

  return (
    <div className="animate-slide-up-in border-t border-border bg-bg-primary/50 px-5 py-4 space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-1">Type</span>
          <div className="flex items-center gap-1.5">
            <span className="text-text-secondary">{alertTypeIcon(alert.type)}</span>
            <span className="text-sm text-text-primary font-medium">{alert.type.replace(/_/g, ' ')}</span>
          </div>
        </div>
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-1">Source</span>
          <span className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-text-secondary">
            {alert.source}
          </span>
        </div>
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-1">Fired</span>
          <span className="text-sm text-text-primary font-mono">
            {new Date(alert.fired_at).toLocaleString()}
          </span>
        </div>
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-1">
            {alert.state === 'acknowledged' ? 'Acknowledged' : alert.state === 'resolved' ? 'Resolved' : 'Status'}
          </span>
          <span className="text-sm text-text-primary font-mono">
            {alert.acknowledged_at
              ? new Date(alert.acknowledged_at).toLocaleString()
              : alert.resolved_at
                ? new Date(alert.resolved_at).toLocaleString()
                : '\u2014'}
          </span>
        </div>
      </div>

      {impact && (impact.sims > 0 || impact.sessions > 0) && (
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-2">Impact Assessment</span>
          <div className="flex items-center gap-6">
            {impact.sims > 0 && (
              <div className="flex items-center gap-2 rounded-[var(--radius-sm)] bg-danger-dim border border-danger/20 px-3 py-2">
                <Radio className="h-3.5 w-3.5 text-danger" />
                <span className="text-xs text-text-primary">
                  <span className="font-mono font-bold text-danger">{formatNumber(impact.sims)}</span> affected SIMs
                </span>
              </div>
            )}
            {impact.sessions > 0 && (
              <div className="flex items-center gap-2 rounded-[var(--radius-sm)] bg-warning-dim border border-warning/20 px-3 py-2">
                <Activity className="h-3.5 w-3.5 text-warning" />
                <span className="text-xs text-text-primary">
                  <span className="font-mono font-bold text-warning">{formatNumber(impact.sessions)}</span> affected sessions
                </span>
              </div>
            )}
          </div>
        </div>
      )}

      {runbook && (
        <div>
          <div className="flex items-center gap-1.5 mb-2">
            <BookOpen className="h-3.5 w-3.5 text-accent" />
            <span className="text-[10px] uppercase tracking-wider text-accent font-medium">Runbook</span>
          </div>
          <div className="rounded-[var(--radius-sm)] border border-accent/20 bg-accent-dim p-3">
            <ol className="space-y-1.5">
              {runbook.map((step, i) => (
                <li key={i} className="flex items-start gap-2 text-xs text-text-primary">
                  <span className="flex-shrink-0 w-5 h-5 rounded-full bg-accent/20 text-accent flex items-center justify-center text-[10px] font-bold font-mono mt-0.5">
                    {i + 1}
                  </span>
                  {step}
                </li>
              ))}
            </ol>
          </div>
        </div>
      )}

      {alert.sim_id && (
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-2">Related Entity</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate(`/sims/${alert.sim_id}`)}
            className="inline-flex items-center gap-1.5 text-xs text-accent hover:underline h-auto p-0"
          >
            <ExternalLink className="h-3 w-3" />
            View SIM {alert.sim_id.slice(0, 12)}
          </Button>
        </div>
      )}

      {alert.meta && Object.keys(alert.meta).length > 0 && (
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-2">Raw Meta</span>
          <pre className="text-xs font-mono text-text-secondary bg-bg-primary rounded-[var(--radius-sm)] p-3 overflow-x-auto max-h-[160px] border border-border">
            {JSON.stringify(alert.meta, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

function statePill(state: string) {
  switch (state) {
    case 'open':
      return <Badge className="bg-danger-dim text-danger border-0 text-[10px] flex-shrink-0">open</Badge>
    case 'acknowledged':
      return <Badge className="bg-warning-dim text-warning border-0 text-[10px] flex-shrink-0">ack</Badge>
    case 'resolved':
      return <Badge className="bg-success-dim text-success border-0 text-[10px] flex-shrink-0">resolved</Badge>
    case 'suppressed':
      return <Badge className="bg-bg-elevated text-text-tertiary border border-border text-[10px] flex-shrink-0">suppressed</Badge>
    default:
      return null
  }
}

function AlertCard({
  alert,
  isExpanded,
  onToggle,
  onCommentOpen,
  delay,
}: {
  alert: Alert
  isExpanded: boolean
  onToggle: () => void
  onCommentOpen: () => void
  delay: number
}) {
  const navigate = useNavigate()

  return (
    <div
      className={cn(
        'stagger-item card-hover rounded-[var(--radius-md)] border bg-bg-surface overflow-hidden',
        alert.severity === 'critical' && alert.state === 'open' && 'border-danger/40',
        (alert.severity === 'high' || alert.severity === 'medium') && alert.state === 'open' && 'border-warning/30',
        alert.state === 'resolved' && 'opacity-70',
        isExpanded && 'border-accent/40',
      )}
      style={{ animationDelay: `${delay}ms` }}
    >
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-3 cursor-pointer transition-colors duration-150',
          'hover:bg-bg-hover/50',
        )}
        onClick={onToggle}
      >
        <SeverityBadge severity={alert.severity} iconOnly className="flex-shrink-0" />
        <SeverityBadge severity={alert.severity} className="flex-shrink-0" />

        {statePill(alert.state)}

        <span className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-text-secondary flex-shrink-0">
          {alert.source}
        </span>

        {(alert.occurrence_count ?? 0) > 1 && (
          <Badge
            variant="outline"
            className="ml-1 h-5 gap-1 border-border bg-bg-elevated text-[10px] text-text-secondary flex-shrink-0"
            aria-label={`occurred ${alert.occurrence_count} times`}
          >
            <Repeat className="h-3 w-3" />
            {formatOccurrence(alert.occurrence_count, alert.first_seen_at, alert.last_seen_at)}
          </Badge>
        )}

        <div className="flex-1 min-w-0">
          <p className={cn(
            'text-sm font-medium truncate',
            alert.state === 'resolved' ? 'text-text-secondary' : 'text-text-primary',
          )}>
            {alertDisplayTitle(alert)}
          </p>
        </div>

        {alert.sim_id && (
          <Button
            variant="ghost"
            size="sm"
            onClick={(e) => {
              e.stopPropagation()
              navigate(`/sims/${alert.sim_id}`)
            }}
            className="hidden sm:flex items-center gap-1 text-[11px] font-mono text-accent hover:underline flex-shrink-0 h-auto p-0"
          >
            <ExternalLink className="h-3 w-3" />
            {alert.sim_id.slice(0, 8)}
          </Button>
        )}

        <div className="hidden md:flex items-center gap-1 text-[11px] text-text-tertiary font-mono flex-shrink-0">
          <Clock className="h-3 w-3" />
          {timeAgo(alert.fired_at)}
        </div>

        <div className="flex items-center gap-1.5 flex-shrink-0" onClick={(e) => e.stopPropagation()}>
          <AlertActionButtons anomaly={alert} />
          <Button
            variant="ghost"
            size="sm"
            onClick={onCommentOpen}
            className="h-7 w-7 p-0 text-text-tertiary hover:text-accent"
            aria-label="Investigation thread"
            title="Investigation thread"
          >
            <MessageSquare className="h-3.5 w-3.5" />
          </Button>
          <RowActionsMenu
            actions={[
              { label: 'View Details', onClick: () => navigate(`/alerts/${alert.id}`) },
              ...(alert.sim_id ? [{ label: 'View SIM', onClick: () => navigate(`/sims/${alert.sim_id}`) }] : []),
            ]}
          />
        </div>

        <div className="flex-shrink-0 text-text-tertiary">
          {isExpanded
            ? <ChevronUp className="h-4 w-4" />
            : <ChevronDown className="h-4 w-4" />}
        </div>
      </div>

      {isExpanded && <AlertCardExpanded alert={alert} />}
    </div>
  )
}

function AlertsSkeleton() {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Skeleton className="h-4 w-48" />
      </div>
      <Skeleton className="h-8 w-64" />
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
      <div className="flex gap-3">
        <Skeleton className="h-9 w-64" />
        <Skeleton className="h-9 w-48" />
      </div>
      <div className="space-y-3">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-14 w-full" />
        ))}
      </div>
    </div>
  )
}

function EmptyState({ hasFilters }: { hasFilters: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center py-20 gap-4">
      <div className="h-16 w-16 rounded-xl bg-success-dim border border-success/20 flex items-center justify-center">
        <Shield className="h-8 w-8 text-success" />
      </div>
      <div className="text-center">
        <p className="text-sm font-medium text-text-primary">
          {hasFilters ? 'No alerts match current filters' : 'All clear'}
        </p>
        <p className="text-xs text-text-secondary mt-1">
          {hasFilters
            ? 'Try adjusting your filter criteria'
            : 'No active alerts at the moment. System is operating normally.'}
        </p>
      </div>
    </div>
  )
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-20 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load alerts</h2>
        <p className="text-sm text-text-secondary mb-4">Unable to fetch alert data. Please try again.</p>
        <Button onClick={onRetry} variant="outline" className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    </div>
  )
}

export default function AlertsPage() {
  const [filters, setFilters] = useState<AlertFilters>({
    severity: '',
    state: '',
    type: '',
    source: '',
    q: '',
  })
  const [searchInput, setSearchInput] = useState('')
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const [commentAlert, setCommentAlert] = useState<Alert | null>(null)
  const [muted, setMuted] = useState(false)
  const searchTimeoutRef = useRef<ReturnType<typeof setTimeout>>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const {
    data, isLoading, isError, refetch, hasNextPage, fetchNextPage, isFetchingNextPage,
  } = useAlerts(filters)

  useRealtimeAlertUpdates()
  const { exportCSV, exporting } = useExport('analytics/anomalies')

  const alerts = useMemo(
    () => data?.pages.flatMap((p) => p.data) ?? [],
    [data],
  )

  const counts = useMemo(() => {
    const open = alerts.filter((a) => a.state === 'open')
    return {
      critical: open.filter((a) => a.severity === 'critical').length,
      high: open.filter((a) => a.severity === 'high').length,
      medium: open.filter((a) => a.severity === 'medium').length,
      low: open.filter((a) => a.severity === 'low').length,
      info: open.filter((a) => a.severity === 'info').length,
      acknowledged: alerts.filter((a) => a.state === 'acknowledged').length,
      resolved: alerts.filter((a) => a.state === 'resolved').length,
    }
  }, [alerts])

  const handleSearch = useCallback((value: string) => {
    setSearchInput(value)
    if (searchTimeoutRef.current) clearTimeout(searchTimeoutRef.current)
    searchTimeoutRef.current = setTimeout(() => {
      setFilters((prev) => ({ ...prev, q: value }))
    }, 300)
  }, [])

  const toggleExpanded = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el || !hasNextPage) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  useEffect(() => {
    return () => {
      if (searchTimeoutRef.current) clearTimeout(searchTimeoutRef.current)
    }
  }, [])

  if (isLoading) return <AlertsSkeleton />
  if (isError) return <ErrorState onRetry={() => refetch()} />

  const hasFilters = !!(filters.severity || filters.state || filters.type || filters.source || filters.q)

  return (
    <div className="space-y-6">
      <div className="stagger-item" style={{ animationDelay: '0ms' }}>
        <Breadcrumb
          items={[
            { label: 'Dashboard', href: '/' },
            { label: 'Alerts' },
          ]}
          className="mb-3"
        />

        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-semibold text-text-primary">Alerts & Incidents</h1>
            {counts.critical > 0 && (
              <Badge variant="danger" className="text-[10px] pulse-dot">
                {counts.critical} critical
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant={muted ? 'destructive' : 'ghost'}
              size="sm"
              className="gap-1.5 text-xs"
              onClick={() => setMuted(!muted)}
            >
              <BellOff className="h-3.5 w-3.5" />
              {muted ? 'Muted' : 'Mute All'}
            </Button>
            <Button variant="outline" size="sm" onClick={() => exportCSV()} disabled={exporting} className="gap-1.5 text-xs">
              {exporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              Export
            </Button>
            <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1.5 text-xs">
              <RefreshCw className="h-3.5 w-3.5" />
              Refresh
            </Button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard
          label="Critical"
          count={counts.critical}
          icon={<AlertCircle className="h-6 w-6" />}
          colorClass="text-danger"
          bgClass="border-danger/30 bg-danger-dim"
          pulse
          delay={50}
        />
        <StatCard
          label="High / Medium"
          count={counts.high + counts.medium}
          icon={<AlertTriangle className="h-6 w-6" />}
          colorClass="text-warning"
          bgClass="border-warning/30 bg-warning-dim"
          delay={100}
        />
        <StatCard
          label="Acknowledged"
          count={counts.acknowledged}
          icon={<Eye className="h-6 w-6" />}
          colorClass="text-info"
          bgClass="border-info/30 bg-info-dim"
          delay={150}
        />
        <StatCard
          label="Resolved (24h)"
          count={counts.resolved}
          icon={<CheckCircle className="h-6 w-6" />}
          colorClass="text-success"
          bgClass="border-success/30 bg-success-dim"
          delay={200}
        />
      </div>

      <div
        className="stagger-item flex flex-col lg:flex-row lg:items-center gap-3"
        style={{ animationDelay: '250ms' }}
      >
        <div className="flex flex-wrap items-center gap-3">
          <PillFilter
            options={severityFilterPills}
            value={filters.severity}
            onChange={(v) => setFilters((prev) => ({ ...prev, severity: v }))}
            colorMap={SEVERITY_PILL_CLASSES}
          />
          <PillFilter
            options={ALERT_STATE_OPTIONS}
            value={filters.state}
            onChange={(v) => setFilters((prev) => ({ ...prev, state: v }))}
            colorMap={{
              open: 'bg-danger-dim text-danger',
              acknowledged: 'bg-info-dim text-info',
              resolved: 'bg-success-dim text-success',
              suppressed: 'bg-bg-elevated text-text-tertiary',
            }}
          />
        </div>
        <div className="flex items-center gap-3 lg:ml-auto">
          <Select
            options={ALERT_SOURCE_OPTIONS}
            value={filters.source}
            onChange={(e) => setFilters((prev) => ({ ...prev, source: e.target.value }))}
            className="w-40"
          />
          <Select
            options={ALERT_TYPE_OPTIONS}
            value={filters.type}
            onChange={(e) => setFilters((prev) => ({ ...prev, type: e.target.value }))}
            className="w-44"
          />
          <div className="relative flex-1 min-w-[200px]">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-text-tertiary pointer-events-none" />
            <Input
              value={searchInput}
              onChange={(e) => handleSearch(e.target.value)}
              placeholder="Search alerts..."
              className="pl-8"
            />
          </div>
        </div>
      </div>

      {alerts.length === 0 ? (
        <EmptyState hasFilters={hasFilters} />
      ) : (
        <div className="space-y-2">
          {alerts.map((alert, idx) => (
            <div key={alert.id} data-row-index={idx} data-href={`/alerts/${alert.id}`}>
              <AlertCard
                alert={alert}
                isExpanded={expandedIds.has(alert.id)}
                onToggle={() => toggleExpanded(alert.id)}
                onCommentOpen={() => setCommentAlert(alert)}
                delay={300 + Math.min(idx, 10) * 40}
              />
            </div>
          ))}
        </div>
      )}

      <div ref={loadMoreRef} className="h-1" />

      {isFetchingNextPage && (
        <div className="flex justify-center py-4">
          <Spinner className="h-5 w-5 text-accent" />
        </div>
      )}

      {!hasNextPage && alerts.length > 0 && (
        <div className="text-center py-4">
          <p className="text-xs text-text-tertiary">
            Showing all {alerts.length} alert{alerts.length !== 1 ? 's' : ''}
          </p>
        </div>
      )}

      {commentAlert && (
        <CommentThread
          alert={commentAlert}
          open={!!commentAlert}
          onClose={() => setCommentAlert(null)}
        />
      )}
    </div>
  )
}
