import { useEffect, useState } from 'react'
import { Clock, Smartphone, ShieldCheck, RefreshCw } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { InfoRow } from '@/components/ui/info-row'
import { timeAgo } from '@/lib/format'
import {
  bindingStatusVariant,
  formatGraceCountdown,
  graceToneFor,
  BINDING_MODE_LABEL,
  BINDING_STATUS_LABEL,
  type DeviceBinding,
} from '@/types/device-binding'

interface BoundIMEIPanelProps {
  binding: DeviceBinding
  onRePair: () => void
  rePairPending?: boolean
}

// Recompute "now" every 60s so countdown text updates without a full refetch.
function useTickingNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return now
}

function GraceCountdownBadge({ expiresAt }: { expiresAt: string }) {
  const now = useTickingNow()
  const tone = graceToneFor(expiresAt, now)
  const label = formatGraceCountdown(expiresAt, now)

  const variant: 'warning' | 'danger' | 'secondary' =
    tone === 'urgent' ? 'danger' : tone === 'expired' ? 'secondary' : 'warning'

  return (
    <Badge variant={variant} className="gap-1.5 font-mono uppercase tracking-wider text-xs">
      <Clock className="h-3 w-3" />
      {label}
    </Badge>
  )
}

export function BoundIMEIPanel({ binding, onRePair, rePairPending }: BoundIMEIPanelProps) {
  const hasBinding = !!binding.bound_imei
  const showGrace =
    binding.binding_mode === 'grace-period' && !!binding.binding_grace_expires_at

  return (
    <Card
      className={
        'relative overflow-hidden ' +
        "before:content-[''] before:absolute before:left-0 before:top-0 before:bottom-0 before:w-[3px] " +
        (hasBinding ? 'before:bg-accent' : 'before:bg-border')
      }
    >
      <CardContent className="p-4 space-y-4">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              {hasBinding ? (
                <ShieldCheck className="h-4 w-4 text-accent" />
              ) : (
                <Smartphone className="h-4 w-4 text-text-tertiary" />
              )}
            </span>
            <div>
              <h2 className="text-sm font-semibold text-text-primary">Bound IMEI</h2>
              <p className="text-xs text-text-tertiary font-mono">
                Device pairing for this SIM
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {showGrace && binding.binding_grace_expires_at && (
              <GraceCountdownBadge expiresAt={binding.binding_grace_expires_at} />
            )}
            {hasBinding && (
              <Button
                variant="outline"
                size="sm"
                onClick={onRePair}
                disabled={rePairPending}
                className="gap-1.5"
                aria-label="Re-pair this SIM"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                Re-pair
              </Button>
            )}
          </div>
        </div>

        {!hasBinding ? (
          <div className="flex flex-col items-center justify-center gap-3 py-8 text-center border border-dashed border-border rounded-[var(--radius-sm)] bg-bg-surface">
            <span className="flex h-10 w-10 items-center justify-center rounded-full bg-bg-elevated border border-border">
              <Smartphone className="h-5 w-5 text-text-tertiary" />
            </span>
            <div className="max-w-md">
              <p className="text-sm font-semibold text-text-primary">No device bound yet</p>
              <p className="mt-1 text-xs text-text-secondary">
                The first IMEI observed will be locked when binding mode is{' '}
                <span className="font-mono text-text-primary">first-use</span>.
              </p>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-3 pt-1">
            <InfoRow
              label="IMEI"
              value={
                <span className="font-mono text-sm text-text-primary tracking-wider">
                  {binding.bound_imei}
                </span>
              }
            />
            <InfoRow
              label="Binding Mode"
              value={
                binding.binding_mode ? (
                  <Badge variant="outline" className="font-mono uppercase tracking-wider text-xs">
                    {BINDING_MODE_LABEL[binding.binding_mode]}
                  </Badge>
                ) : (
                  <span className="text-text-tertiary">—</span>
                )
              }
            />
            <InfoRow
              label="Status"
              value={
                binding.binding_status ? (
                  <Badge
                    variant={bindingStatusVariant(binding.binding_status)}
                    className="gap-1 uppercase tracking-wider text-xs"
                  >
                    {binding.binding_status === 'verified' && (
                      <span className="h-1.5 w-1.5 rounded-full bg-current" />
                    )}
                    {BINDING_STATUS_LABEL[binding.binding_status]}
                  </Badge>
                ) : (
                  <span className="text-text-tertiary">—</span>
                )
              }
            />
            <InfoRow
              label="Verified"
              value={
                binding.binding_verified_at ? (
                  <span
                    className="text-text-secondary"
                    title={new Date(binding.binding_verified_at).toLocaleString()}
                  >
                    {timeAgo(binding.binding_verified_at)}
                  </span>
                ) : (
                  <span className="text-text-tertiary">—</span>
                )
              }
            />
            <InfoRow
              label="Last IMEI Seen"
              value={
                binding.last_imei_seen_at ? (
                  <span
                    className="text-text-secondary"
                    title={new Date(binding.last_imei_seen_at).toLocaleString()}
                  >
                    {timeAgo(binding.last_imei_seen_at)}
                  </span>
                ) : (
                  <span className="text-text-tertiary">—</span>
                )
              }
            />
            <InfoRow
              label="Observations"
              value={
                <span className="font-mono text-text-primary">
                  {binding.history_count.toLocaleString()}
                </span>
              }
            />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
