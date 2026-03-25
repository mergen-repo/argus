import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Radio, Zap, Wifi, WifiOff, Shield, Activity, TrendingUp, Lock, Database, AlertTriangle, Server, Smartphone,
} from 'lucide-react'
import { Sheet, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { useEventStore, type LiveEvent } from '@/stores/events'

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

function severityColor(s: string): string {
  switch (s) {
    case 'critical': return 'text-danger'
    case 'warning': return 'text-warning'
    default: return 'text-text-tertiary'
  }
}

function severityVariant(s: string): 'danger' | 'warning' | 'default' {
  switch (s) {
    case 'critical': return 'danger'
    case 'warning': return 'warning'
    default: return 'default'
  }
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function EventStreamDrawer() {
  const { events, drawerOpen, setDrawerOpen } = useEventStore()
  const navigate = useNavigate()

  const handleClick = useCallback((event: LiveEvent) => {
    if (event.entity_type && event.entity_id) {
      const routes: Record<string, string> = {
        sim: '/sims', session: '/sessions', operator: '/operators', policy: '/policies', apn: '/apns',
      }
      const base = routes[event.entity_type] || '/alerts'
      setDrawerOpen(false)
      navigate(`${base}/${event.entity_id}`)
    }
  }, [navigate, setDrawerOpen])

  return (
    <Sheet open={drawerOpen} onOpenChange={setDrawerOpen} side="right">
      <SheetHeader>
        <div className="flex items-center justify-between">
          <SheetTitle className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-accent" />
            Live Event Stream
          </SheetTitle>
          <span className="flex items-center gap-1.5">
            <span className="h-1.5 w-1.5 rounded-full bg-success pulse-dot" style={{ boxShadow: '0 0 6px rgba(0,255,136,0.4)' }} />
            <span className="text-[9px] font-semibold tracking-[1px] text-success">LIVE</span>
          </span>
        </div>
      </SheetHeader>

      <div className="text-[10px] text-text-tertiary mb-3 px-1">
        Showing last {events.length} events from all WebSocket channels
      </div>

      <div className="flex flex-col gap-0.5 max-h-[calc(100vh-160px)] overflow-y-auto">
        {events.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-[200px] text-text-tertiary text-sm gap-2">
            <Radio className="h-5 w-5 animate-pulse" />
            <span>Waiting for events...</span>
          </div>
        ) : (
          events.map((event, idx) => (
            <div
              key={event.id}
              className={cn(
                'flex items-start gap-2.5 py-1.5 px-2 rounded-[var(--radius-sm)] hover:bg-bg-hover transition-colors',
                event.entity_type && event.entity_id && 'cursor-pointer',
                idx === 0 && 'animate-slide-up-in',
              )}
              onClick={() => handleClick(event)}
            >
              <span className={cn('mt-0.5 shrink-0', severityColor(event.severity))}>
                {eventIcon(event.type)}
              </span>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[10px] text-text-tertiary shrink-0">{formatTime(event.timestamp)}</span>
                  <span className="text-xs text-text-primary truncate">{event.message}</span>
                </div>
                <div className="flex items-center gap-2 mt-0.5">
                  <span className="text-[10px] text-text-tertiary font-mono">{event.type}</span>
                  {event.entity_type && (
                    <span className="text-[10px] text-accent font-mono">{event.entity_type}:{event.entity_id?.slice(0, 8)}</span>
                  )}
                </div>
              </div>
              <Badge variant={severityVariant(event.severity)} className="text-[9px] shrink-0 mt-0.5">{event.severity}</Badge>
            </div>
          ))
        )}
      </div>
    </Sheet>
  )
}
