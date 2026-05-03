import * as React from 'react'
import { Activity, AlertTriangle, BellRing, Clock } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { EmptyState } from '@/components/shared/empty-state'
import { cn } from '@/lib/utils'
import type { IMEILookupHistoryEntry } from '@/types/imei-lookup'

function formatTimestamp(value?: string): string {
  if (!value) return '—'
  try {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return date.toLocaleString(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
  } catch {
    return value
  }
}

function protocolBadgeVariant(protocol?: string): 'default' | 'secondary' {
  if (!protocol) return 'secondary'
  return 'default'
}

interface HistorySectionProps {
  history: IMEILookupHistoryEntry[]
  /**
   * Limit the displayed entries (e.g., last 30 days, 50 rows max from server).
   * Defaults to 30.
   */
  limit?: number
}

export const HistorySection = React.memo(function HistorySection({
  history,
  limit = 30,
}: HistorySectionProps) {
  const visible = React.useMemo(() => history.slice(0, limit), [history, limit])

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Activity className="h-3.5 w-3.5 text-text-tertiary" />
            <CardTitle className="text-sm">Recent Observations</CardTitle>
          </div>
          {history.length > 0 && (
            <span className="text-[10px] uppercase tracking-wider text-text-tertiary">
              {history.length} {history.length === 1 ? 'event' : 'events'}
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        {visible.length === 0 ? (
          <EmptyState
            icon={Clock}
            title="No history"
            description="No IMEI observations recorded for this device in the last 30 days."
          />
        ) : (
          <ul className="relative space-y-2">
            <span
              aria-hidden="true"
              className="absolute left-[7px] top-1 bottom-1 w-px bg-border-subtle"
            />
            {visible.map((entry, idx) => (
              <li
                key={entry.id ?? `${entry.observed_at}-${idx}`}
                className="relative flex items-start gap-3 rounded-[var(--radius-sm)] px-3 py-2 hover:bg-bg-hover transition-colors"
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    'mt-1.5 h-2 w-2 shrink-0 rounded-full ring-2 ring-bg-surface',
                    entry.was_mismatch
                      ? 'bg-danger'
                      : entry.alarm_raised
                        ? 'bg-warning'
                        : 'bg-accent',
                  )}
                />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-mono text-xs text-text-primary">
                      {formatTimestamp(entry.observed_at)}
                    </span>
                    {entry.capture_protocol && (
                      <Badge
                        variant={protocolBadgeVariant(entry.capture_protocol)}
                        className="text-[10px] uppercase tracking-wider"
                      >
                        {entry.capture_protocol}
                      </Badge>
                    )}
                    {entry.was_mismatch && (
                      <Badge variant="danger" className="text-[10px] gap-1">
                        <AlertTriangle className="h-3 w-3" />
                        Mismatch
                      </Badge>
                    )}
                    {entry.alarm_raised && (
                      <Badge variant="warning" className="text-[10px] gap-1">
                        <BellRing className="h-3 w-3" />
                        Alarm
                      </Badge>
                    )}
                  </div>
                  {entry.observed_imei && (
                    <p className="mt-0.5 font-mono text-[11px] text-text-tertiary truncate">
                      Observed IMEI: {entry.observed_imei}
                    </p>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
})
