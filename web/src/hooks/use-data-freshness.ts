import { useState, useEffect, useCallback } from 'react'

type FreshnessSource = 'ws' | 'poll'
type Indicator = 'live' | 'stale' | 'offline'

const STALE_THRESHOLD_MS = 5 * 60 * 1000

const AUTO_REFRESH_OPTIONS = [15, 30, 60, 0] as const
export type AutoRefreshOption = (typeof AUTO_REFRESH_OPTIONS)[number]

interface DataFreshnessOptions {
  source: FreshnessSource
  lastUpdated?: Date | null
  refetch?: () => void
  wsConnected?: boolean
  pageKey?: string
}

export function useDataFreshness({
  source,
  lastUpdated,
  refetch,
  wsConnected = true,
  pageKey,
}: DataFreshnessOptions) {
  const storageKey = pageKey ? `argus:auto-refresh:${pageKey}` : null
  const stored = storageKey ? (Number(localStorage.getItem(storageKey)) || 30) : 30
  const [autoRefresh, setAutoRefreshState] = useState<AutoRefreshOption>(stored as AutoRefreshOption)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    if (source !== 'poll' || autoRefresh === 0 || !refetch) return
    const t = setInterval(refetch, autoRefresh * 1000)
    return () => clearInterval(t)
  }, [source, autoRefresh, refetch])

  const setAutoRefresh = useCallback(
    (val: AutoRefreshOption) => {
      setAutoRefreshState(val)
      if (storageKey) localStorage.setItem(storageKey, String(val))
    },
    [storageKey],
  )

  const indicator: Indicator =
    source === 'ws'
      ? wsConnected
        ? 'live'
        : 'offline'
      : lastUpdated && now - lastUpdated.getTime() < STALE_THRESHOLD_MS
      ? 'live'
      : 'stale'

  const label =
    lastUpdated
      ? formatDistanceLabel(lastUpdated, new Date(now))
      : 'Unknown'

  return { indicator, label, autoRefresh, setAutoRefresh, autoRefreshOptions: AUTO_REFRESH_OPTIONS }
}

function formatDistanceLabel(from: Date, to: Date): string {
  const diffSec = Math.round((to.getTime() - from.getTime()) / 1000)
  if (diffSec < 5) return 'Just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.round(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return `${Math.round(diffMin / 60)}h ago`
}
