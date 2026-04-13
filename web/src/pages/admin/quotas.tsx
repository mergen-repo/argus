import { RefreshCw, AlertCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useTenantQuotas } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import type { QuotaMetric } from '@/types/admin'

function QuotaBar({ metric, label }: { metric: QuotaMetric; label: string }) {
  const color =
    metric.status === 'danger'
      ? 'bg-danger'
      : metric.status === 'warning'
      ? 'bg-warning'
      : 'bg-accent-primary'

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="text-text-secondary">{label}</span>
        <span className={cn('font-medium', metric.status === 'danger' ? 'text-danger' : metric.status === 'warning' ? 'text-warning' : 'text-text-primary')}>
          {metric.pct.toFixed(0)}%
        </span>
      </div>
      <div className="h-2 rounded-full bg-bg-muted overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all', color)}
          style={{ width: `${Math.min(metric.pct, 100)}%` }}
        />
      </div>
      <div className="text-[10px] text-text-tertiary">
        {metric.current.toLocaleString()} / {metric.max.toLocaleString()}
      </div>
    </div>
  )
}

function statusBadge(status: string) {
  if (status === 'danger') return <Badge variant="danger" className="text-xs">Over limit</Badge>
  if (status === 'warning') return <Badge variant="warning" className="text-xs">Near limit</Badge>
  return <Badge variant="success" className="text-xs">OK</Badge>
}

function worstStatus(metrics: QuotaMetric[]): string {
  if (metrics.some((m) => m.status === 'danger')) return 'danger'
  if (metrics.some((m) => m.status === 'warning')) return 'warning'
  return 'ok'
}

export default function QuotasPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: quotas, isLoading, isError, refetch } = useTenantQuotas()

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
          <h1 className="text-xl font-semibold text-text-primary">Quota Breakdown</h1>
          <p className="text-sm text-text-secondary mt-0.5">Per-tenant resource quota utilization</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load quota data.
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-52 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {(quotas ?? []).map((q) => {
            const metrics = [q.sims, q.api_rps, q.sessions, q.storage_bytes]
            const overall = worstStatus(metrics)
            return (
              <Card key={q.tenant_id} className="bg-bg-surface border-border">
                <CardHeader className="pb-2">
                  <CardTitle className="text-sm font-medium flex items-center justify-between">
                    <span className="text-text-primary">{q.tenant_name}</span>
                    {statusBadge(overall)}
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <QuotaBar metric={q.sims} label="SIMs" />
                  <QuotaBar metric={q.api_rps} label="API RPS" />
                  <QuotaBar metric={q.sessions} label="Sessions" />
                  <QuotaBar metric={q.storage_bytes} label="Storage" />
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
