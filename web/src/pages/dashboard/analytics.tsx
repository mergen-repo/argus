import { useState, useMemo, useRef } from 'react'
import {
  AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid,
} from 'recharts'
import {
  BarChart3, RefreshCw, AlertCircle, TrendingUp, Layers, Check, ImageDown,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { TimeframeSelector } from '@/components/ui/timeframe-selector'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { cn } from '@/lib/utils'
import { useUsageAnalytics, type UsageFilters } from '@/hooks/use-analytics'
import { useOperatorList } from '@/hooks/use-operators'
import { useAPNList } from '@/hooks/use-apns'
import type { UsagePeriod, UsageGroupBy, UsageMetric, TimeSeriesPoint } from '@/types/analytics'
import { Skeleton } from '@/components/ui/skeleton'
import { EntityLink } from '@/components/shared/entity-link'
import {
  formatBytes,
  formatNumber,
  formatDuration,
  formatDeltaPct,
  humanizeRatType,
  humanizeGroupDim,
} from '@/lib/format'
import type { DeltaTone } from '@/lib/format'
import { useChartExport } from '@/hooks/use-chart-export'
import { TwoWayTraffic } from '@/components/analytics/two-way-traffic'
import { UsageChartTooltip } from '@/components/analytics/usage-chart-tooltip'

const TIMEFRAME_TO_PERIOD: Record<string, UsagePeriod> = {
  '15m': '1h',
  '1h': '1h',
  '6h': '24h',
  '24h': '24h',
  '7d': '7d',
  '30d': '30d',
}

const GROUP_BY_OPTIONS = [
  { value: '', label: 'No Grouping' },
  { value: 'operator', label: 'Operator' },
  { value: 'apn', label: 'APN' },
  { value: 'rat_type', label: 'RAT Type' },
]

const METRIC_OPTIONS = [
  { value: 'total_bytes', label: 'Bytes' },
  { value: 'sessions', label: 'Sessions' },
  { value: 'auths', label: 'Auths' },
]

const RAT_TYPE_OPTIONS = [
  { value: '', label: 'All RATs' },
  { value: 'nb_iot', label: 'NB-IoT' },
  { value: 'lte_m', label: 'LTE-M' },
  { value: 'lte', label: 'LTE' },
  { value: 'nr_5g', label: '5G NR' },
]

const GROUP_COLORS = [
  'var(--color-accent)',
  'var(--color-success)',
  'var(--color-warning)',
  'var(--color-purple)',
  'var(--color-danger)',
  'var(--color-cyan)',
  'var(--color-info)',
  'var(--color-orange)',
]

const AGGREGATED_PERIODS: UsagePeriod[] = ['24h', '7d', '30d']

function resolveGroupLabel(groupBy: string | undefined, key: string | undefined | null): string {
  if (!key) return ''
  if (key === '__unassigned__') {
    switch (groupBy) {
      case 'apn': return 'Unassigned APN'
      case 'operator': return 'Unknown Operator'
      case 'rat_type': return 'Unknown RAT'
      default: return 'Unassigned'
    }
  }
  if (groupBy === 'rat_type') return humanizeRatType(key)
  return key
}

const TONE_CLASS: Record<DeltaTone, string> = {
  positive: 'text-success',
  negative: 'text-danger',
  neutral: 'text-text-tertiary',
  null: 'text-text-tertiary',
}

function DeltaBadge({
  current,
  previous,
  polarity = 'up-good',
}: {
  current: number
  previous: number
  polarity?: 'up-good' | 'down-good'
}) {
  const { text, tone } = formatDeltaPct(current, previous, polarity)
  if (tone === 'null') return null
  const cls = TONE_CLASS[tone]
  if (text === '↑') {
    return <span className={`inline-flex items-center text-xs font-mono ${cls}`}><TrendingUp className="h-3 w-3" /></span>
  }
  return (
    <span className={`inline-flex items-center gap-0.5 text-xs font-mono ${cls}`}>
      {text}
    </span>
  )
}

function UsageSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i}><CardContent className="pt-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
        ))}
      </div>
      <Card><CardContent className="pt-4"><Skeleton className="h-[300px] w-full" /></CardContent></Card>
      <Card><CardContent className="pt-4"><Skeleton className="h-[200px] w-full" /></CardContent></Card>
    </div>
  )
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load usage analytics</h2>
        <p className="text-sm text-text-secondary mb-4">Unable to fetch analytics data. Please try again.</p>
        <Button onClick={onRetry} variant="outline" className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    </div>
  )
}

