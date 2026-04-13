import { useState } from 'react'
import { RefreshCw, AlertCircle, Clock } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { useDSARQueue } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import type { DSARFilters } from '@/types/admin'

const STATUS_OPTIONS = [
  { value: '', label: 'All' },
  { value: 'received', label: 'Received' },
  { value: 'processing', label: 'Processing' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
]

function statusVariant(status: string): 'default' | 'success' | 'warning' | 'danger' {
  switch (status) {
    case 'completed': return 'success'
    case 'processing': return 'warning'
    case 'failed': return 'danger'
    default: return 'default'
  }
}

function typeLabel(type: string) {
  switch (type) {
    case 'data_portability_export': return 'Data Export'
    case 'kvkk_purge_daily': return 'KVKK Purge'
    case 'sim_erasure': return 'SIM Erasure'
    default: return type
  }
}

function SLATimer({ remaining, total }: { remaining: number; total: number }) {
  const pct = total > 0 ? (remaining / total) * 100 : 0
  const color = pct < 20 ? 'text-danger' : pct < 50 ? 'text-warning' : 'text-text-secondary'
  return (
    <div className={cn('flex items-center gap-1 text-xs', color)}>
      <Clock className="h-3 w-3" />
      <span>{remaining.toFixed(0)}h remaining</span>
    </div>
  )
}

export default function DSARQueuePage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const isTenantAdmin = user?.role === 'tenant_admin'
  const [filters, setFilters] = useState<DSARFilters>({})
  const [activeStatus, setActiveStatus] = useState('')

  const effectiveFilters = { ...filters, status: activeStatus || undefined }
  const { data, isLoading, isError, refetch } = useDSARQueue(effectiveFilters)

  if (!isSuperAdmin && !isTenantAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">Insufficient permissions.</p>
        </div>
      </div>
    )
  }

  const items = data?.data ?? []

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">DSAR Queue</h1>
          <p className="text-sm text-text-secondary mt-0.5">Data Subject Access Request processing queue</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex gap-1">
        {STATUS_OPTIONS.map((opt) => (
          <Button
            key={opt.value}
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              setActiveStatus(opt.value)
              setFilters({})
            }}
            className={cn(
              'rounded-full px-3 py-1 text-xs border h-auto transition-colors',
              activeStatus === opt.value
                ? 'bg-accent-dim text-accent border-accent/40'
                : 'border-border text-text-secondary hover:text-text-primary'
            )}
          >
            {opt.label}
          </Button>
        ))}
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load DSAR queue.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Job ID</TableHead>
                <TableHead>Type</TableHead>
                {isSuperAdmin && <TableHead>Tenant</TableHead>}
                <TableHead>Status</TableHead>
                <TableHead>SLA</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={isSuperAdmin ? 6 : 5} className="text-center py-12 text-text-tertiary">
                    No DSAR requests found.
                  </TableCell>
                </TableRow>
              ) : (
                items.map((item) => (
                  <TableRow key={item.job_id}>
                    <TableCell className="font-mono text-xs text-text-secondary">
                      {item.job_id.slice(0, 8)}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-text-primary">{typeLabel(item.type)}</span>
                    </TableCell>
                    {isSuperAdmin && (
                      <TableCell className="text-text-secondary text-sm">{item.tenant_id.slice(0, 8)}</TableCell>
                    )}
                    <TableCell>
                      <Badge variant={statusVariant(item.status)}>{item.status}</Badge>
                    </TableCell>
                    <TableCell>
                      <SLATimer remaining={item.sla_remaining_hours} total={item.sla_hours} />
                    </TableCell>
                    <TableCell className="text-xs text-text-tertiary">
                      {new Date(item.created_at).toLocaleDateString()}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </Card>

      {data?.meta?.has_more && (
        <div className="flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setFilters((f) => ({ ...f, cursor: data.meta!.cursor }))}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
