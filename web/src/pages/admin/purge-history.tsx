import { useState } from 'react'
import { RefreshCw, AlertCircle } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { usePurgeHistory } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { EntityLink } from '@/components/shared/entity-link'
import type { PurgeHistoryFilters } from '@/types/admin'

export default function PurgeHistoryPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [filters, setFilters] = useState<PurgeHistoryFilters>({})

  const { data, isLoading, isError, refetch } = usePurgeHistory(filters)

  if (!isSuperAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">super_admin role required.</p>
        </div>
      </div>
    )
  }

  const items = data?.data ?? []

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Purge History</h1>
          <p className="text-sm text-text-secondary mt-0.5">SIM records that have been permanently purged</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load purge history.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ICCID</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>Tenant</TableHead>
                <TableHead>Purged At</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Actor</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-12 text-text-tertiary">
                    No purged SIM records found.
                  </TableCell>
                </TableRow>
              ) : (
                items.map((item) => (
                  <TableRow key={item.sim_id}>
                    <TableCell className="font-mono text-xs text-text-primary">{item.iccid}</TableCell>
                    <TableCell className="font-mono text-xs text-text-secondary">{item.msisdn}</TableCell>
                    <TableCell className="text-sm text-text-secondary">{item.tenant_name}</TableCell>
                    <TableCell className="text-xs text-text-tertiary">
                      {new Date(item.purged_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-sm text-text-secondary">{item.reason || '—'}</TableCell>
                    <TableCell className="font-mono text-xs text-text-tertiary">
                      {item.actor_id ? <EntityLink entityType="user" entityId={item.actor_id} label={item.actor_email || item.actor_name} /> : 'system'}
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
