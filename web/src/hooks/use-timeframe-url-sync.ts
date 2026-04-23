import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { TimeframePreset } from '@/components/ui/timeframe-selector'

const VALID_PRESETS: TimeframePreset[] = ['15m', '1h', '6h', '24h', '7d', '30d', 'custom']

function isValidPreset(v: string): v is TimeframePreset {
  return VALID_PRESETS.includes(v as TimeframePreset)
}

// Reject non-date-parseable strings so a crafted URL cannot inject arbitrary
// values into `filters.from/to` via the hook-owned tf_start/tf_end params.
// Backend is the authoritative validator; this is defensive hygiene.
function isValidISODate(v: string): boolean {
  if (!v) return false
  const d = new Date(v)
  return !isNaN(d.getTime())
}

export function useTimeframeUrlSync(defaultValue: TimeframePreset = '24h') {
  const [searchParams, setSearchParams] = useSearchParams()

  const rawTf = searchParams.get('tf')
  const timeframe: TimeframePreset = rawTf && isValidPreset(rawTf) ? rawTf : defaultValue

  const tfStart = searchParams.get('tf_start')
  const tfEnd = searchParams.get('tf_end')
  const customRange: { start: string; end: string } | null =
    timeframe === 'custom' && tfStart && tfEnd && isValidISODate(tfStart) && isValidISODate(tfEnd)
      ? { start: tfStart, end: tfEnd }
      : null

  const setTimeframe = useCallback(
    (t: TimeframePreset) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.set('tf', t)
          if (t !== 'custom') {
            next.delete('tf_start')
            next.delete('tf_end')
          }
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setCustomRange = useCallback(
    (r: { start: string; end: string } | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (r) {
            next.set('tf', 'custom')
            next.set('tf_start', r.start)
            next.set('tf_end', r.end)
          } else {
            next.delete('tf_start')
            next.delete('tf_end')
            if (next.get('tf') === 'custom') next.set('tf', defaultValue)
          }
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams, defaultValue],
  )

  return { timeframe, setTimeframe, customRange, setCustomRange }
}
