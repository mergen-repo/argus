import { useState, useRef, useEffect, useMemo, useCallback } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell,
} from 'recharts'
import {
  Shield, AlertCircle, Search, RefreshCw,
  Clock,
  Activity, Ban, Tag, Bell, FileText, MoreHorizontal, CheckCircle2, BookOpen, ArrowUpRight,
  Download, Loader2, XCircle,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { TimeframeSelector, type TimeframeValue } from '@/components/ui/timeframe-selector'
import { EmptyState } from '@/components/shared/empty-state'
import { OperatorChip } from '@/components/shared/operator-chip'
import { EntityLink } from '@/components/shared/entity-link'
import { useExport } from '@/hooks/use-export'
import { cn } from '@/lib/utils'
import { timeAgo } from '@/lib/format'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { SEVERITY_FILTER_OPTIONS } from '@/lib/severity'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import type { OperatorCode } from '@/lib/operator-chip'
import {
  type PolicyViolation,
  deriveStatus,
  VIOLATION_TYPE_FILTER_OPTIONS,
  ACTION_TAKEN_FILTER_OPTIONS,
  STATUS_FILTER_OPTIONS,
} from '@/types/violation'
import {
  useViolations,
  useViolationCounts,
  useAcknowledgeViolation,
  useRemediate,
  useBulkAcknowledge,
  useBulkDismiss,
  type ViolationFilters,
  type RemediateAction,
} from '@/hooks/use-violations'
import { StatusBadge } from '@/components/violations/status-badge'
import { AcknowledgeDialog } from '@/components/violations/acknowledge-dialog'
import { RemediateDialog } from '@/components/violations/remediate-dialog'
import { BulkActionBar } from '@/components/violations/bulk-action-bar'
import { Checkbox } from '@/components/ui/checkbox'

const TYPE_COLORS: Record<string, string> = {
  block: 'var(--color-danger)', disconnect: 'var(--color-danger)', suspend: 'var(--color-danger)',
  throttle: 'var(--color-warning)', policy_notify: 'var(--color-info)', policy_log: 'var(--color-accent)',
  policy_tag: 'var(--color-purple)',
}

const SEVERITY_CHART_FILLS: Record<string, string> = {
  critical: 'var(--color-danger)',
  high: 'var(--color-danger)',
  medium: 'var(--color-warning)',
  low: 'var(--color-info)',
  info: 'var(--color-accent)',
}

function typeIcon(type: string) {
  switch (type) {
    case 'bandwidth_exceeded': case 'session_limit': return <Activity className="h-3.5 w-3.5" />
    case 'quota_exceeded': return <Ban className="h-3.5 w-3.5" />
    case 'time_restriction': return <Clock className="h-3.5 w-3.5" />
    case 'geo_blocked': return <Shield className="h-3.5 w-3.5" />
    case 'block': case 'disconnect': case 'suspend': return <Ban className="h-3.5 w-3.5" />
    case 'throttle': return <Activity className="h-3.5 w-3.5" />
    case 'policy_notify': return <Bell className="h-3.5 w-3.5" />
    case 'policy_log': return <FileText className="h-3.5 w-3.5" />
    case 'policy_tag': return <Tag className="h-3.5 w-3.5" />
    default: return <Shield className="h-3.5 w-3.5" />
  }
}

const tooltipStyle = {
  backgroundColor: 'var(--color-bg-elevated)',
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--color-text-primary)',
  fontSize: '12px',
}

function detailLabel(key: string): string {
  const map: Record<string, string> = {
    reason: 'Reason', threshold_bytes: 'Threshold', current_bytes: 'Current Usage',
    rate: 'Throttle Rate', original_rate: 'Original Rate', operator: 'Operator',
    anomaly_type: 'Anomaly Type', channel: 'Channel', recipient: 'Recipient',
    message: 'Message', daily_bytes: 'Daily Usage', key: 'Tag Key', value: 'Tag Value',
    previous_value: 'Previous Value', max_sessions: 'Max Sessions', current_sessions: 'Current Sessions',
    rat_type: 'RAT Type', remediation: 'Remediation',
  }
  return map[key] || key.replace(/_/g, ' ')
}

