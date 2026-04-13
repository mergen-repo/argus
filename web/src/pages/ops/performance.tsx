import { Activity, RefreshCw, TrendingUp, AlertCircle, Cpu, MemoryStick, Zap } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useOpsSnapshot } from '@/hooks/use-ops'
import { useNavigate } from 'react-router-dom'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { useMemo } from 'react'
import type { RouteMetric } from '@/types/ops'

function formatBytes(bytes: number) {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

function ErrorRateBadge({ rate }: { rate: number }) {
  if (rate === 0) return <Badge className="bg-success-dim text-success border-0">0%</Badge>
  if (rate < 0.01) return <Badge className="bg-warning-dim text-warning border-0">{(rate * 100).toFixed(2)}%</Badge>
  return <Badge className="bg-danger-dim text-danger border-0">{(rate * 100).toFixed(2)}%</Badge>
}

export default function PerformanceDashboard() {
  const navigate = useNavigate()
  const { data, isLoading, refetch } = useOpsSnapshot(15_000)

  const topRoutes = useMemo(
    () => (data?.http.by_route ?? []).slice(0, 10),
    [data],
  )

  const runtimeChartData = useMemo(() => {
    if (!data) return []
    return [
      {
        t: 'now',
        goroutines: data.runtime.goroutines,
        mem_mb: Math.round(data.runtime.mem_alloc_bytes / (1024 * 1024)),
        gc_ms: data.runtime.gc_pause_p99_ms,
      },
    ]
  }, [data])

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-64" />
        <div className="grid grid-cols-6 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
        <Skeleton className="h-64" />
      </div>
    )
  }

  const totals = data?.http.totals
  const runtime = data?.runtime

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-[15px] font-semibold text-text-primary flex items-center gap-2">
            <Activity className="h-4 w-4 text-accent" />
            Performance Dashboard
          </h1>
          <p className="text-[11px] text-text-tertiary mt-0.5">Auto-refresh every 15s</p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => refetch()}
          className="text-text-secondary hover:text-text-primary"
        >
          <RefreshCw className="h-4 w-4 mr-1" />
          Refresh
        </Button>
      </div>

      {!data ? (
        <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
          <CardContent className="p-6 text-center">
            <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
            <p className="text-[14px] text-text-secondary">No metrics emitted yet — generate traffic to populate.</p>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
            {[
              { label: 'HTTP req/s', value: totals?.requests.toLocaleString() ?? '—', icon: TrendingUp, color: 'text-accent' },
              { label: 'Error %', value: totals ? `${(totals.error_rate * 100).toFixed(2)}%` : '—', icon: AlertCircle, color: totals && totals.error_rate > 0.01 ? 'text-danger' : 'text-success' },
              { label: 'Goroutines', value: runtime?.goroutines.toLocaleString() ?? '—', icon: Activity, color: 'text-purple' },
              { label: 'Mem Alloc', value: runtime ? formatBytes(runtime.mem_alloc_bytes) : '—', icon: MemoryStick, color: 'text-info' },
              { label: 'GC p99', value: runtime ? `${runtime.gc_pause_p99_ms.toFixed(2)}ms` : '—', icon: Zap, color: 'text-warning' },
              { label: 'Total Errors', value: totals?.errors.toLocaleString() ?? '—', icon: AlertCircle, color: 'text-danger' },
            ].map(({ label, value, icon: Icon, color }) => (
              <Card key={label} className="bg-bg-surface border-border rounded-[10px] shadow-card hover:shadow-glow hover:border-accent transition-all">
                <CardContent className="p-6">
                  <div className="flex items-center gap-2 mb-2">
                    <Icon className={`h-4 w-4 ${color}`} />
                    <span className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary">{label}</span>
                  </div>
                  <div className="text-[28px] font-mono font-bold text-text-primary">{value}</div>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
              <CardHeader className="pb-3">
                <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
                  Latency Heatmap — Top Routes
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow className="border-border hover:bg-transparent">
                      <TableHead className="text-[11px] text-text-tertiary pl-6">Route</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">p50</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">p95</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">p99</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">Err%</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topRoutes.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="text-center text-[13px] text-text-tertiary py-8">
                          No route data yet
                        </TableCell>
                      </TableRow>
                    ) : (
                      topRoutes.map((r: RouteMetric) => (
                        <TableRow
                          key={`${r.method}:${r.route}`}
                          className="border-border hover:bg-bg-hover cursor-pointer"
                          onClick={() => navigate(`/ops/errors?route=${encodeURIComponent(r.route)}`)}
                        >
                          <TableCell className="pl-6">
                            <span className="text-[11px] font-mono text-accent mr-2">{r.method}</span>
                            <span className="text-[13px] text-text-primary">{r.route}</span>
                          </TableCell>
                          <TableCell className="text-right text-[13px] font-mono text-text-secondary">{r.p50_ms.toFixed(0)}ms</TableCell>
                          <TableCell className="text-right text-[13px] font-mono text-text-secondary">{r.p95_ms.toFixed(0)}ms</TableCell>
                          <TableCell className="text-right text-[13px] font-mono text-text-secondary">{r.p99_ms.toFixed(0)}ms</TableCell>
                          <TableCell className="text-right pr-4">
                            <ErrorRateBadge rate={r.error_rate} />
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
              <CardHeader className="pb-3">
                <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
                  Hot Endpoints — By Call Volume
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow className="border-border hover:bg-transparent">
                      <TableHead className="text-[11px] text-text-tertiary pl-6">#</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary">Route</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Calls</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topRoutes.map((r: RouteMetric, i: number) => (
                      <TableRow key={`hot-${r.route}-${i}`} className="border-border hover:bg-bg-hover">
                        <TableCell className="pl-6 text-[11px] text-text-tertiary">{i + 1}</TableCell>
                        <TableCell className="text-[13px] text-text-primary font-mono">{r.route}</TableCell>
                        <TableCell className="text-right pr-4 text-[13px] font-mono text-text-secondary">{r.count.toLocaleString()}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          </div>

          <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
            <CardHeader className="pb-3">
              <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
                Runtime Snapshot
              </CardTitle>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={140}>
                <LineChart data={runtimeChartData}>
                  <XAxis dataKey="t" hide />
                  <YAxis hide />
                  <Tooltip
                    contentStyle={{ background: 'var(--color-bg-elevated)', border: '1px solid var(--color-border)', borderRadius: 8 }}
                    labelStyle={{ color: 'var(--color-text-secondary)' }}
                    itemStyle={{ color: 'var(--color-text-primary)' }}
                  />
                  <Legend iconType="circle" iconSize={8} />
                  <Line type="monotone" dataKey="goroutines" stroke="var(--color-accent)" strokeWidth={2} dot={false} name="Goroutines" />
                  <Line type="monotone" dataKey="mem_mb" stroke="var(--color-success)" strokeWidth={2} dot={false} name="Mem MB" />
                  <Line type="monotone" dataKey="gc_ms" stroke="var(--color-warning)" strokeWidth={2} dot={false} name="GC p99 ms" />
                </LineChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          {data.jobs.by_type.length > 0 && (
            <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
              <CardHeader className="pb-3">
                <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
                  Job Durations
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow className="border-border hover:bg-transparent">
                      <TableHead className="text-[11px] text-text-tertiary pl-6">Job Type</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">Runs</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">Success</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right">Failed</TableHead>
                      <TableHead className="text-[11px] text-text-tertiary text-right pr-4">p95 (s)</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.jobs.by_type.map((j) => (
                      <TableRow key={j.job_type} className="border-border hover:bg-bg-hover">
                        <TableCell className="pl-6 text-[13px] font-mono text-text-primary">{j.job_type}</TableCell>
                        <TableCell className="text-right text-[13px] text-text-secondary">{j.runs.toLocaleString()}</TableCell>
                        <TableCell className="text-right text-[13px] text-success">{j.success.toLocaleString()}</TableCell>
                        <TableCell className="text-right text-[13px] text-danger">{j.failed.toLocaleString()}</TableCell>
                        <TableCell className="text-right pr-4 text-[13px] font-mono text-text-secondary">{j.p95_s.toFixed(1)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  )
}
