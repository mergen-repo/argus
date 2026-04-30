import * as React from 'react'
import type { TooltipContentProps } from 'recharts'
import type { ValueType, NameType, Payload } from 'recharts/types/component/DefaultTooltipContent'
import { TwoWayTraffic } from '@/components/analytics/two-way-traffic'
import {
  formatBytes,
  formatNumber,
  formatTimestamp,
  formatDeltaPct,
  timeAgo,
} from '@/lib/format'
import type { TimeSeriesPoint, UsageMetric, UsageGroupBy } from '@/types/analytics'

interface UsageChartTooltipOwnProps {
  period: string
  metric: UsageMetric
  groupBy: UsageGroupBy
  allData: TimeSeriesPoint[]
  groupKeys: string[]
}

type UsageChartTooltipProps = TooltipContentProps<ValueType, NameType> & UsageChartTooltipOwnProps

export function UsageChartTooltip({
  active,
  payload,
  label,
  period,
  groupBy,
  allData,
}: UsageChartTooltipProps) {
  if (!active || !payload?.length || !label) return null

  const panelClass =
    'bg-bg-elevated border border-border rounded-md p-2 shadow-lg text-xs text-text-primary font-mono'

  const ts = String(label)

  // Two render paths:
  //  - Non-grouped: reads raw `data.time_series` via `allData` so we can show the
  //    full TimeSeriesPoint fields (bytes_in, bytes_out, sessions, auths, unique_sims)
  //    and compute delta vs previous bucket.
  //  - Grouped: reads the recharts payload directly because `chartData` is pivoted
  //    per-bucket with one key per group series; the raw bytes_in/bytes_out fields
  //    are not propagated into the pivoted bucket, so TwoWayTraffic only renders
  //    in the non-grouped branch.
  if (!groupBy) {
    const curr = allData.find((p) => p.ts === ts)
    if (!curr) return null

    const idx = allData.findIndex((p) => p.ts === ts)
    const prev = idx > 0 ? allData[idx - 1] : null
    const delta = prev ? formatDeltaPct(curr.total_bytes, prev.total_bytes) : null

    const toneClass =
      delta?.tone === 'positive'
        ? 'text-success'
        : delta?.tone === 'negative'
          ? 'text-danger'
          : 'text-text-tertiary'

    return (
      <div role="tooltip" className={panelClass}>
        <div className="mb-1 text-text-secondary">
          {formatTimestamp(ts, period)}{' '}
          <span className="text-text-tertiary">({timeAgo(ts)})</span>
        </div>
        <div className="flex items-center gap-1.5 mb-0.5">
          <TwoWayTraffic in={curr.bytes_in} out={curr.bytes_out} />
        </div>
        <div className="flex items-center justify-between gap-4 mb-0.5">
          <span className="text-text-secondary">Total</span>
          <span>{formatBytes(curr.total_bytes)}</span>
        </div>
        {delta && (
          <div className="flex items-center justify-between gap-4 mb-0.5">
            <span className="text-text-secondary">Δ prev</span>
            <span className={toneClass}>{delta.text}</span>
          </div>
        )}
        <div className="flex items-center justify-between gap-4 mb-0.5">
          <span className="text-text-secondary">Sessions</span>
          <span>{formatNumber(curr.sessions)}</span>
        </div>
        <div className="flex items-center justify-between gap-4 mb-0.5">
          <span className="text-text-secondary">Auths</span>
          <span>{formatNumber(curr.auths)}</span>
        </div>
        {curr.unique_sims > 0 && (
          <div className="flex items-center justify-between gap-4">
            <span className="text-text-secondary">Unique SIMs</span>
            <span>{formatNumber(curr.unique_sims)}</span>
          </div>
        )}
      </div>
    )
  }

  type Entry = Payload<ValueType, NameType>
  const typedPayload = payload as Entry[]

  const topEntry = typedPayload.reduce<{ name: string; value: number } | null>(
    (best, entry: Entry) => {
      const v = typeof entry.value === 'number' ? entry.value : 0
      if (!best || v > best.value) return { name: String(entry.name ?? ''), value: v }
      return best
    },
    null,
  )

  return (
    <div role="tooltip" className={panelClass}>
      <div className="mb-1 text-text-secondary">
        {formatTimestamp(ts, period)}{' '}
        <span className="text-text-tertiary">({timeAgo(ts)})</span>
      </div>
      {typedPayload.map((entry: Entry, i: number) => {
        const value = typeof entry.value === 'number' ? entry.value : 0
        return (
          <div key={i} className="flex items-center justify-between gap-4 mb-0.5">
            <span className="flex items-center gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full flex-shrink-0"
                style={{ backgroundColor: entry.color }}
              />
              <span className="text-text-secondary">{entry.name}</span>
            </span>
            <span>{formatBytes(value)}</span>
          </div>
        )
      })}
      {topEntry && (
        <div className="mt-1 pt-1 border-t border-border text-text-tertiary">
          Top: <span className="text-text-primary">{topEntry.name}</span>{' '}
          — {formatNumber(topEntry.value)}
        </div>
      )}
    </div>
  )
}
