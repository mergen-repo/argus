import { useState } from 'react'
import { RefreshCw, AlertCircle, Users, Wifi, Database, TrendingUp } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { useTenantResources } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

function SparkBar({ data }: { data: number[] }) {
  if (!data?.length) return <span className="text-text-tertiary text-xs">—</span>
  const max = Math.max(...data, 1)
  return (
    <div className="flex items-end gap-0.5 h-6">
      {data.map((v, i) => (
        <div
          key={i}
          className="w-1 rounded-sm bg-accent-primary/60"
          style={{ height: `${Math.round((v / max) * 24)}px` }}
        />
      ))}
    </div>
  )
}

function formatBytes(bytes: number) {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`
  return `${bytes} B`
}

export default function TenantResourcesPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [view, setView] = useState<'cards' | 'table'>('cards')

  const { data: tenants, isLoading, isError, refetch } = useTenantResources()

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

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Tenant Resource Dashboard</h1>
          <p className="text-sm text-text-secondary mt-0.5">Live resource usage across all tenants</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-lg border border-border overflow-hidden">
            {(['cards', 'table'] as const).map((v) => (
              <Button
                key={v}
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setView(v)}
                className={cn(
                  'rounded-none px-3 py-1.5 text-sm h-auto capitalize',
                  view === v
                    ? 'bg-accent-dim text-accent'
                    : 'text-text-secondary hover:text-text-primary'
                )}
              >
                {v}
              </Button>
            ))}
          </div>
          <Button variant="ghost" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load tenant resources.
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-40 rounded-xl" />
          ))}
        </div>
      ) : view === 'cards' ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {(tenants ?? []).map((t) => (
            <Card key={t.tenant_id} className="bg-bg-surface border-border">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-text-primary flex items-center justify-between">
                  {t.tenant_name}
                  <Badge variant="outline" className="text-xs font-mono">{t.tenant_id.slice(0, 8)}</Badge>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-2 text-xs">
                  <div className="flex items-center gap-1.5 text-text-secondary">
                    <Users className="h-3.5 w-3.5" />
                    <span>{formatNumber(t.sim_count)} SIMs</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-text-secondary">
                    <Wifi className="h-3.5 w-3.5" />
                    <span>{t.active_sessions} sessions</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-text-secondary">
                    <TrendingUp className="h-3.5 w-3.5" />
                    <span>{t.api_rps.toFixed(1)} rps</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-text-secondary">
                    <Database className="h-3.5 w-3.5" />
                    <span>{formatBytes(t.storage_bytes)}</span>
                  </div>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-tertiary">CDR 30d: {formatBytes(t.cdr_bytes_30d)}</span>
                  <SparkBar data={t.spark} />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (
        <Card className="bg-bg-surface border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tenant</TableHead>
                <TableHead className="text-right">SIMs</TableHead>
                <TableHead className="text-right">Sessions</TableHead>
                <TableHead className="text-right">API RPS</TableHead>
                <TableHead className="text-right">CDR 30d</TableHead>
                <TableHead className="text-right">Storage</TableHead>
                <TableHead>Trend</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(tenants ?? []).map((t) => (
                <TableRow key={t.tenant_id}>
                  <TableCell>
                    <div className="font-medium text-text-primary">{t.tenant_name}</div>
                    <div className="text-xs text-text-tertiary font-mono">{t.tenant_id.slice(0, 8)}</div>
                  </TableCell>
                  <TableCell className="text-right">{formatNumber(t.sim_count)}</TableCell>
                  <TableCell className="text-right">{t.active_sessions}</TableCell>
                  <TableCell className="text-right">{t.api_rps.toFixed(1)}</TableCell>
                  <TableCell className="text-right">{formatBytes(t.cdr_bytes_30d)}</TableCell>
                  <TableCell className="text-right">{formatBytes(t.storage_bytes)}</TableCell>
                  <TableCell><SparkBar data={t.spark} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </div>
  )
}