function EmptyState({ from, to, hasFilter }: { from?: string; to?: string; hasFilter: boolean }) {
  const hint = hasFilter
    ? 'Try expanding the date range or clearing the active filter.'
    : 'Try expanding the date range.'
  const range = from && to
    ? ` (${new Date(from).toLocaleDateString('en-GB', { month: 'short', day: 'numeric' })} – ${new Date(to).toLocaleDateString('en-GB', { month: 'short', day: 'numeric' })})`
    : ''
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <BarChart3 className="h-10 w-10 text-text-tertiary" />
      <p className="text-sm text-text-secondary">No data for selected period{range}</p>
      <p className="text-xs text-text-tertiary">{hint}</p>
    </div>
  )
}

function PillFilter({ label, value, displayValue, options, onChange }: {
  label: string
  value: string
  displayValue: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
}) {
  const active = !!value
  return (
    <DropdownMenu>
      <DropdownMenuTrigger className={cn(
        'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
        active
          ? 'border-accent/30 bg-accent-dim text-accent'
          : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
      )}>
        <span>{label}{active ? `: ${displayValue}` : ''}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {options.map((opt) => (
          <DropdownMenuItem key={opt.value} onClick={() => onChange(opt.value)}>
            <span className="flex-1">{opt.label}</span>
            {value === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export default function AnalyticsPage() {
  const [timeframe, setTimeframe] = useState('24h')
  const [groupBy, setGroupBy] = useState<UsageGroupBy>('')
  const [metric, setMetric] = useState<UsageMetric>('total_bytes')
  const [operatorId, setOperatorId] = useState('')
  const [apnId, setApnId] = useState('')
  const [ratType, setRatType] = useState('')

  const period = TIMEFRAME_TO_PERIOD[timeframe] ?? '24h'

  const { data: operators } = useOperatorList()
  const { data: apns } = useAPNList({})

  const operatorOptions = useMemo(() => [
    { value: '', label: 'All Operators' },
    ...(operators ?? []).map((o) => ({ value: o.id, label: o.name })),
  ], [operators])

  const apnOptions = useMemo(() => [
    { value: '', label: 'All APNs' },
    ...(apns ?? []).map((a) => ({ value: a.id, label: a.display_name ?? a.name })),
  ], [apns])

  const filters: UsageFilters = {
    period,
    group_by: groupBy || undefined,
    operator_id: operatorId || undefined,
    apn_id: apnId || undefined,
    rat_type: ratType || undefined,
    compare: true,
  }

  const { data, isLoading, isError, refetch } = useUsageAnalytics(filters)

  const chartRef = useRef<HTMLDivElement>(null)
  const { exportPng, exporting } = useChartExport(chartRef)

  const hasFilter = !!(operatorId || apnId || ratType)

  const groupKeys = useMemo(() => {
    if (!data?.time_series || !groupBy) return []
    const keys = new Set<string>()
    data.time_series.forEach((p) => {
      if (p.group_key) keys.add(p.group_key)
    })
    return Array.from(keys)
  }, [data?.time_series, groupBy])

  const chartData = useMemo(() => {
    if (!data?.time_series) return []
    if (!groupBy || groupKeys.length === 0) {
      return data.time_series.map((p) => ({
        ts: p.ts,
        [metric]: p[metric as keyof TimeSeriesPoint],
      }))
    }
    const bucketMap = new Map<string, Record<string, unknown>>()
    data.time_series.forEach((p) => {
      const key = p.ts
      if (!bucketMap.has(key)) {
        bucketMap.set(key, { ts: p.ts })
      }
      const bucket = bucketMap.get(key)!
      if (p.group_key) {
        bucket[p.group_key] = p[metric as keyof TimeSeriesPoint]
      }
    })
    return Array.from(bucketMap.values())
  }, [data?.time_series, groupBy, groupKeys, metric])

  if (isLoading) return <UsageSkeleton />
  if (isError) return <ErrorState onRetry={() => refetch()} />

  const isEmpty = !data || data.time_series.length === 0
  const isAggregatedPeriod = AGGREGATED_PERIODS.includes(period)

  const prevTotals = data?.comparison?.previous_totals

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Analytics &mdash; Usage</h1>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <PillFilter
          label="Group"
          value={groupBy}
          displayValue={GROUP_BY_OPTIONS.find((o) => o.value === groupBy)?.label ?? 'No Grouping'}
          options={GROUP_BY_OPTIONS}
          onChange={(v) => setGroupBy(v as UsageGroupBy)}
        />
        <PillFilter
          label="Metric"
          value={metric}
          displayValue={METRIC_OPTIONS.find((o) => o.value === metric)?.label ?? 'Bytes'}
          options={METRIC_OPTIONS}
          onChange={(v) => setMetric(v as UsageMetric)}
        />
        <PillFilter
          label="Operator"
          value={operatorId}
          displayValue={operatorId ? (operators?.find((o) => o.id === operatorId)?.name ?? '') : ''}
          options={operatorOptions}
          onChange={setOperatorId}
        />
        <PillFilter
          label="APN"
          value={apnId}
          displayValue={apnId ? (apns?.find((a) => a.id === apnId)?.display_name ?? apns?.find((a) => a.id === apnId)?.name ?? '') : ''}
          options={apnOptions}
          onChange={setApnId}
        />
        <PillFilter
          label="RAT"
          value={ratType}
          displayValue={ratType ? (RAT_TYPE_OPTIONS.find((o) => o.value === ratType)?.label ?? ratType) : ''}
          options={RAT_TYPE_OPTIONS}
          onChange={setRatType}
        />
        <div className="ml-auto flex items-center gap-2">
          <TimeframeSelector value={timeframe} onChange={setTimeframe} />
          <Button variant="ghost" size="icon" onClick={() => refetch()} title="Refresh">
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {data && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Total Bytes</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                <AnimatedCounter value={data.totals.total_bytes} formatter={formatBytes} />
              </div>
              {prevTotals !== undefined && (
                <DeltaBadge current={data.totals.total_bytes} previous={prevTotals.total_bytes} />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Sessions</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                <AnimatedCounter value={data.totals.total_sessions} formatter={formatNumber} />
              </div>
              {prevTotals !== undefined && (
                <DeltaBadge current={data.totals.total_sessions} previous={prevTotals.total_sessions} />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Auths</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                <AnimatedCounter value={data.totals.total_auths} formatter={formatNumber} />
              </div>
              {prevTotals !== undefined && (
                <DeltaBadge current={data.totals.total_auths} previous={prevTotals.total_auths} />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Unique SIMs</span>
            </CardHeader>
            <CardContent className="pt-0">
              {data.totals.unique_sims === 0 && isAggregatedPeriod ? (
                <span
                  className="font-mono text-[22px] font-bold text-text-tertiary"
                  title="Unavailable for aggregated view"
                >—</span>
              ) : (
                <div className="font-mono text-[22px] font-bold text-text-primary">
                  <AnimatedCounter value={data.totals.unique_sims} formatter={formatNumber} />
                </div>
              )}
              {prevTotals !== undefined && data.totals.unique_sims > 0 && (
                <DeltaBadge current={data.totals.unique_sims} previous={prevTotals.unique_sims} />
              )}
            </CardContent>
          </Card>
        </div>
      )}

      <Card ref={chartRef}>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>
            {metric === 'total_bytes' ? 'Traffic' : metric === 'sessions' ? 'Sessions' : 'Authentications'} Over Time
          </CardTitle>
          <div className="flex items-center gap-2">
            {data && (
              <span className="text-[11px] text-text-tertiary font-mono">
                {data.bucket_size} buckets
              </span>
            )}
            {/* FIX-220: Export CSV deferred to FIX-236 streaming pattern — no button rendered. */}
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => exportPng('usage-chart.png')}
              disabled={exporting}
              title="Export chart as PNG"
            >
              <ImageDown className="h-3.5 w-3.5" />
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isEmpty ? (
            <EmptyState from={data?.from} to={data?.to} hasFilter={hasFilter} />
          ) : (
            <>
              {groupBy && groupKeys.length === 0 && (
                <p className="text-xs text-text-tertiary text-center py-4">
                  No groupings found — all values in &lsquo;__unassigned__&rsquo; bucket. Configure APN mappings to see breakdown.
                </p>
              )}
              <div className="h-[300px]">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={chartData} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
                    <XAxis
                      dataKey="ts"
                      tick={{ fill: 'var(--color-text-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
                      tickLine={false}
                      axisLine={false}
                      tickFormatter={(v) => {
                        const d = new Date(v)
                        if (!Number.isFinite(d.getTime())) return v
                        if (period === '1h' || period === '24h') {
                          return d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
                        }
                        return d.toLocaleDateString('en-GB', { month: 'short', day: 'numeric' })
                      }}
                    />
                    <YAxis
                      tick={{ fill: 'var(--color-text-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
                      tickLine={false}
                      axisLine={false}
                      tickFormatter={(v) => metric === 'total_bytes' ? formatBytes(v) : formatNumber(v)}
                    />
                    <Tooltip
                      content={(props) => (
                        <UsageChartTooltip
                          {...props}
                          period={period}
                          metric={metric}
                          groupBy={groupBy}
                          allData={data?.time_series ?? []}
                          groupKeys={groupKeys}
                        />
                      )}
                    />
                    {groupBy && groupKeys.length > 0 ? (
                      groupKeys.map((key, i) => (
                        <Area
                          key={key}
                          type="monotone"
                          dataKey={key}
                          stackId="1"
                          stroke={GROUP_COLORS[i % GROUP_COLORS.length]}
                          fill={GROUP_COLORS[i % GROUP_COLORS.length]}
                          fillOpacity={0.3}
                          strokeWidth={2}
                          name={resolveGroupLabel(groupBy, key)}
                        />
                      ))
                    ) : (
                      <Area
                        type="monotone"
                        dataKey={metric}
                        stroke="var(--color-accent)"
                        fill="var(--color-accent)"
                        fillOpacity={0.15}
                        strokeWidth={2}
                      />
                    )}
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </>
          )}
          {groupBy && groupKeys.length > 0 && (
            <div className="flex flex-wrap items-center gap-3 mt-3 pt-3 border-t border-border">
              <Layers className="h-3.5 w-3.5 text-text-tertiary" />
              {groupKeys.map((key, i) => (
                <div key={key} className="flex items-center gap-1.5 text-xs text-text-secondary">
                  <span
                    className="h-2.5 w-2.5 rounded-full"
                    style={{ backgroundColor: GROUP_COLORS[i % GROUP_COLORS.length] }}
                  />
                  {resolveGroupLabel(groupBy, key)}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {data && data.top_consumers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Top Consumers</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>#</TableHead>
                  <TableHead>ICCID</TableHead>
                  <TableHead className="hidden md:table-cell">IMSI</TableHead>
                  <TableHead className="hidden md:table-cell">MSISDN</TableHead>
                  <TableHead>Operator</TableHead>
                  <TableHead>APN</TableHead>
                  <TableHead className="text-right">IN / OUT</TableHead>
                  <TableHead className="text-right">Total</TableHead>
                  <TableHead className="text-right">Sessions</TableHead>
                  <TableHead className="text-right">Avg Duration</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.top_consumers.map((tc, i) => (
                  <TableRow key={tc.sim_id}>
                    <TableCell className="font-mono text-text-tertiary w-8">{i + 1}</TableCell>
                    <TableCell>
                      <EntityLink entityType="sim" entityId={tc.sim_id} label={tc.iccid} truncate />
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <span className="font-mono text-xs text-text-secondary" title={tc.imsi}>
                        {tc.imsi ? tc.imsi.slice(-7) : '—'}
                      </span>
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      {tc.msisdn
                        ? <span className="font-mono text-xs">{tc.msisdn}</span>
                        : <span className="text-text-tertiary">—</span>
                      }
                    </TableCell>
                    <TableCell className="text-xs">
                      <EntityLink entityType="operator" entityId={tc.operator_id ?? ''} label={tc.operator_name ?? ''} />
                    </TableCell>
                    <TableCell className="text-xs">
                      <EntityLink entityType="apn" entityId={tc.apn_id ?? ''} label={tc.apn_name ?? ''} />
                    </TableCell>
                    <TableCell className="text-right">
                      <TwoWayTraffic in={tc.bytes_in} out={tc.bytes_out} />
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatBytes(tc.total_bytes)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatNumber(tc.sessions)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {tc.avg_duration_sec != null ? formatDuration(tc.avg_duration_sec) : '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {data && data.breakdowns && Object.keys(data.breakdowns).length > 0 && (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
          {Object.entries(data.breakdowns).map(([dim, items]) => (
            <Card key={dim}>
              <CardHeader>
                <CardTitle>{humanizeGroupDim(dim)} Breakdown</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex flex-col gap-2">
                  {items.map((item) => {
                    const label = resolveGroupLabel(dim, item.key)
                    return (
                      <div key={item.key} className="flex items-center justify-between">
                        <span className="text-xs text-text-secondary truncate" title={label}>
                          {label}
                        </span>
                        <div className="flex items-center gap-2 flex-shrink-0 ml-2">
                          <div className="w-20 h-1.5 bg-bg-hover rounded-full overflow-hidden">
                            <div
                              className="h-full rounded-full bg-accent"
                              style={{ width: `${Math.min(item.percentage, 100)}%` }}
                            />
                          </div>
                          <span className="text-xs font-mono text-text-primary w-12 text-right">
                            {item.percentage.toFixed(1)}%
                          </span>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
