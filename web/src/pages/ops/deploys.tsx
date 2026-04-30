import { Rocket, ExternalLink, Filter } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useDeployHistory } from '@/hooks/use-ops'
import { EntityLink } from '@/components/shared'
import { useState } from 'react'

const GIT_REPO_URL = import.meta.env.VITE_GIT_REPO_URL as string | undefined

function GitSHALink({ sha }: { sha?: string }) {
  if (!sha) return null
  const short = sha.slice(0, 8)
  if (GIT_REPO_URL) {
    return (
      <a
        href={`${GIT_REPO_URL}/commit/${sha}`}
        target="_blank"
        rel="noreferrer"
        className="flex items-center gap-1 text-accent hover:underline font-mono text-[12px]"
        onClick={(e) => e.stopPropagation()}
      >
        {short} <ExternalLink className="h-3 w-3" />
      </a>
    )
  }
  return <span className="font-mono text-[12px] text-text-secondary">{short}</span>
}

export default function DeployHistory() {
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')

  const { data, isLoading } = useDeployHistory({
    from: fromDate || undefined,
    to: toDate || undefined,
    limit: 50,
  })

  const entries: Array<Record<string, string>> = data?.data ?? []

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-64" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-2">
        <Rocket className="h-4 w-4 text-accent" />
        <h1 className="text-[15px] font-semibold text-text-primary">Deploy History</h1>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <Filter className="h-4 w-4 text-text-tertiary" />
        <Input
          type="datetime-local"
          placeholder="From"
          value={fromDate}
          onChange={(e) => setFromDate(e.target.value)}
          className="max-w-xs bg-bg-surface border-border text-text-primary text-[13px]"
        />
        <Input
          type="datetime-local"
          placeholder="To"
          value={toDate}
          onChange={(e) => setToDate(e.target.value)}
          className="max-w-xs bg-bg-surface border-border text-text-primary text-[13px]"
        />
      </div>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
            Deployment Audit Log
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="text-[11px] text-text-tertiary pl-6">Timestamp</TableHead>
                <TableHead className="text-[11px] text-text-tertiary">Git SHA</TableHead>
                <TableHead className="text-[11px] text-text-tertiary">Action</TableHead>
                <TableHead className="text-[11px] text-text-tertiary">Actor</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-[13px] text-text-tertiary py-8">
                    No deployment records found.
                  </TableCell>
                </TableRow>
              ) : (
                entries.map((entry, i) => (
                  <TableRow key={`deploy-${i}`} className="border-border hover:bg-bg-hover">
                    <TableCell className="pl-6 text-[12px] font-mono text-text-secondary">
                      {entry.created_at ? new Date(entry.created_at).toLocaleString('tr-TR') : '—'}
                    </TableCell>
                    <TableCell>
                      <GitSHALink sha={entry.entity_id} />
                    </TableCell>
                    <TableCell className="text-[12px] text-text-primary">{entry.action}</TableCell>
                    <TableCell className="text-[12px] text-text-secondary font-mono">{entry.user_id ? <EntityLink entityType="user" entityId={entry.user_id} truncate /> : '—'}</TableCell>
                    <TableCell className="text-right pr-4">
                      <Badge className="bg-success-dim text-success border-0 text-[10px]">deployed</Badge>
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
