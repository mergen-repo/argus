import { useState, useRef, useEffect, useMemo, useCallback } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell,
} from 'recharts'
import {
  Shield, AlertCircle, AlertTriangle, Search, RefreshCw,
  ExternalLink, Clock, ChevronDown, ChevronUp,
  Activity, Ban, Tag, Bell, FileText, MoreHorizontal, CheckCircle2, BookOpen, ArrowUpRight,
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
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { timeAgo, formatNumber } from '@/lib/format'
import type { ListResponse } from '@/types/sim'

interface PolicyViolation {
  id: string
  tenant_id: string
  sim_id: string
  sim_iccid?: string
  policy_id: string
  policy_name?: string
  version_id: string
  rule_index: number
  violation_type: string
  action_taken: string
  details: Record<string, unknown>
  session_id?: string
  operator_id?: string
  operator_name?: string
  apn_id?: string
  apn_name?: string
  severity: string
  created_at: string
  acknowledged_at?: string | null
  acknowledged_by?: string | null
}

function useAcknowledgeViolation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, note }: { id: string; note?: string }) => {
      const res = await api.post<{ status: string; data: { id: string; acknowledged_at: string; acknowledged_by: string; note?: string } }>(
        `/policy-violations/${id}/acknowledge`,
        { note },
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['violations'] })
    },
  })
}

interface Filters {
  violation_type: string
  severity: string
}

const TYPE_OPTIONS = [
  { value: '', label: 'All Types' },
  { value: 'block', label: 'Block' },
  { value: 'disconnect', label: 'Disconnect' },
  { value: 'suspend', label: 'Suspend' },
  { value: 'throttle', label: 'Throttle' },
  { value: 'policy_notify', label: 'Notify' },
  { value: 'policy_log', label: 'Log' },
  { value: 'policy_tag', label: 'Tag' },
]

const SEVERITY_OPTIONS = [
  { value: '', label: 'All Severities' },
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
]

const TYPE_COLORS: Record<string, string> = {
  block: 'var(--color-danger)', disconnect: 'var(--color-danger)', suspend: 'var(--color-danger)',
  throttle: 'var(--color-warning)', policy_notify: 'var(--color-info)', policy_log: 'var(--color-accent)',
  policy_tag: 'var(--color-purple)',
}

const SEV_COLORS: Record<string, string> = {
  critical: 'var(--color-danger)', warning: 'var(--color-warning)', info: 'var(--color-accent)',
}

function typeIcon(type: string) {
  switch (type) {
    case 'block': case 'disconnect': case 'suspend': return <Ban className="h-3.5 w-3.5" />
    case 'throttle': return <Activity className="h-3.5 w-3.5" />
    case 'policy_notify': return <Bell className="h-3.5 w-3.5" />
    case 'policy_log': return <FileText className="h-3.5 w-3.5" />
    case 'policy_tag': return <Tag className="h-3.5 w-3.5" />
    default: return <Shield className="h-3.5 w-3.5" />
  }
}

function severityVariant(s: string): 'danger' | 'warning' | 'default' {
  switch (s) { case 'critical': return 'danger'; case 'warning': return 'warning'; default: return 'default' }
}

