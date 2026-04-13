import { AlertTriangle, Filter } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { useOpsSnapshot } from '@/hooks/use-ops'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { useMemo, useState } from 'react'
import type { RouteMetric } from '@/types/ops'

export default function ErrorDrilldown() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [routeFilter, setRouteFilter] = useState(searchParams.get('route') ?? '')

  const { data, isLoading } = useOpsSnapshot(15_000)

  const errorRoutes = useMemo(() => {
    const routes = data?.http.by_route ?? []
    return routes.filter((r: RouteMetric) => {
      const matchesRoute = !routeFilter || r.route.toLowerCase().includes(routeFilter.toLowerCase())
      return r.error_count > 0 && matchesRoute
    })
  }, [data, routeFilter])

  const chartData = useMemo(() => {
    const byStatus = data?.http.by_status ?? []
    const row4xx = byStatus.filter((s) => s.status.startsWith('4')).reduce((acc, s) => acc + s.count, 0)
    const row5xx = byStatus.filter((s) => s.status.startsWith('5')).reduce((acc, s) => acc + s.count, 0)
    return [{ name: 'current', '4xx': row4xx, '5xx': row5xx }]
  }, [data])

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-danger" />
        <h1 className="text-[15px] font-semibold text-text-primary">Error Drill-down</h1>
      </div>

      <div className="flex items-center gap-3">
        <Filter className="h-4 w-4 text-text-tertiary" />
        <Input
          placeholder="Filter by route..."
          value={routeFilter}
          onChange={(e) => setRouteFilter(e.target.value)}
          className="max-w-xs bg-bg-surface border-border text-text-primary placeholder:text-text-tertiary"
        />
      </div>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
            Error Volume by Status Code
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ResponsiveContainer width="100%" height={160}>
            <AreaChart data={chartData}>
              <XAxis dataKey="name" hide />
              <YAxis hide />
              <Tooltip
                contentStyle={{ background: 'var(--color-bg-elevated)', border: '1px solid var(--color-border)', borderRadius: 8 }}
                labelStyle={{ color: 'var(--color-text-secondary)' }}
                itemStyle={{ color: 'var(--color-text-primary)' }}
              />
              <Legend iconType="circle" iconSize={8} />
              <Area type="monotone" dataKey="4xx" stackId="1" stroke="var(--color-warning)" fill="var(--color-warning)" fillOpacity={0.15} name="4xx" />
              <Area type="monotone" dataKey="5xx" stackId="1" stroke="var(--color-danger)" fill="var(--color-danger)" fillOpacity={0.2} name="5xx" />
            </AreaChart>
          </ResponsiveContainer>
        </CardContent>
      </Card>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
            Error Routes
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="text-[11px] text-text-tertiary pl-6">Route</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right">Errors</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right">Total</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Rate</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {errorRoutes.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-[13px] text-text-tertiary py-8">
                    No errors detected — everything looks healthy.
                  </TableCell>
                </TableRow>
              ) : (
                errorRoutes.map((r: RouteMetric) => (
                  <TableRow
                    key={`${r.method}:${r.route}`}
                    className="border-border hover:bg-bg-hover cursor-pointer"
                    onClick={() => navigate(`/audit?action=http_5xx&route=${encodeURIComponent(r.route)}`)}
                  >
                    <TableCell className="pl-6">
                      <span className="text-[11px] font-mono text-accent mr-2">{r.method}</span>
                      <span className="text-[13px] text-text-primary">{r.route}</span>
                    </TableCell>
                    <TableCell className="text-right text-[13px] text-danger font-mono">{r.error_count.toLocaleString()}</TableCell>
                    <TableCell className="text-right text-[13px] text-text-secondary font-mono">{r.count.toLocaleString()}</TableCell>
                    <TableCell className="text-right pr-4">
                      <Badge className="bg-danger-dim text-danger border-0">{(r.error_rate * 100).toFixed(2)}%</Badge>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
