import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid,
} from 'recharts'
import {
  DollarSign, TrendingUp, TrendingDown, RefreshCw, AlertCircle,
  Lightbulb, ArrowRight, BarChart3,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { TimeframeSelector } from '@/components/ui/timeframe-selector'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { useCostAnalytics, type CostFilters } from '@/hooks/use-analytics'
import type { UsagePeriod } from '@/types/analytics'

const COST_TIMEFRAME_OPTIONS = [
  { value: '1h', label: '1h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes, formatNumber } from '@/lib/format'

function formatCurrency(n: number): string {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function DeltaBadge({ delta }: { delta: number }) {
  if (delta === 0) return null
  const positive = delta > 0
  return (
    <span className={`inline-flex items-center gap-0.5 text-xs font-mono ${positive ? 'text-danger' : 'text-success'}`}>
      {positive ? <TrendingUp className="h-3 w-3" /> : <TrendingDown className="h-3 w-3" />}
      {positive ? '+' : ''}{delta.toFixed(1)}%
    </span>
  )
}

function CostSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-6 w-48" />
      <div className="flex gap-3"><Skeleton className="h-9 w-32" /></div>
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card><CardContent className="pt-4"><Skeleton className="h-24 w-full" /></CardContent></Card>
        <Card className="col-span-2"><CardContent className="pt-4"><Skeleton className="h-24 w-full" /></CardContent></Card>
      </div>
      <Card><CardContent className="pt-4"><Skeleton className="h-[250px] w-full" /></CardContent></Card>
      <Card><CardContent className="pt-4"><Skeleton className="h-[200px] w-full" /></CardContent></Card>
    </div>
  )
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load cost analytics</h2>
        <p className="text-sm text-text-secondary mb-4">Unable to fetch cost data. Please try again.</p>
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
      <DollarSign className="h-10 w-10 text-text-tertiary" />
      <p className="text-sm text-text-secondary">No cost data for selected period</p>
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

const SUGGESTION_ACTIONS: Record<string, { label: string; path: string }> = {
  operator_switch: { label: 'Bulk Operator Switch', path: '/sims?action=bulk-operator-switch' },
  terminate: { label: 'Bulk Terminate', path: '/sims?action=bulk-terminate' },
  plan_downgrade: { label: 'Review SIMs', path: '/sims?sort=usage_asc' },
}

export default function AnalyticsCostPage() {
  const navigate = useNavigate()
  const [period, setPeriod] = useState<UsagePeriod>('30d')

  const filters: CostFilters = {
    period,
  }

  const { data, isLoading, isError, refetch } = useCostAnalytics(filters)

  if (isLoading) return <CostSkeleton />
  if (isError) return <ErrorState onRetry={() => refetch()} />

  const isEmpty = !data || (data.by_operator.length === 0 && data.total_cost === 0)

  const chartData = (data?.by_operator ?? []).map((op) => ({
    name: op.operator_id.slice(0, 8),
    cost: op.total_usage_cost,
    carrier: op.total_carrier_cost,
  }))

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Analytics &mdash; Cost</h1>
        <div className="flex items-center gap-2">
          <TimeframeSelector
            value={period}
            onChange={(v) => setPeriod(v as UsagePeriod)}
            options={COST_TIMEFRAME_OPTIONS}
          />
          <Button variant="outline" size="icon" onClick={() => refetch()} className="h-8 w-8">
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {isEmpty ? (
        <EmptyState />
      ) : (
        <>
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            <Card className="relative overflow-hidden">
              <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-warning" />
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
                    Total Cost
                  </span>
                  <DollarSign className="h-4 w-4 text-warning opacity-70" />
                </div>
              </CardHeader>
              <CardContent className="pt-0">
                <div className="font-mono text-[28px] font-bold text-text-primary leading-none mb-1">
                  <AnimatedCounter value={data!.total_cost} formatter={formatCurrency} />
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-[11px] text-text-tertiary">{data!.currency}</span>
                  {data!.comparison && <DeltaBadge delta={data!.comparison.cost_delta_pct} />}
                </div>
                {data!.comparison && (
                  <p className="text-[11px] text-text-tertiary mt-1">
                    Previous: {formatCurrency(data!.comparison.previous_total_cost)}
                  </p>
                )}
              </CardContent>
            </Card>

            {data!.comparison && (
              <>
                <Card>
                  <CardHeader className="pb-2">
                    <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
                      Traffic Change
                    </span>
                  </CardHeader>
                  <CardContent className="pt-0">
                    <div className="font-mono text-[22px] font-bold text-text-primary">
                      {formatBytes(data!.comparison.previous_bytes)}
                    </div>
                    <DeltaBadge delta={data!.comparison.bytes_delta_pct} />
                    <p className="text-[11px] text-text-tertiary mt-1">Previous period volume</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
                      SIM Count Change
                    </span>
                  </CardHeader>
                  <CardContent className="pt-0">
                    <div className="font-mono text-[22px] font-bold text-text-primary">
                      {formatNumber(data!.comparison.previous_sims)}
                    </div>
                    <DeltaBadge delta={data!.comparison.sims_delta_pct} />
                    <p className="text-[11px] text-text-tertiary mt-1">Previous period SIMs</p>
                  </CardContent>
                </Card>
              </>
            )}
          </div>

          {chartData.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Carrier Comparison</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="h-[250px]">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                      data={chartData}
                      layout="vertical"
                      margin={{ left: 0, right: 16, top: 0, bottom: 0 }}
                    >
                      <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" horizontal={false} />
                      <XAxis
                        type="number"
                        tick={{ fill: 'var(--color-text-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }}
                        tickLine={false}
                        axisLine={false}
                        tickFormatter={(v) => `$${formatNumber(v)}`}
                      />
                      <YAxis
                        type="category"
                        dataKey="name"
                        width={80}
                        tick={{ fill: 'var(--color-text-secondary)', fontSize: 12, fontFamily: 'var(--font-mono)' }}
                        tickLine={false}
                        axisLine={false}
                      />
                      <Tooltip
                        contentStyle={tooltipStyle}
                        formatter={(value, name) => [
                          formatCurrency(Number(value)),
                          name === 'cost' ? 'Usage Cost' : 'Carrier Cost',
                        ]}
                      />
                      <Bar dataKey="cost" fill="var(--color-accent)" radius={[0, 4, 4, 0]} barSize={14} name="cost" />
                      <Bar dataKey="carrier" fill="var(--color-warning)" radius={[0, 4, 4, 0]} barSize={14} name="carrier" />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
                <div className="flex items-center gap-4 mt-2 pt-2 border-t border-border">
                  <div className="flex items-center gap-1.5 text-xs text-text-secondary">
                    <span className="h-2.5 w-2.5 rounded-full bg-accent" /> Usage Cost
                  </div>
                  <div className="flex items-center gap-1.5 text-xs text-text-secondary">
                    <span className="h-2.5 w-2.5 rounded-full bg-warning" /> Carrier Cost
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {data!.cost_per_mb.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Cost per MB</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Operator</TableHead>
                      <TableHead>RAT Type</TableHead>
                      <TableHead className="text-right">Avg $/MB</TableHead>
                      <TableHead className="text-right">Total Cost</TableHead>
                      <TableHead className="text-right">Total MB</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data!.cost_per_mb.map((row, i) => (
                      <TableRow key={i}>
                        <TableCell className="font-mono text-xs">{row.operator_id.slice(0, 8)}...</TableCell>
                        <TableCell>
                          <Badge variant="secondary">{row.rat_type || 'N/A'}</Badge>
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs">
                          ${row.avg_cost_per_mb.toFixed(4)}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs">
                          {formatCurrency(row.total_cost)}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs">
                          {row.total_mb.toFixed(1)} MB
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {data!.suggestions.length > 0 && (
            <Card>
              <CardHeader>
                <div className="flex items-center gap-2">
                  <Lightbulb className="h-4 w-4 text-warning" />
                  <CardTitle>Optimization Suggestions</CardTitle>
                </div>
                <CardDescription>
                  Potential savings identified based on current usage patterns
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                  {data!.suggestions.map((s, i) => {
                    const action = SUGGESTION_ACTIONS[s.action]
                    return (
                      <Card key={i} className="border-warning/20">
                        <CardContent className="pt-4">
                          <div className="flex items-start justify-between gap-3">
                            <div className="flex-1 min-w-0">
                              <Badge variant="warning" className="mb-2">{s.type.replace(/_/g, ' ')}</Badge>
                              <p className="text-sm text-text-primary mb-2">{s.description}</p>
                              <div className="flex items-center gap-3 text-xs text-text-secondary">
                                <span>{formatNumber(s.affected_sim_count)} SIMs affected</span>
                                <span className="font-mono text-success font-medium">
                                  Save {formatCurrency(s.potential_savings)}
                                </span>
                              </div>
                            </div>
                            {action && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="gap-1 flex-shrink-0"
                                onClick={() => navigate(action.path)}
                              >
                                {action.label}
                                <ArrowRight className="h-3 w-3" />
                              </Button>
                            )}
                          </div>
                        </CardContent>
                      </Card>
                    )
                  })}
                </div>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  )
}