function useViolations(filters: Filters) {
  return useInfiniteQuery({
    queryKey: ['violations', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (filters.violation_type) params.set('violation_type', filters.violation_type)
      if (filters.severity) params.set('severity', filters.severity)
      const res = await api.get<ListResponse<PolicyViolation>>(`/policy-violations?${params.toString()}`)
      return { ...res.data, data: res.data.data ?? [] }
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.meta?.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

function useViolationCounts() {
  return useQuery({
    queryKey: ['violations', 'counts'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: Record<string, number> }>('/policy-violations/counts')
      return res.data.data ?? {}
    },
    staleTime: 30_000,
  })
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
    rat_type: 'RAT Type',
  }
  return map[key] || key.replace(/_/g, ' ')
}

function formatDetailValue(key: string, val: unknown): string {
  if (typeof val === 'number') {
    if (key.includes('bytes') || key.includes('rate')) return `${(val / 1_000_000).toFixed(1)} MB`
    return val.toLocaleString()
  }
  return String(val)
}

export default function ViolationsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo<Filters>(() => ({
    violation_type: searchParams.get('violation_type') ?? '',
    severity: searchParams.get('severity') ?? '',
  }), [searchParams])
  const setFilters = useCallback((updater: Filters | ((prev: Filters) => Filters)) => {
    const next = typeof updater === 'function' ? updater(filters) : updater
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      if (next.violation_type) p.set('violation_type', next.violation_type); else p.delete('violation_type')
      if (next.severity) p.set('severity', next.severity); else p.delete('severity')
      return p
    }, { replace: false })
  }, [filters, setSearchParams])
  const [searchInput, setSearchInput] = useState('')
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set())
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data, isLoading, isError, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } = useViolations(filters)
  const { data: counts } = useViolationCounts()
  const acknowledgeMutation = useAcknowledgeViolation()

  const handleDismiss = useCallback(async (v: PolicyViolation) => {
    setDismissedIds((prev) => new Set([...prev, v.id]))
    try {
      await acknowledgeMutation.mutateAsync({ id: v.id })
      toast.success('Violation dismissed')
    } catch {
      setDismissedIds((prev) => { const n = new Set(prev); n.delete(v.id); return n })
      toast.error('Failed to dismiss violation')
    }
  }, [acknowledgeMutation])

  const violations = useMemo(() => data?.pages.flatMap((p) => p.data ?? []) ?? [], [data])

  const filtered = useMemo(() => {
    if (!searchInput) return violations
    const q = searchInput.toLowerCase()
    return violations.filter((v) =>
      v.violation_type.includes(q) || v.action_taken.includes(q) ||
      v.sim_iccid?.toLowerCase().includes(q) || v.policy_name?.toLowerCase().includes(q) ||
      v.operator_name?.toLowerCase().includes(q) || v.severity.includes(q)
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
      name: sev, value: count, fill: SEV_COLORS[sev] || 'var(--color-text-tertiary)',
    }))
  }, [violations])

  const topSims = useMemo(() => {
    const agg: Record<string, { count: number; iccid: string; simId: string }> = {}
    violations.forEach((v) => {
      if (!agg[v.sim_id]) agg[v.sim_id] = { count: 0, iccid: v.sim_iccid || v.sim_id.slice(0, 12), simId: v.sim_id }
      agg[v.sim_id].count++
    })
    return Object.values(agg).sort((a, b) => b.count - a.count).slice(0, 5)
  }, [violations])

  const toggleExpanded = useCallback((id: string) => {
    setExpandedIds((prev) => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
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
          <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1.5 text-xs"><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
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
      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
          <Input type="text" value={searchInput} onChange={(e) => setSearchInput(e.target.value)} placeholder="Filter by SIM, policy, type..."
            className="h-8 pl-8 pr-3 text-xs" />
        </div>
        <Select options={TYPE_OPTIONS} value={filters.violation_type} onChange={(e) => setFilters((f) => ({ ...f, violation_type: e.target.value }))} className="w-36" />
        <Select options={SEVERITY_OPTIONS} value={filters.severity} onChange={(e) => setFilters((f) => ({ ...f, severity: e.target.value }))} className="w-36" />
        {(filters.violation_type || filters.severity) && (
          <Button variant="ghost" size="sm" onClick={() => setFilters({ violation_type: '', severity: '' })} className="text-[11px] text-text-tertiary hover:text-accent h-auto p-0">Clear</Button>
        )}
      </div>

      {/* Violations List */}
      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 gap-4">
          <div className="h-16 w-16 rounded-xl bg-success-dim border border-success/20 flex items-center justify-center">
            <Shield className="h-8 w-8 text-success" />
          </div>
          <p className="text-sm font-medium text-text-primary">No violations</p>
          <p className="text-xs text-text-secondary">{filters.violation_type || filters.severity ? 'Try adjusting filters' : 'No policy violations recorded.'}</p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {filtered.filter((v) => !dismissedIds.has(v.id) && !v.acknowledged_at).map((v) => {
            const expanded = expandedIds.has(v.id)
            return (
              <div key={v.id} className={cn(
                'rounded-[var(--radius-md)] border bg-bg-surface overflow-hidden transition-colors',
                v.severity === 'critical' && 'border-danger/30',
                v.severity === 'warning' && 'border-warning/20',
              )}>
                <div className="flex items-center gap-3 px-4 py-2.5 cursor-pointer hover:bg-bg-hover/50 transition-colors" onClick={() => toggleExpanded(v.id)}>
                  <span className={v.severity === 'critical' ? 'text-danger' : v.severity === 'warning' ? 'text-warning' : 'text-text-tertiary'}>{typeIcon(v.violation_type)}</span>
                  <Badge variant={severityVariant(v.severity)} className="text-[9px] shrink-0">{v.severity}</Badge>
                  <span className="text-xs font-medium text-text-primary">{v.violation_type.replace(/_/g, ' ')}</span>
                  <span className="text-[10px] text-text-tertiary">→ {v.action_taken}</span>
                  <Link to={`/sims/${v.sim_id}`} onClick={(e) => e.stopPropagation()} className="hidden sm:flex items-center gap-1 text-[11px] font-mono text-accent hover:underline shrink-0">
                    <ExternalLink className="h-3 w-3" />{v.sim_iccid || 'SIM'}
                  </Link>
                  {v.operator_name && <span className="hidden lg:block text-[10px] text-text-tertiary">{v.operator_name}</span>}
                  <span className="hidden md:flex items-center gap-1 text-[10px] text-text-tertiary font-mono shrink-0 ml-auto"><Clock className="h-3 w-3" />{timeAgo(v.created_at)}</span>
                  <div className="shrink-0 text-text-tertiary">{expanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}</div>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      className="h-6 w-6 p-0 shrink-0 text-text-tertiary hover:text-text-primary inline-flex items-center justify-center rounded transition-colors hover:bg-bg-hover"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <MoreHorizontal className="h-3.5 w-3.5" />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-48">
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => navigate(`/sims/${v.sim_id}?action=suspend`)}
                      >
                        <Ban className="h-3.5 w-3.5 text-danger" />
                        Suspend SIM
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => navigate(`/policies/${v.policy_id}?rule=${v.rule_index}`)}
                      >
                        <BookOpen className="h-3.5 w-3.5 text-accent" />
                        Review Policy
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => handleDismiss(v)}
                      >
                        <CheckCircle2 className="h-3.5 w-3.5 text-success" />
                        Dismiss
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        className="text-xs gap-2"
                        onSelect={() => navigate('/notifications')}
                      >
                        <ArrowUpRight className="h-3.5 w-3.5 text-warning" />
                        Escalate
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>

                {expanded && (
                  <div className="border-t border-border bg-bg-primary/50 px-4 py-3 space-y-3 animate-slide-up-in">
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">SIM</span>
                        <Link to={`/sims/${v.sim_id}`} className="text-xs text-accent hover:underline font-mono">{v.sim_iccid || v.sim_id.slice(0, 12)}</Link>
                      </div>
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Policy</span>
                        <Link to={`/policies/${v.policy_id}`} className="text-xs text-accent hover:underline">{v.policy_name || 'View Policy'}</Link>
                      </div>
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Operator</span>
                        <span className="text-xs text-text-primary">{v.operator_name || '—'}</span>
                      </div>
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Time</span>
                        <span className="text-xs text-text-primary font-mono">{new Date(v.created_at).toLocaleString()}</span>
                      </div>
                    </div>
                    {v.details && Object.keys(v.details).length > 0 && (
                      <div>
                        <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-2">Details</span>
                        <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                          {Object.entries(v.details).map(([key, val]) => (
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
              </div>
            )
          })}
        </div>
      )}

      <div ref={loadMoreRef} className="h-1" />
      {isFetchingNextPage && <div className="flex justify-center py-4"><Spinner className="h-5 w-5 text-accent" /></div>}
      {!hasNextPage && filtered.length > 0 && <p className="text-center py-3 text-xs text-text-tertiary">{filtered.length} violation{filtered.length !== 1 ? 's' : ''}</p>}
    </div>
  )
}
