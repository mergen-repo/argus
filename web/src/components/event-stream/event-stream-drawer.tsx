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
          events.map((event, idx) => {
            // metrics.realtime is a system-pulse signal pushed every second;
            // render it as a single compact row (timestamp + pulse dot) so it
            // doesn't drown out substantive events.
            if (event.type === 'metrics.realtime') {
              return (
                <div
                  key={event.id}
                  className={cn(
                    'flex items-center gap-2 py-0.5 px-2 rounded-[var(--radius-sm)] hover:bg-bg-hover transition-colors',
                    idx === 0 && 'animate-slide-up-in',
                  )}
                >
                  <span className="h-1.5 w-1.5 rounded-full bg-accent/60 pulse-dot shrink-0" />
                  <span className="font-mono text-[10px] text-text-tertiary">{formatTime(event.timestamp)}</span>
                  <span className="text-[10px] text-text-tertiary">metrics pulse</span>
                </div>
              )
            }
            return (
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
                  <div className="flex items-center gap-2 mt-0.5 flex-wrap">
                    <span className="text-[10px] text-text-tertiary font-mono">{event.type}</span>
                    <SourceChips event={event} />
                  </div>
                </div>
                <Badge variant={severityVariant(event.severity)} className="text-[9px] shrink-0 mt-0.5">{event.severity}</Badge>
              </div>
            )
          })
        )}
      </div>
    </Sheet>
  )
}

// SourceChips renders a line of inline `Label value` pairs derived from the
// LiveEvent's payload (IMSI, IP, MSISDN, operator_id, etc.). Priority:
// SIM-level chips (IMSI/IP/MSISDN) come first for session events; then
// operator/apn/policy/job IDs; finally entity_type:entity_id as a fallback
// so legacy events with no specific context still show something.
function SourceChips({ event }: { event: LiveEvent }) {
  const chips: Array<{ label: string; value: string; highlight?: boolean }> = []
  if (event.imsi) chips.push({ label: 'IMSI', value: event.imsi, highlight: true })
  if (event.framed_ip) chips.push({ label: 'IP', value: event.framed_ip, highlight: true })
  if (event.msisdn) chips.push({ label: 'MSISDN', value: event.msisdn })
  if (event.operator_id && !event.imsi) chips.push({ label: 'Op', value: event.operator_id.slice(0, 8) })
  if (event.apn_id && !event.imsi) chips.push({ label: 'APN', value: event.apn_id.slice(0, 8) })
  if (event.policy_id) chips.push({ label: 'Policy', value: event.policy_id.slice(0, 8) })
  if (event.job_id) chips.push({ label: 'Job', value: event.job_id.slice(0, 8) })
  if (typeof event.progress_pct === 'number') chips.push({ label: '%', value: `${Math.round(event.progress_pct)}` })
  if (chips.length === 0 && event.entity_type && event.entity_id) {
    chips.push({ label: event.entity_type, value: event.entity_id.slice(0, 8) })
  }
  if (chips.length === 0) return null
  return (
    <>
      {chips.map((c, i) => (
        <span key={i} className="inline-flex items-center gap-1 text-[10px] font-mono">
          <span className="text-text-tertiary opacity-60">{c.label}</span>
          <span className={c.highlight ? 'text-accent' : 'text-text-secondary'}>{c.value}</span>
        </span>
      ))}
    </>
  )
}
