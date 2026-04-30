import { useNavigate } from 'react-router-dom'
import {
  Activity, AlertTriangle, ChevronRight, Database, Radio, Server, Shield, Smartphone, Wifi, Zap,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/format'
import { severityIconClass } from '@/lib/severity'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { Button } from '@/components/ui/button'
import type { LiveEvent } from '@/stores/events'
import { EventEntityButton } from './event-entity-button'
import { EventSourceChips } from './event-source-chips'

function eventIcon(type: string) {
  if (type.includes('session')) return <Radio className="h-3.5 w-3.5" />
  if (type.includes('auth') || type.includes('metric')) return <Activity className="h-3.5 w-3.5" />
  if (type.includes('alert') || type.includes('anomaly')) return <AlertTriangle className="h-3.5 w-3.5" />
  if (type.includes('operator')) return <Server className="h-3.5 w-3.5" />
  if (type.includes('sim')) return <Smartphone className="h-3.5 w-3.5" />
  if (type.includes('policy')) return <Shield className="h-3.5 w-3.5" />
  if (type.includes('ip') || type.includes('pool')) return <Database className="h-3.5 w-3.5" />
  if (type.includes('job')) return <Zap className="h-3.5 w-3.5" />
  return <Wifi className="h-3.5 w-3.5" />
}

function formatAbsTime(iso: string): string {
  const d = new Date(iso)
  if (!Number.isFinite(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

interface EventRowProps {
  event: LiveEvent
  onClose: () => void
  isFirst?: boolean
}

export function EventRow({ event, onClose, isFirst = false }: EventRowProps) {
  const navigate = useNavigate()

  const title = event.title || event.message || event.type
  const message = event.message && event.message !== title ? event.message : undefined
  const alertId =
    typeof event.meta?.alert_id === 'string' && event.meta.alert_id.length > 0
      ? (event.meta.alert_id as string)
      : undefined

  return (
    <div
      className={cn(
        'group flex flex-col gap-1 py-1.5 px-2 rounded-[var(--radius-sm)] hover:bg-bg-hover transition-colors border-b border-border-subtle/30',
        isFirst && 'animate-slide-up-in',
      )}
    >
      <div className="flex items-start gap-2.5">
        <span className={cn('mt-0.5 shrink-0', severityIconClass(event.severity))}>
          {eventIcon(event.type)}
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-mono text-[10px] text-text-tertiary shrink-0">
              {formatAbsTime(event.timestamp)}
            </span>
            <span className="text-xs text-text-primary font-medium truncate">{title}</span>
          </div>
          <div className="flex items-center gap-2 mt-0.5 flex-wrap">
            {event.source && (
              <span className="inline-flex items-center gap-1 text-[10px] font-mono">
                <span className="text-text-tertiary opacity-60">src</span>
                <span className="text-text-secondary">{event.source}</span>
              </span>
            )}
            <span className="text-[10px] text-text-tertiary font-mono opacity-70">{event.type}</span>
            <span className="text-[10px] text-text-tertiary opacity-60">·</span>
            <span className="text-[10px] text-text-tertiary">{formatRelativeTime(event.timestamp)}</span>
          </div>
        </div>
        <SeverityBadge severity={event.severity} className="shrink-0 mt-0.5" />
      </div>

      {(event.entity?.id || event.entity_id) && (
        <div className="pl-6">
          <EventEntityButton
            entity={event.entity}
            entityTypeFallback={event.entity_type}
            entityIdFallback={event.entity_id}
            onNavigate={onClose}
          />
        </div>
      )}

      {message && (
        <div className="pl-6 text-[11px] text-text-secondary leading-snug">{message}</div>
      )}

      <div className="pl-6 flex items-center gap-2 flex-wrap">
        <EventSourceChips event={event} />
      </div>

      {alertId && (
        <div className="pl-6">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={(e) => {
              e.stopPropagation()
              onClose()
              navigate(`/alerts/${alertId}`)
            }}
            aria-label="Alarm detayını aç"
            className="h-auto gap-0.5 px-0 py-0 text-[10px] font-medium text-accent hover:bg-transparent hover:text-accent-bright hover:underline focus-visible:ring-1 focus-visible:ring-accent"
          >
            <span>Detaylar</span>
            <ChevronRight className="h-2.5 w-2.5" aria-hidden="true" />
          </Button>
        </div>
      )}
    </div>
  )
}