function formatBytes(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)} GB`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)} MB`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)} KB`
  return `${n} B`
}

function formatDetailValue(key: string, val: unknown): string {
  if (typeof val === 'number') {
    if (key.includes('bytes') || key.includes('rate')) return formatBytes(val)
    return val.toLocaleString()
  }
  return String(val)
}

// Reads measured / threshold pairs out of `details` for inline row display
// (FIX-244 AC-8). Returns nulls if either side is missing.
function extractUsageInline(details?: Record<string, unknown>): { current: number; threshold: number } | null {
  if (!details) return null
  const cur = details.current_bytes
  const thr = details.threshold_bytes
  if (typeof cur === 'number' && typeof thr === 'number') return { current: cur, threshold: thr }
  return null
}

function humanizeDateRange(filters: ViolationFilters): string {
  if (filters.date_from && filters.date_to) {
    const from = new Date(filters.date_from)
    const to = new Date(filters.date_to)
    return `between ${from.toLocaleDateString()} and ${to.toLocaleDateString()}`
  }
  if (filters.date_from) return `since ${new Date(filters.date_from).toLocaleDateString()}`
  if (filters.date_to) return `up to ${new Date(filters.date_to).toLocaleDateString()}`
  return 'in the selected timeframe'
}

// Map TimeframeSelector value → backend `date_from` / `date_to` ISO strings.
// Presets resolve relative to NOW; 'custom' keeps the explicit from/to as-is.
function timeframeToRange(tv: TimeframeValue): { from?: string; to?: string } {
  if (tv.value === 'custom') return { from: tv.from, to: tv.to }
  const now = Date.now()
  const offsets: Record<string, number> = {
    '15m': 15 * 60 * 1000,
    '1h': 60 * 60 * 1000,
    '6h': 6 * 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
  }
  const ms = offsets[tv.value]
  if (!ms) return {}
  return { from: new Date(now - ms).toISOString(), to: new Date(now).toISOString() }
}

export default function ViolationsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const filters = useMemo<ViolationFilters>(() => ({
    violation_type: searchParams.get('violation_type') ?? '',
    action_taken: searchParams.get('action_taken') ?? '',
    severity: searchParams.get('severity') ?? '',
    status: searchParams.get('status') ?? '',
    date_from: searchParams.get('date_from') ?? '',
    date_to: searchParams.get('date_to') ?? '',
  }), [searchParams])

  const setFilter = useCallback((key: keyof ViolationFilters, value: string) => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      if (value) p.set(key, value); else p.delete(key)
      return p
    }, { replace: false })
  }, [setSearchParams])

  const clearFilters = useCallback(() => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      ;['violation_type', 'action_taken', 'severity', 'status', 'date_from', 'date_to'].forEach((k) => p.delete(k))
      return p
    }, { replace: false })
  }, [setSearchParams])

  const hasAnyFilter = !!(filters.violation_type || filters.action_taken || filters.severity ||
    filters.status || filters.date_from || filters.date_to)

  const timeframeValue = useMemo<TimeframeValue>(() => {
    if (filters.date_from || filters.date_to) {
      return { value: 'custom', from: filters.date_from, to: filters.date_to }
    }
    return { value: '24h' }
  }, [filters.date_from, filters.date_to])

  const handleTimeframeChange = useCallback((tv: TimeframeValue) => {
    const { from, to } = timeframeToRange(tv)
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      if (from) p.set('date_from', from); else p.delete('date_from')
      if (to) p.set('date_to', to); else p.delete('date_to')
      return p
    }, { replace: false })
  }, [setSearchParams])

  const [searchInput, setSearchInput] = useState('')
  const [selectedViolation, setSelectedViolation] = useState<PolicyViolation | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const loadMoreRef = useRef<HTMLDivElement>(null)

  type DialogState =
    | { kind: 'ack-single'; violation: PolicyViolation }
    | { kind: 'remediate-single'; violation: PolicyViolation; action: RemediateAction }
    | { kind: 'ack-bulk' }
    | { kind: 'remediate-bulk'; action: 'dismiss' }
    | null
  const [dialogState, setDialogState] = useState<DialogState>(null)
  const closeDialog = useCallback(() => setDialogState(null), [])

  const { data, isLoading, isError, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } = useViolations(filters)
  const { data: counts } = useViolationCounts()
  const acknowledgeMutation = useAcknowledgeViolation()
  const remediateMutation = useRemediate()
  const bulkAckMutation = useBulkAcknowledge()
  const bulkDismissMutation = useBulkDismiss()
  const { exportCSV, exporting } = useExport('policy-violations')

  const isAnyMutating =
    acknowledgeMutation.isPending ||
    remediateMutation.isPending ||
    bulkAckMutation.isPending ||
    bulkDismissMutation.isPending

  const toggleRowSelection = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })
  }, [])

  const clearSelection = useCallback(() => setSelectedIds(new Set()), [])

  const handleAcknowledgeConfirm = useCallback(async (note: string) => {
    if (!dialogState) return
    try {
      if (dialogState.kind === 'ack-single') {
        await acknowledgeMutation.mutateAsync({ id: dialogState.violation.id, note: note || undefined })
        toast.success('Violation acknowledged')
      } else if (dialogState.kind === 'ack-bulk') {
        const ids = [...selectedIds]
        const result = await bulkAckMutation.mutateAsync({ ids, note: note || undefined })
        const okN = result?.succeeded?.length ?? 0
        const failN = result?.failed?.length ?? 0
        if (failN === 0) toast.success(`${okN} violation${okN === 1 ? '' : 's'} acknowledged`)
        else toast.warning(`${okN} acknowledged · ${failN} failed`)
        clearSelection()
      }
      closeDialog()
    } catch (err) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })
        ?.response?.data?.error?.message
      toast.error(msg ?? 'Acknowledge failed')
    }
  }, [dialogState, acknowledgeMutation, bulkAckMutation, selectedIds, clearSelection, closeDialog])

  const handleRemediateConfirm = useCallback(async (reason: string) => {
    if (!dialogState) return
    try {
      if (dialogState.kind === 'remediate-single') {
        await remediateMutation.mutateAsync({
          violationId: dialogState.violation.id,
          action: dialogState.action,
          reason,
          simId: dialogState.violation.sim_id,
        })
        const labels: Record<RemediateAction, string> = {
          suspend_sim: 'SIM suspended',
          escalate: 'Violation escalated',
          dismiss: 'Violation dismissed',
        }
        toast.success(labels[dialogState.action])
      } else if (dialogState.kind === 'remediate-bulk' && dialogState.action === 'dismiss') {
        const ids = [...selectedIds]
        const result = await bulkDismissMutation.mutateAsync({ ids, reason })
        const okN = result?.succeeded?.length ?? 0
        const failN = result?.failed?.length ?? 0
        if (failN === 0) toast.success(`${okN} violation${okN === 1 ? '' : 's'} dismissed`)
        else toast.warning(`${okN} dismissed · ${failN} failed`)
        clearSelection()
      }
      closeDialog()
    } catch (err) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })
        ?.response?.data?.error?.message
      toast.error(msg ?? 'Action failed')
    }
  }, [dialogState, remediateMutation, bulkDismissMutation, selectedIds, clearSelection, closeDialog])

  const violations = useMemo(() => data?.pages.flatMap((p) => p.data ?? []) ?? [], [data])

  const filtered = useMemo(() => {
    if (!searchInput) return violations
    const q = searchInput.toLowerCase()
    return violations.filter((v) =>
      v.violation_type.includes(q) || v.action_taken.includes(q) ||
      v.iccid?.toLowerCase().includes(q) || v.sim_iccid?.toLowerCase().includes(q) ||
      v.policy_name?.toLowerCase().includes(q) ||
      v.operator_name?.toLowerCase().includes(q) || v.severity.includes(q),
    )
  }, [violations, searchInput])

  const totalCount = useMemo(() => counts ? Object.values(counts).reduce((a, b) => a + b, 0) : 0, [counts])

  const typeChartData = useMemo(() => {
    if (!counts) return []
    return Object.entries(counts).map(([type, count]) => ({
      name: type.replace('policy_', ''),
      value: count,
      fill: TYPE_COLORS[type] || 'var(--color-text-tertiary)',
    })).sort((a, b) => b.value - a.value)
  }, [counts])

  const sevChartData = useMemo(() => {
    const agg: Record<string, number> = {}
    violations.forEach((v) => { agg[v.severity] = (agg[v.severity] || 0) + 1 })
    return Object.entries(agg).map(([sev, count]) => ({
      name: sev, value: count, fill: SEVERITY_CHART_FILLS[sev] || 'var(--color-text-tertiary)',
    }))
  }, [violations])

  const topSims = useMemo(() => {
    const agg: Record<string, { count: number; iccid: string; simId: string }> = {}
    violations.forEach((v) => {
      if (!agg[v.sim_id]) agg[v.sim_id] = { count: 0, iccid: v.iccid || v.msisdn || v.sim_iccid || v.sim_id.slice(0, 12), simId: v.sim_id }
      agg[v.sim_id].count++
    })
    return Object.values(agg).sort((a, b) => b.count - a.count).slice(0, 5)
  }, [violations])

  const handleRowClick = useCallback((v: PolicyViolation) => {
    setSelectedViolation(v)
  }, [])

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el || !hasNextPage) return
    const observer = new IntersectionObserver(
      (entries) => { if (entries[0]?.isIntersecting && hasNextPage && !isFetchingNextPage) fetchNextPage() },
      { threshold: 0.1 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  if (isLoading) return (
    <div className="space-y-4">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-8 w-64" />
      <div className="grid grid-cols-3 gap-4"><Skeleton className="h-48" /><Skeleton className="h-48" /><Skeleton className="h-48" /></div>
      <div className="space-y-2">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-12" />)}</div>
    </div>
  )

  if (isError) return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load violations</h2>
        <Button onClick={() => refetch()} variant="outline" className="gap-2"><RefreshCw className="h-4 w-4" /> Retry</Button>
      </div>
    </div>
  )

  return (
    <div className="space-y-5">
      <div>
        <Breadcrumb items={[{ label: 'Dashboard', href: '/' }, { label: 'Policy Violations' }]} className="mb-2" />
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-[16px] font-semibold text-text-primary">Policy Violations</h1>
            {totalCount > 0 && <Badge variant="danger" className="text-[10px]">{totalCount} in 24h</Badge>}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" className="gap-1.5 text-xs" onClick={() => exportCSV(Object.fromEntries(searchParams))} disabled={exporting}>
              {exporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              Export
            </Button>
            <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1.5 text-xs"><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
          </div>
        </div>
      </div>

      {/* Charts Row */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* By Type — Bar Chart */}
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm">By Type (24h)</CardTitle></CardHeader>
          <CardContent>
            {typeChartData.length > 0 ? (
              <div className="h-[160px]">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={typeChartData} layout="vertical" margin={{ left: 0, right: 8, top: 0, bottom: 0 }}>
                    <XAxis type="number" hide />
                    <YAxis type="category" dataKey="name" width={70} tick={{ fill: 'var(--color-text-secondary)', fontSize: 11, fontFamily: 'var(--font-mono)' }} tickLine={false} axisLine={false} />
                    <Tooltip contentStyle={tooltipStyle} />
                    <Bar dataKey="value" radius={[0, 4, 4, 0]} barSize={14}>
                      {typeChartData.map((d, i) => <Cell key={i} fill={d.fill} />)}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-[160px] flex items-center justify-center text-xs text-text-tertiary">No data</div>
            )}
          </CardContent>
        </Card>

        {/* By Severity — Donut */}
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm">By Severity</CardTitle></CardHeader>
          <CardContent>
            {sevChartData.length > 0 ? (
              <div className="flex items-center gap-4">
                <div className="w-[120px] h-[120px]">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie data={sevChartData} dataKey="value" cx="50%" cy="50%" innerRadius={35} outerRadius={55} paddingAngle={3} strokeWidth={0}>
                        {sevChartData.map((d, i) => <Cell key={i} fill={d.fill} />)}
                      </Pie>
                      <Tooltip contentStyle={tooltipStyle} />
                    </PieChart>
                  </ResponsiveContainer>
                </div>
                <div className="flex flex-col gap-2">
                  {sevChartData.map((d) => (
                    <div key={d.name} className="flex items-center gap-2 text-xs">
                      <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: d.fill }} />
                      <span className="text-text-secondary capitalize">{d.name}</span>
                      <span className="font-mono text-text-primary ml-auto">{d.value}</span>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <div className="h-[120px] flex items-center justify-center text-xs text-text-tertiary">No data</div>
            )}
          </CardContent>
        </Card>

        {/* Top Offending SIMs */}
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm">Top SIMs</CardTitle></CardHeader>
          <CardContent>
            {topSims.length > 0 ? (
              <div className="space-y-2">
                {topSims.map((s, i) => (
                  <div key={s.simId} className="flex items-center gap-2">
                    <span className="w-5 text-[10px] font-mono text-text-tertiary text-right">#{i + 1}</span>
                    <div className="flex-1 min-w-0">
                      <Link to={`/sims/${s.simId}`} className="font-mono text-xs text-accent hover:underline truncate block">{s.iccid}</Link>
                    </div>
                    <div className="flex items-center gap-2">
                      <div className="w-16 h-1.5 bg-bg-hover rounded-full overflow-hidden">
                        <div className="h-full rounded-full bg-danger" style={{ width: `${(s.count / topSims[0].count) * 100}%` }} />
                      </div>
                      <span className="font-mono text-xs text-text-primary w-6 text-right">{s.count}</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="h-[120px] flex items-center justify-center text-xs text-text-tertiary">No data</div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-2 flex-wrap">
        <div className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
          <Input type="text" value={searchInput} onChange={(e) => setSearchInput(e.target.value)} placeholder="Filter by SIM, policy, type..."
            className="h-8 pl-8 pr-3 text-xs" />
        </div>
        <Select
          aria-label="Type"
          options={[...VIOLATION_TYPE_FILTER_OPTIONS]}
          value={filters.violation_type ?? ''}
          onChange={(e) => setFilter('violation_type', e.target.value)}
          className="w-40"
        />
        <Select
          aria-label="Action"
          options={[...ACTION_TAKEN_FILTER_OPTIONS]}
          value={filters.action_taken ?? ''}
          onChange={(e) => setFilter('action_taken', e.target.value)}
          className="w-36"
        />
        <Select
          aria-label="Severity"
          options={[...SEVERITY_FILTER_OPTIONS]}
          value={filters.severity ?? ''}
          onChange={(e) => setFilter('severity', e.target.value)}
          className="w-36"
        />
        <Select
          aria-label="Status"
          options={[...STATUS_FILTER_OPTIONS]}
          value={filters.status ?? ''}
          onChange={(e) => setFilter('status', e.target.value)}
          className="w-40"
        />
        <TimeframeSelector
          aria-label="Date range"
          value={timeframeValue}
          onChange={handleTimeframeChange}
        />
        {hasAnyFilter && (
          <Button variant="ghost" size="sm" onClick={clearFilters} className="text-[11px] text-text-tertiary hover:text-accent h-auto p-0">Clear</Button>
        )}
      </div>

      {/* Selection toolbar — shows select-all-on-page when rows exist */}
      {filtered.length > 0 && (
        <div className="flex items-center gap-2 text-[11px] text-text-tertiary">
          <Checkbox
            checked={filtered.length > 0 && filtered.every((v) => selectedIds.has(v.id))}
            onChange={(e) => {
              if ((e.target as HTMLInputElement).checked) {
                setSelectedIds(new Set([...selectedIds, ...filtered.map((v) => v.id)]))
              } else {
                const next = new Set(selectedIds)
                filtered.forEach((v) => next.delete(v.id))
                setSelectedIds(next)
              }
            }}
            aria-label="Select all violations on this page"
            title="Selection scoped to visible page — bulk-by-filter coming with FIX-236"
          />
          <span title="Selection scoped to visible page — bulk-by-filter coming with FIX-236">
            Select all on page · scoped to visible rows
          </span>
        </div>
      )}

      {/* Violations List */}
      {filtered.length === 0 ? (
        <EmptyState
          icon={Shield}
          title="No policy violations"
          description={
            hasAnyFilter
              ? 'Try adjusting your filters.'
              : `No policy violations ${humanizeDateRange(filters)}.`
          }
        />
      ) : (
        <div className="space-y-1.5">
          {filtered.map((v, idx) => {
            const status = deriveStatus(v)
            const usage = extractUsageInline(v.details)
            const iccidLabel = v.iccid || v.sim_iccid || v.sim_id.slice(0, 12)
            const isSelected = selectedIds.has(v.id)
            return (
              <div key={v.id} data-row-index={idx} data-href={`/violations/${v.id}`} className={cn(
                'rounded-[var(--radius-md)] border bg-bg-surface overflow-hidden transition-colors',
                v.severity === 'critical' && 'border-danger/30',
                (v.severity === 'high' || v.severity === 'medium') && 'border-warning/20',
                isSelected && 'border-accent/50 bg-bg-hover/30',
              )}>
                <div
                  role="button"
                  tabIndex={0}
                  aria-label={`Open details for ${v.violation_type} violation on ${iccidLabel}`}
                  className="flex items-center gap-3 px-4 py-2.5 cursor-pointer hover:bg-bg-hover/50 transition-colors"
                  onClick={() => handleRowClick(v)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      handleRowClick(v)
                    }
                  }}
                >
                  <span onClick={(e) => e.stopPropagation()} className="shrink-0">
                    <Checkbox
                      checked={isSelected}
                      onChange={() => toggleRowSelection(v.id)}
                      aria-label={`Select violation on ${iccidLabel}`}
                    />
                  </span>
                  <span className={v.severity === 'critical' || v.severity === 'high' ? 'text-danger' : v.severity === 'medium' ? 'text-warning' : 'text-text-tertiary'}>{typeIcon(v.violation_type)}</span>
                  <SeverityBadge severity={v.severity} className="shrink-0" />
                  <StatusBadge status={status} className="shrink-0" />
                  <span className="text-xs font-medium text-text-primary">{v.violation_type.replace(/_/g, ' ')}</span>
                  <span className="text-[10px] text-text-tertiary">→ {v.action_taken}</span>
                  <span className="hidden sm:inline-flex items-center" onClick={(e) => e.stopPropagation()}>
                    <EntityLink
                      entityType="sim"
                      entityId={v.sim_id}
                      label={iccidLabel}
                      className="text-[11px] font-mono"
                    />
                  </span>
                  {v.policy_id && (
                    <span className="hidden md:inline-flex items-center" onClick={(e) => e.stopPropagation()}>
                      <EntityLink
                        entityType="policy"
                        entityId={v.policy_id}
                        label={
                          v.policy_name
                            ? `${v.policy_name}${v.policy_version_number != null ? ` v${v.policy_version_number}` : ''}`
                            : undefined
                        }
                        className="text-[11px]"
                      />
                    </span>
                  )}
                  {usage && (
                    <span className="hidden xl:inline-flex items-center font-mono text-[10px] text-text-tertiary shrink-0">
                      {formatBytes(usage.current)} / {formatBytes(usage.threshold)}
                    </span>
                  )}
                  {(v.operator_name || v.operator_id) && (
                    <span className="hidden lg:block" onClick={(e) => e.stopPropagation()}>
                      <OperatorChip
                        name={v.operator_name ?? undefined}
                        code={v.operator_code as OperatorCode | undefined}
                        rawId={v.operator_id}
                        clickable
                        onClick={() => navigate(`/operators/${v.operator_id}`)}
                      />
                    </span>
                  )}
                  <span className="hidden md:flex items-center gap-1 text-[10px] text-text-tertiary font-mono shrink-0 ml-auto"><Clock className="h-3 w-3" />{timeAgo(v.created_at)}</span>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      className="h-6 w-6 p-0 shrink-0 text-text-tertiary hover:text-text-primary inline-flex items-center justify-center rounded transition-colors hover:bg-bg-hover"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <MoreHorizontal className="h-3.5 w-3.5" />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-52">
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        disabled={status !== 'open'}
                        onSelect={() => setDialogState({ kind: 'ack-single', violation: v })}
                      >
                        <CheckCircle2 className="h-3.5 w-3.5 text-success" />
                        Acknowledge
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => setDialogState({ kind: 'remediate-single', violation: v, action: 'suspend_sim' })}
                      >
                        <Ban className="h-3.5 w-3.5 text-danger" />
                        Suspend SIM
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => setDialogState({ kind: 'remediate-single', violation: v, action: 'escalate' })}
                      >
                        <ArrowUpRight className="h-3.5 w-3.5 text-warning" />
                        Escalate
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => setDialogState({ kind: 'remediate-single', violation: v, action: 'dismiss' })}
                      >
                        <XCircle className="h-3.5 w-3.5 text-text-tertiary" />
                        Dismiss (false positive)
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => navigate(`/policies/${v.policy_id}?rule=${v.rule_index}`)}
                      >
                        <BookOpen className="h-3.5 w-3.5 text-accent" />
                        Review Policy
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <SlidePanel
        open={!!selectedViolation}
        onOpenChange={(o) => !o && setSelectedViolation(null)}
        title={selectedViolation ? `Violation · ${selectedViolation.policy_name ?? selectedViolation.violation_type}` : ''}
        description={selectedViolation?.created_at ? new Date(selectedViolation.created_at).toLocaleString() : undefined}
        width="lg"
      >
        {selectedViolation && (
          <div className="space-y-4">
            <div className="flex items-center gap-2 flex-wrap">
              <SeverityBadge severity={selectedViolation.severity} />
              <StatusBadge status={deriveStatus(selectedViolation)} />
              <span className="text-[10px] uppercase tracking-wider text-text-tertiary">Type</span>
              <span className="text-xs text-text-primary capitalize">{selectedViolation.violation_type.replace(/_/g, ' ')}</span>
              <span className="text-[10px] uppercase tracking-wider text-text-tertiary ml-3">Action</span>
              <span className="text-xs text-text-primary capitalize">{selectedViolation.action_taken}</span>
            </div>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">SIM</span>
                <EntityLink
                  entityType="sim"
                  entityId={selectedViolation.sim_id}
                  label={selectedViolation.iccid || selectedViolation.sim_iccid || selectedViolation.sim_id.slice(0, 12)}
                />
              </div>
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Policy</span>
                <EntityLink
                  entityType="policy"
                  entityId={selectedViolation.policy_id}
                  label={
                    selectedViolation.policy_name
                      ? `${selectedViolation.policy_name}${selectedViolation.policy_version_number != null ? ` v${selectedViolation.policy_version_number}` : ''}`
                      : undefined
                  }
                />
              </div>
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Operator</span>
                {(selectedViolation.operator_name || selectedViolation.operator_id)
                  ? <OperatorChip
                      name={selectedViolation.operator_name ?? undefined}
                      code={selectedViolation.operator_code as OperatorCode | undefined}
                      rawId={selectedViolation.operator_id}
                      clickable
                      onClick={() => navigate(`/operators/${selectedViolation.operator_id}`)}
                    />
                  : <span className="text-xs text-text-tertiary">—</span>
                }
              </div>
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">APN</span>
                <span className="text-xs text-text-primary">{selectedViolation.apn_name ?? '—'}</span>
              </div>
              {selectedViolation.session_id && (
                <div>
                  <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Session</span>
                  <EntityLink
                    entityType="session"
                    entityId={selectedViolation.session_id}
                    truncate
                  />
                </div>
              )}
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Time</span>
                <span className="text-xs text-text-primary font-mono">{new Date(selectedViolation.created_at).toLocaleString()}</span>
              </div>
              {selectedViolation.acknowledged_at && (
                <div>
                  <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Acknowledged</span>
                  <span className="text-xs text-text-primary font-mono">{new Date(selectedViolation.acknowledged_at).toLocaleString()}</span>
                </div>
              )}
            </div>
            {selectedViolation.details && Object.keys(selectedViolation.details).length > 0 && (
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-2">Details</span>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                  {Object.entries(selectedViolation.details).map(([key, val]) => (
                    <div key={key} className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-primary px-3 py-2">
                      <div className="text-[10px] text-text-tertiary capitalize">{detailLabel(key)}</div>
                      <div className="text-xs text-text-primary font-mono mt-0.5">{formatDetailValue(key, val)}</div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
        <SlidePanelFooter>
          <Button variant="ghost" size="sm" onClick={() => setSelectedViolation(null)}>Close</Button>
          {selectedViolation && deriveStatus(selectedViolation) === 'open' && (
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => setDialogState({ kind: 'ack-single', violation: selectedViolation })}
            >
              <CheckCircle2 className="h-3.5 w-3.5 text-success" />
              Acknowledge
            </Button>
          )}
          {selectedViolation && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="default" className="gap-1.5">
                  Remediate
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuItem
                  className="text-xs gap-2"
                  onSelect={() => setDialogState({ kind: 'remediate-single', violation: selectedViolation, action: 'suspend_sim' })}
                >
                  <Ban className="h-3.5 w-3.5 text-danger" />
                  Suspend SIM
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-xs gap-2"
                  onSelect={() => setDialogState({ kind: 'remediate-single', violation: selectedViolation, action: 'escalate' })}
                >
                  <ArrowUpRight className="h-3.5 w-3.5 text-warning" />
                  Escalate
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-xs gap-2"
                  onSelect={() => setDialogState({ kind: 'remediate-single', violation: selectedViolation, action: 'dismiss' })}
                >
                  <XCircle className="h-3.5 w-3.5 text-text-tertiary" />
                  Dismiss
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </SlidePanelFooter>
      </SlidePanel>

      {/* Action Dialogs (single + bulk) */}
      <AcknowledgeDialog
        open={dialogState?.kind === 'ack-single' || dialogState?.kind === 'ack-bulk'}
        onOpenChange={(open) => { if (!open) closeDialog() }}
        mode={dialogState?.kind === 'ack-bulk' ? 'bulk' : 'single'}
        count={dialogState?.kind === 'ack-bulk' ? selectedIds.size : undefined}
        violationLabel={
          dialogState?.kind === 'ack-single'
            ? dialogState.violation.policy_name ?? dialogState.violation.violation_type
            : undefined
        }
        loading={isAnyMutating}
        onConfirm={handleAcknowledgeConfirm}
      />
      <RemediateDialog
        open={dialogState?.kind === 'remediate-single' || dialogState?.kind === 'remediate-bulk'}
        onOpenChange={(open) => { if (!open) closeDialog() }}
        action={
          dialogState?.kind === 'remediate-single'
            ? dialogState.action
            : dialogState?.kind === 'remediate-bulk'
            ? dialogState.action
            : 'dismiss'
        }
        mode={dialogState?.kind === 'remediate-bulk' ? 'bulk' : 'single'}
        count={dialogState?.kind === 'remediate-bulk' ? selectedIds.size : undefined}
        iccid={
          dialogState?.kind === 'remediate-single'
            ? dialogState.violation.iccid ?? dialogState.violation.sim_iccid ?? undefined
            : undefined
        }
        loading={isAnyMutating}
        onConfirm={handleRemediateConfirm}
      />

      <BulkActionBar
        count={selectedIds.size}
        loading={isAnyMutating}
        onAcknowledge={() => setDialogState({ kind: 'ack-bulk' })}
        onDismiss={() => setDialogState({ kind: 'remediate-bulk', action: 'dismiss' })}
        onClear={clearSelection}
      />

      <div ref={loadMoreRef} className="h-1" />
      {isFetchingNextPage && <div className="flex justify-center py-4"><Spinner className="h-5 w-5 text-accent" /></div>}
      {!hasNextPage && filtered.length > 0 && <p className="text-center py-3 text-xs text-text-tertiary">{filtered.length} violation{filtered.length !== 1 ? 's' : ''}</p>}
    </div>
  )
}
