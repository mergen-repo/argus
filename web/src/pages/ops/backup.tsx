import { Archive, CheckCircle2, XCircle, AlertCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useBackupStatus } from '@/hooks/use-settings'

function StatusIcon({ status }: { status: string }) {
  if (status === 'completed' || status === 'success') return <CheckCircle2 className="h-4 w-4 text-success" />
  if (status === 'failed' || status === 'error') return <XCircle className="h-4 w-4 text-danger" />
  return <AlertCircle className="h-4 w-4 text-warning" />
}

function StateBadge({ state }: { state: string }) {
  if (state === 'completed' || state === 'success') return <Badge className="bg-success-dim text-success border-0">{state}</Badge>
  if (state === 'failed' || state === 'error') return <Badge className="bg-danger-dim text-danger border-0">{state}</Badge>
  return <Badge className="bg-warning-dim text-warning border-0">{state}</Badge>
}

export default function BackupStatus() {
  const { data, isLoading } = useBackupStatus()

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <div className="grid grid-cols-3 gap-4">
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
        </div>
        <Skeleton className="h-64" />
      </div>
    )
  }

  if (!data) {
    return (
      <div className="p-6 text-center">
        <Archive className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
        <p className="text-[14px] text-text-secondary">No backup data available.</p>
      </div>
    )
  }

  const summaries = [
    { label: 'Last Daily', backup: data.last_daily },
    { label: 'Last Weekly', backup: data.last_weekly },
    { label: 'Last Monthly', backup: data.last_monthly },
  ]

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-2">
        <Archive className="h-4 w-4 text-accent" />
        <h1 className="text-[15px] font-semibold text-text-primary">Backup & Restore Status</h1>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {summaries.map(({ label, backup }) => (
          <Card key={label} className="bg-bg-surface border-border rounded-[10px] shadow-card">
            <CardHeader className="pb-2">
              <CardTitle className="text-[12px] text-text-tertiary uppercase tracking-[1px] flex items-center gap-2">
                {backup && <StatusIcon status={backup.status} />}
                {label}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {!backup ? (
                <p className="text-[13px] text-text-tertiary">No data</p>
              ) : (
                <div className="space-y-1.5">
                  <div className="text-[13px]">
                    <StateBadge state={backup.status} />
                  </div>
                  <div className="text-[12px] font-mono text-text-secondary">
                    {new Date(backup.started_at).toLocaleString('tr-TR')}
                  </div>
                  <div className="text-[12px] text-text-tertiary">
                    {backup.size_mb.toFixed(1)} MB
                  </div>
                  {backup.sha256 && (
                    <div className="text-[11px] font-mono text-text-tertiary truncate" title={backup.sha256}>
                      sha256: {backup.sha256.slice(0, 16)}…
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>

      {data.last_verify && (
        <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
          <CardHeader className="pb-2">
            <CardTitle className="text-[12px] text-text-tertiary uppercase tracking-[1px]">Last Verification</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-4 text-[13px]">
              <StateBadge state={data.last_verify.status} />
              <span className="text-text-secondary font-mono">{new Date(data.last_verify.verified_at).toLocaleString('tr-TR')}</span>
              <span className="text-text-tertiary">Tenants: {data.last_verify.tenants_count}</span>
              <span className="text-text-tertiary">SIMs: {data.last_verify.sims_count.toLocaleString()}</span>
            </div>
          </CardContent>
        </Card>
      )}

      {data.history && data.history.length > 0 && (
        <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
          <CardHeader className="pb-3">
            <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
              Backup History (Last 30)
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow className="border-border hover:bg-transparent">
                  <TableHead className="text-[11px] text-text-tertiary pl-6">Kind</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary">Started</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">Size</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right">SHA256</TableHead>
                  <TableHead className="text-[11px] text-text-tertiary text-right pr-4">State</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.history.slice(0, 30).map((b, i) => (
                  <TableRow key={i} className="border-border hover:bg-bg-hover">
                    <TableCell className="pl-6">
                      <Badge className="bg-bg-elevated text-text-secondary border border-border text-[10px]">{b.kind}</Badge>
                    </TableCell>
                    <TableCell className="text-[12px] font-mono text-text-secondary">
                      {new Date(b.started_at).toLocaleString('tr-TR')}
                    </TableCell>
                    <TableCell className="text-right text-[12px] font-mono text-text-secondary">{b.size_mb.toFixed(1)} MB</TableCell>
                    <TableCell className="text-right text-[11px] font-mono text-text-tertiary">{b.sha256?.slice(0, 12)}…</TableCell>
                    <TableCell className="text-right pr-4">
                      <StateBadge state={b.status} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
