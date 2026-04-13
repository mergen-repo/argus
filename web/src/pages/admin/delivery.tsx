import { useState } from 'react'
import { RefreshCw, AlertCircle, CheckCircle2, AlertTriangle, XCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeliveryStatus } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import type { ChannelHealth } from '@/types/admin'

const WINDOW_OPTIONS = [
  { value: '1h', label: 'Last 1h' },
  { value: '24h', label: 'Last 24h' },
  { value: '7d', label: 'Last 7d' },
] as const

const CHANNELS = [
  { key: 'webhook', label: 'Webhook' },
  { key: 'email', label: 'Email' },
  { key: 'sms', label: 'SMS' },
  { key: 'in_app', label: 'In-App' },
  { key: 'telegram', label: 'Telegram' },
] as const

function HealthIcon({ health }: { health: string }) {
  if (health === 'green') return <CheckCircle2 className="h-5 w-5 text-success" />
  if (health === 'yellow') return <AlertTriangle className="h-5 w-5 text-warning" />
  return <XCircle className="h-5 w-5 text-danger" />
}

function healthBadge(health: string) {
  if (health === 'green') return <Badge variant="success">Healthy</Badge>
  if (health === 'yellow') return <Badge variant="warning">Degraded</Badge>
  return <Badge variant="danger">Unhealthy</Badge>
}

function ChannelCard({ label, ch }: { label: string; ch: ChannelHealth }) {
  return (
    <Card className="bg-bg-surface border-border">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium flex items-center justify-between">
          <div className="flex items-center gap-2">
            <HealthIcon health={ch.health} />
            <span className="text-text-primary">{label}</span>
          </div>
          {healthBadge(ch.health)}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
          <div className="text-text-secondary">Success rate</div>
          <div className="text-right font-medium text-text-primary">
            {(ch.success_rate * 100).toFixed(1)}%
          </div>
          <div className="text-text-secondary">Failure rate</div>
          <div className={cn('text-right font-medium', ch.failure_rate > 0.05 ? 'text-danger' : 'text-text-primary')}>
            {(ch.failure_rate * 100).toFixed(1)}%
          </div>
          <div className="text-text-secondary">Retry depth</div>
          <div className={cn('text-right font-medium', ch.retry_depth > 100 ? 'text-warning' : 'text-text-primary')}>
            {ch.retry_depth}
          </div>
          <div className="text-text-secondary">P50 latency</div>
          <div className="text-right text-text-primary">{ch.p50_ms}ms</div>
          <div className="text-text-secondary">P95 latency</div>
          <div className="text-right text-text-primary">{ch.p95_ms}ms</div>
          <div className="text-text-secondary">P99 latency</div>
          <div className="text-right text-text-primary">{ch.p99_ms}ms</div>
          {ch.last_delivery_at && (
            <>
              <div className="text-text-secondary">Last delivery</div>
              <div className="text-right text-text-tertiary">
                {new Date(ch.last_delivery_at).toLocaleTimeString()}
              </div>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

export default function DeliveryStatusPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [window, setWindow] = useState<'1h' | '24h' | '7d'>('24h')

  const { data: status, isLoading, isError, refetch } = useDeliveryStatus(window)

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
          <h1 className="text-xl font-semibold text-text-primary">Delivery Channel Status</h1>
          <p className="text-sm text-text-secondary mt-0.5">Notification delivery health per channel</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-lg border border-border overflow-hidden">
            {WINDOW_OPTIONS.map((opt) => (
              <Button
                key={opt.value}
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setWindow(opt.value)}
                className={cn(
                  'rounded-none px-3 py-1.5 text-xs h-auto',
                  window === opt.value
                    ? 'bg-accent-dim text-accent'
                    : 'text-text-secondary hover:text-text-primary'
                )}
              >
                {opt.label}
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
          Failed to load delivery status.
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-48 rounded-xl" />
          ))}
        </div>
      ) : status ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {CHANNELS.map(({ key, label }) => (
            <ChannelCard
              key={key}
              label={label}
              ch={status[key as keyof typeof status]}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}
