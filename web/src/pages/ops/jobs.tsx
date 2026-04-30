import { ListTodo, AlertCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useOpsSnapshot } from '@/hooks/use-ops'
import { useNavigate } from 'react-router-dom'

export default function JobQueueObs() {
  const navigate = useNavigate()
  const { data, isLoading } = useOpsSnapshot(15_000)

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <div className="grid grid-cols-3 gap-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
        </div>
        <Skeleton className="h-64" />
      </div>
    )
  }

  const jobs = data?.jobs.by_type ?? []
  const totalRuns = jobs.reduce((sum, j) => sum + j.runs, 0)
  const totalFailed = jobs.reduce((sum, j) => sum + j.failed, 0)
  const totalSuccess = jobs.reduce((sum, j) => sum + j.success, 0)

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-2">
        <ListTodo className="h-4 w-4 text-accent" />
        <h1 className="text-[15px] font-semibold text-text-primary">Job Queue Observability</h1>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {[
          { label: 'Total Runs', value: totalRuns.toLocaleString(), color: 'text-text-primary' },
          { label: 'Successful', value: totalSuccess.toLocaleString(), color: 'text-success' },
          { label: 'Failed', value: totalFailed.toLocaleString(), color: totalFailed > 0 ? 'text-danger' : 'text-text-secondary' },
        ].map(({ label, value, color }) => (
          <Card key={label} className="bg-bg-surface border-border rounded-[10px] shadow-card">
            <CardContent className="p-6">
              <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-1">{label}</div>
              <div className={`text-[28px] font-mono font-bold ${color}`}>{value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
            Job Types
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {jobs.length === 0 ? (
            <div className="p-6 text-center">
              <AlertCircle className="h-8 w-8 text-text-tertiary mx-auto mb-2" />
              <p className="text-[13px] text-text-secondary">No job data available yet.</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="border-border hover:bg-transparent">
                  <TableHead className="text-[11px] text-text-tertiary pl-6">Type</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">Runs</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">Success%</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">p50 (s)</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">p95 (s)</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right pr-4">p99 (s)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {jobs.map((j) => {
                  const successPct = j.runs > 0 ? (j.success / j.runs) * 100 : 0
                  return (
                    <TableRow
                      key={j.job_type}
                      className="border-border hover:bg-bg-hover cursor-pointer"
                      onClick={() => navigate(`/jobs?type=${j.job_type}`)}
                    >
                      <TableCell className="pl-6 text-[13px] font-mono text-text-primary">{j.job_type}</TableCell>
                      <TableCell className="text-right text-[13px] text-text-secondary">{j.runs.toLocaleString()}</TableCell>
                      <TableCell className="text-right">
                        <Badge className={successPct >= 99 ? 'bg-success-dim text-success border-0' : successPct >= 95 ? 'bg-warning-dim text-warning border-0' : 'bg-danger-dim text-danger border-0'}>
                          {successPct.toFixed(1)}%
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right text-[13px] font-mono text-text-secondary">{j.p50_s.toFixed(1)}</TableCell>
                      <TableCell className="text-right text-[13px] font-mono text-text-secondary">{j.p95_s.toFixed(1)}</TableCell>
                      <TableCell className="text-right pr-4 text-[13px] font-mono text-text-secondary">{j.p99_s.toFixed(1)}</TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
