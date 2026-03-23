import { useState, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid,
} from 'recharts'
import {
  BarChart3, RefreshCw, AlertCircle, TrendingUp, TrendingDown, Layers,
  Filter, ToggleLeft, ToggleRight,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Select } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { useUsageAnalytics, type UsageFilters } from '@/hooks/use-analytics'
import type { UsagePeriod, UsageGroupBy, UsageMetric, TimeSeriesPoint } from '@/types/analytics'

const PERIOD_OPTIONS = [
  { value: '1h', label: '1 Hour' },
  { value: '24h', label: '24 Hours' },
  { value: '7d', label: '7 Days' },
  { value: '30d', label: '30 Days' },
  { value: 'custom', label: 'Custom' },
]

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

import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes, formatNumber } from '@/lib/format'

function formatTimestamp(ts: string, period: string): string {
  const d = new Date(ts)
  if (period === '1h' || period === '24h') {
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function DeltaBadge({ delta }: { delta: number }) {
  if (delta === 0) return null
  const positive = delta > 0
  return (
    <span className={`inline-flex items-center gap-0.5 text-xs font-mono ${positive ? 'text-success' : 'text-danger'}`}>
      {positive ? <TrendingUp className="h-3 w-3" /> : <TrendingDown className="h-3 w-3" />}
      {positive ? '+' : ''}{delta.toFixed(1)}%
    </span>
  )
}

function UsageSkeleton() {
  return (
    <div className="p-6 space-y-4">
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

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <BarChart3 className="h-10 w-10 text-text-tertiary" />
      <p className="text-sm text-text-secondary">No data for selected period</p>
    </div>
  )
}

const tooltipStyle = {
  backgroundColor: 'var(--color-bg-elevated)',
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--color-text-primary)',
  fontSize: '12px',
}

export default function AnalyticsPage() {
  const navigate = useNavigate()
  const [period, setPeriod] = useState<UsagePeriod>('24h')
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [groupBy, setGroupBy] = useState<UsageGroupBy>('')
  const [metric, setMetric] = useState<UsageMetric>('total_bytes')
  const [compare, setCompare] = useState(false)
  const [operatorId, setOperatorId] = useState('')
  const [apnId, setApnId] = useState('')
  const [ratType, setRatType] = useState('')
  const [showFilters, setShowFilters] = useState(false)

  const filters: UsageFilters = {
    period,
    from: period === 'custom' ? customFrom : undefined,
    to: period === 'custom' ? customTo : undefined,
    group_by: groupBy || undefined,
    operator_id: operatorId || undefined,
    apn_id: apnId || undefined,
    rat_type: ratType || undefined,
    compare,
  }

  const { data, isLoading, isError, refetch } = useUsageAnalytics(filters)

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
        ts: formatTimestamp(p.ts, period),
        [metric]: p[metric as keyof TimeSeriesPoint],
      }))
    }
    const bucketMap = new Map<string, Record<string, unknown>>()
    data.time_series.forEach((p) => {
      const key = p.ts
      if (!bucketMap.has(key)) {
        bucketMap.set(key, { ts: formatTimestamp(p.ts, period) })
      }
      const bucket = bucketMap.get(key)!
      if (p.group_key) {
        bucket[p.group_key] = p[metric as keyof TimeSeriesPoint]
      }
    })
    return Array.from(bucketMap.values())
  }, [data?.time_series, groupBy, groupKeys, metric, period])

  if (isLoading) return <UsageSkeleton />
  if (isError) return <ErrorState onRetry={() => refetch()} />

  const isEmpty = !data || data.time_series.length === 0

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Analytics &mdash; Usage</h1>
        <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1">
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Select
          options={PERIOD_OPTIONS}
          value={period}
          onChange={(e) => setPeriod(e.target.value as UsagePeriod)}
          className="w-32"
        />
        {period === 'custom' && (
          <>
            <Input
              type="datetime-local"
              value={customFrom}
              onChange={(e) => setCustomFrom(e.target.value)}
              className="w-48"
              placeholder="From"
            />
            <Input
              type="datetime-local"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              className="w-48"
              placeholder="To"
            />
          </>
        )}
        <Select
          options={GROUP_BY_OPTIONS}
          value={groupBy}
          onChange={(e) => setGroupBy(e.target.value as UsageGroupBy)}
          className="w-36"
        />
        <Select
          options={METRIC_OPTIONS}
          value={metric}
          onChange={(e) => setMetric(e.target.value as UsageMetric)}
          className="w-28"
        />
        <Button
          variant={compare ? 'default' : 'outline'}
          size="sm"
          onClick={() => setCompare(!compare)}
          className="gap-1"
        >
          {compare ? <ToggleRight className="h-3.5 w-3.5" /> : <ToggleLeft className="h-3.5 w-3.5" />}
          Compare
        </Button>
        <Button
          variant={showFilters ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setShowFilters(!showFilters)}
          className="gap-1"
        >
          <Filter className="h-3.5 w-3.5" />
          Filters
        </Button>
      </div>

      {showFilters && (
        <Card>
          <CardContent className="pt-4">
            <div className="flex flex-wrap items-center gap-3">
              <div>
                <label className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1 block">Operator ID</label>
                <Input
                  value={operatorId}
                  onChange={(e) => setOperatorId(e.target.value)}
                  placeholder="UUID"
                  className="w-64"
                />
              </div>
              <div>
                <label className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1 block">APN ID</label>
                <Input
                  value={apnId}
                  onChange={(e) => setApnId(e.target.value)}
                  placeholder="UUID"
                  className="w-64"
                />
              </div>
              <div>
                <label className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1 block">RAT Type</label>
                <Select
                  options={[
                    { value: '', label: 'All' },
                    { value: '4G', label: '4G LTE' },
                    { value: '5G', label: '5G NR' },
                    { value: '3G', label: '3G' },
                    { value: '2G', label: '2G' },
                  ]}
                  value={ratType}
                  onChange={(e) => setRatType(e.target.value)}
                  className="w-28"
                />
              </div>
              <Button variant="ghost" size="sm" onClick={() => { setOperatorId(''); setApnId(''); setRatType('') }}>
                Clear
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {data && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Total Bytes</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                {formatBytes(data.totals.total_bytes)}
              </div>
              {data.comparison && <DeltaBadge delta={data.comparison.bytes_delta_pct} />}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Sessions</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                {formatNumber(data.totals.total_sessions)}
              </div>
              {data.comparison && <DeltaBadge delta={data.comparison.sessions_delta_pct} />}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Auths</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                {formatNumber(data.totals.total_auths)}
              </div>
              {data.comparison && <DeltaBadge delta={data.comparison.auths_delta_pct} />}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Unique SIMs</span>
            </CardHeader>
            <CardContent className="pt-0">
              <div className="font-mono text-[22px] font-bold text-text-primary">
                {formatNumber(data.totals.unique_sims)}
              </div>
              {data.comparison && <DeltaBadge delta={data.comparison.sims_delta_pct} />}
            </CardContent>
          </Card>
        </div>
      )}

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>
            {metric === 'total_bytes' ? 'Traffic' : metric === 'sessions' ? 'Sessions' : 'Authentications'} Over Time
          </CardTitle>
          {data && (
            <span className="text-[11px] text-text-tertiary font-mono">
              {data.bucket_size} buckets
            </span>
          )}
        </CardHeader>
        <CardContent>
          {isEmpty ? (
            <EmptyState />
          ) : (
            <div className="h-[300px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
                  <XAxis
                    dataKey="ts"
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis
                    tick={{ fill: 'var(--color-text-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(v) => metric === 'total_bytes' ? formatBytes(v) : formatNumber(v)}
                  />
                  <Tooltip
                    contentStyle={tooltipStyle}
                    formatter={(value) => [
                      metric === 'total_bytes' ? formatBytes(Number(value)) : formatNumber(Number(value)),
                      metric === 'total_bytes' ? 'Bytes' : metric === 'sessions' ? 'Sessions' : 'Auths',
                    ]}
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
                        name={key}
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
                  {key}
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
                  <TableHead>SIM ID</TableHead>
                  <TableHead className="text-right">Usage</TableHead>
                  <TableHead className="text-right">Sessions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.top_consumers.map((tc, i) => (
                  <TableRow
                    key={tc.sim_id}
                    className="cursor-pointer"
                    onClick={() => navigate(`/sims/${tc.sim_id}`)}
                  >
                    <TableCell className="font-mono text-text-tertiary w-8">{i + 1}</TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-accent hover:underline">
                        {tc.sim_id.slice(0, 8)}...
                      </span>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatBytes(tc.total_bytes)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatNumber(tc.sessions)}
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
                <CardTitle className="capitalize">{dim.replace('_id', '')} Breakdown</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex flex-col gap-2">
                  {items.map((item) => (
                    <div key={item.key} className="flex items-center justify-between">
                      <span className="text-xs text-text-secondary font-mono truncate max-w-[120px]">
                        {item.key.slice(0, 8)}...
                      </span>
                      <div className="flex items-center gap-2">
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
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
