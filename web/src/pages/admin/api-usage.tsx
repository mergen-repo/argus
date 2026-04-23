import { useState } from 'react'
import { RefreshCw, AlertCircle, AlertTriangle } from 'lucide-react'
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
import { TimeframeSelector } from '@/components/ui/timeframe-selector'
import { useAPIKeyUsage } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import { EntityLink } from '@/components/shared/entity-link'

const WINDOW_PRESETS = [
  { value: '1h' as const, label: 'Last 1h' },
  { value: '24h' as const, label: 'Last 24h' },
  { value: '7d' as const, label: 'Last 7d' },
  { value: '30d' as const, label: 'Last 30d' },
]

function ConsumptionBar({ pct }: { pct: number }) {
  const color =
    pct >= 95 ? 'bg-danger' : pct >= 80 ? 'bg-warning' : 'bg-accent-primary'
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-24 rounded-full bg-bg-muted overflow-hidden">
        <div
          className={cn('h-full rounded-full', color)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <span className="text-xs text-text-secondary">{pct.toFixed(0)}%</span>
    </div>
  )
}

export default function APIUsagePage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [window, setWindow] = useState<'1h' | '24h' | '7d' | '30d'>('24h')

  const { data: keys, isLoading, isError, refetch } = useAPIKeyUsage(window)

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

  const anomalies = (keys ?? []).filter((k) => k.anomaly).length

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">API Key Usage</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Rate limit consumption and anomaly detection
            {anomalies > 0 && (
              <Badge variant="warning" className="ml-2">{anomalies} anomaly</Badge>
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <TimeframeSelector
            value={window}
            onChange={(v) => setWindow((typeof v === 'string' ? v : v.value) as '1h' | '24h' | '7d' | '30d')}
            options={WINDOW_PRESETS}
            allowCustom={false}
          />
          <Button variant="ghost" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load API key usage.
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
                <TableHead>Key</TableHead>
                <TableHead>Tenant</TableHead>
                <TableHead className="text-right">Requests</TableHead>
                <TableHead>Consumption</TableHead>
                <TableHead className="text-right">Error Rate</TableHead>
                <TableHead>Anomaly</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(keys ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-12 text-text-tertiary">
                    No API key usage data found.
                  </TableCell>
                </TableRow>
              ) : (
                (keys ?? []).map((k) => (
                  <TableRow key={k.key_id}>
                    <TableCell>
                      <div className="font-medium text-text-primary">{k.key_name}</div>
                      <EntityLink entityType="apikey" entityId={k.key_id} label={k.key_name} />
                    </TableCell>
                    <TableCell className="text-text-secondary">{k.tenant_name}</TableCell>
                    <TableCell className="text-right font-mono text-sm">
                      {k.requests.toLocaleString()}
                    </TableCell>
                    <TableCell><ConsumptionBar pct={k.consumption_pct} /></TableCell>
                    <TableCell className="text-right">
                      <span className={cn('text-sm', k.error_rate > 0.05 ? 'text-danger font-semibold' : 'text-text-secondary')}>
                        {(k.error_rate * 100).toFixed(1)}%
                      </span>
                    </TableCell>
                    <TableCell>
                      {k.anomaly ? (
                        <div className="flex items-center gap-1 text-warning text-xs">
                          <AlertTriangle className="h-3.5 w-3.5" />
                          <span>Anomaly</span>
                        </div>
                      ) : (
                        <span className="text-xs text-text-tertiary">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  )
}
