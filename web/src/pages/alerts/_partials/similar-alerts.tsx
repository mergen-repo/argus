import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, RefreshCw } from 'lucide-react'
import { api } from '@/lib/api'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { EntityLink } from '@/components/shared/entity-link'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { timeAgo } from '@/lib/format'
import type { Alert } from '@/types/analytics'

interface SimilarAlertsProps {
  anchorId: string
  anchorDedupKey?: string | null
  anchorType?: string
  anchorSource?: string
}

interface SimilarAlertsResponse {
  status: string
  data: Alert[]
  meta: {
    anchor_id: string
    match_strategy: string
    count: number
  }
}

function useSimilarAlerts(anchorId: string) {
  return useQuery({
    queryKey: ['alerts', anchorId, 'similar'],
    queryFn: async () => {
      const res = await api.get<SimilarAlertsResponse>(`/alerts/${anchorId}/similar?limit=20`)
      return res.data
    },
    staleTime: 60_000,
    enabled: !!anchorId,
  })
}

function statePill(state: string) {
  switch (state) {
    case 'open':
      return <Badge className="bg-danger-dim text-danger border-0 text-[10px] flex-shrink-0">open</Badge>
    case 'acknowledged':
      return <Badge className="bg-warning-dim text-warning border-0 text-[10px] flex-shrink-0">ack</Badge>
    case 'resolved':
      return <Badge className="bg-success-dim text-success border-0 text-[10px] flex-shrink-0">resolved</Badge>
    case 'suppressed':
      return <Badge className="bg-bg-elevated text-text-tertiary border border-border text-[10px] flex-shrink-0">suppressed</Badge>
    default:
      return null
  }
}

function viewAllHref(
  anchorDedupKey?: string | null,
  anchorType?: string,
  anchorSource?: string,
): string {
  // FIX-229 Gate F-A1: prefer dedup_key when the anchor has one — the
  // similar-alerts API matches by dedup_key first; the deeplink should
  // mirror that ranking. Falls back to type+source when dedup_key is
  // absent. Both are accepted by the /alerts list endpoint.
  const params = new URLSearchParams()
  if (anchorDedupKey) {
    params.set('dedup_key', anchorDedupKey)
  } else {
    if (anchorType) params.set('type', anchorType)
    if (anchorSource) params.set('source', anchorSource)
  }
  return `/alerts?${params.toString()}`
}

export function SimilarAlerts({
  anchorId,
  anchorDedupKey,
  anchorType,
  anchorSource,
}: SimilarAlertsProps) {
  const { data, isLoading, isError, refetch } = useSimilarAlerts(anchorId)
  const LIMIT = 20

  if (isLoading) {
    return (
      <div className="space-y-2 py-1">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 py-2">
            <Skeleton className="h-5 w-16 rounded-[var(--radius-sm)]" />
            <Skeleton className="h-4 w-12 rounded-[var(--radius-sm)]" />
            <Skeleton className="h-4 flex-1 rounded-[var(--radius-sm)]" />
            <Skeleton className="h-4 w-20 rounded-[var(--radius-sm)]" />
          </div>
        ))}
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center gap-3 py-6 text-center">
        <p className="text-sm text-text-secondary">Could not load similar alerts.</p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          className="gap-1.5 text-xs"
        >
          <RefreshCw className="h-3.5 w-3.5" />
          Retry
        </Button>
      </div>
    )
  }

  const alerts = data?.data ?? []
  const count = data?.meta?.count ?? alerts.length
  const atLimit = count >= LIMIT

  if (alerts.length === 0) {
    return (
      <p className="py-6 text-center text-sm text-text-secondary">
        No similar alerts in retention window.
      </p>
    )
  }

  const href = viewAllHref(anchorDedupKey, anchorType, anchorSource)

  return (
    <div className="bg-bg-surface rounded-[var(--radius-sm)] divide-y divide-border-subtle">
      {alerts.map((alert) => (
        <div
          key={alert.id}
          className={cn(
            'flex items-center gap-3 px-3 py-2.5 transition-colors duration-150',
            'hover:bg-bg-hover/50',
          )}
        >
          <SeverityBadge severity={alert.severity} iconOnly className="flex-shrink-0" />
          {statePill(alert.state)}
          <span className="flex-1 min-w-0 text-sm text-text-primary truncate" title={alert.title}>
            {alert.title.length > 80 ? `${alert.title.slice(0, 80)}…` : alert.title}
          </span>
          {alert.operator_id && (
            <span className="flex-shrink-0">
              <EntityLink
                entityType="operator"
                entityId={alert.operator_id}
                label={alert.operator_id}
                showIcon
              />
            </span>
          )}
          <span className="flex-shrink-0 text-[11px] text-text-tertiary font-mono">
            {timeAgo(alert.fired_at)}
          </span>
        </div>
      ))}

      <div className="flex items-center justify-between px-3 py-2">
        {atLimit && (
          <p className="text-[11px] text-text-tertiary">
            + More similar exist — narrow filters to view all
          </p>
        )}
        <Link
          to={href}
          className={cn(
            'ml-auto flex items-center gap-1 text-xs text-accent hover:underline',
            !atLimit && 'ml-0',
          )}
        >
          View all
          <ArrowRight className="h-3 w-3" />
        </Link>
      </div>
    </div>
  )
}

export { useSimilarAlerts }
