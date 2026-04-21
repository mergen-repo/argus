import { useEffect } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { Topbar } from './topbar'
import { CommandPalette } from '@/components/command-palette/command-palette'
import { NotificationDrawer } from '@/components/notification/notification-drawer'
import { EventStreamDrawer } from '@/components/event-stream/event-stream-drawer'
import { ErrorBoundary } from '@/components/error-boundary'
import { StatusBar } from './status-bar'
import { KeyboardShortcuts } from '@/components/ui/keyboard-shortcuts'
import { useKeyboardNav } from '@/hooks/use-keyboard-nav'
import { useUIStore } from '@/stores/ui'
import { useEventStore, type LiveEvent } from '@/stores/events'
import { wsClient } from '@/lib/ws'
import { cn } from '@/lib/utils'
import { ImpersonationBanner } from '@/components/shared/impersonation-banner'
import { AnnouncementBanner } from '@/components/shared/announcement-banner'

function useGlobalEventListener() {
  const addEvent = useEventStore((s) => s.addEvent)

  useEffect(() => {
    const pickString = (v: unknown): string | undefined => (typeof v === 'string' && v ? v : undefined)
    const pickNumber = (v: unknown): number | undefined => (typeof v === 'number' ? v : undefined)
    const pickObject = (v: unknown): Record<string, unknown> | undefined =>
      v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined
    const pickEntity = (v: unknown): { type: string; id: string; display_name?: string } | undefined => {
      const obj = pickObject(v)
      if (!obj) return undefined
      const type = pickString(obj.type)
      const id = pickString(obj.id)
      if (!type || !id) return undefined
      return { type, id, display_name: pickString(obj.display_name) }
    }
    const normalizeSeverity = (v: unknown): LiveEvent['severity'] => {
      const s = pickString(v)
      if (s === 'critical' || s === 'high' || s === 'medium' || s === 'low' || s === 'info') return s
      if (import.meta.env.DEV && s) {
        // eslint-disable-next-line no-console
        console.debug('[events] unknown severity coerced to info', s)
      }
      return 'info'
    }

    const unsub = wsClient.on('*', (rawMsg: unknown) => {
      const msg = rawMsg as { type?: string; data?: Record<string, unknown> }
      if (!msg.type) return
      // metrics.realtime is a 1Hz system-pulse heartbeat (auth/s, error%,
      // active_sessions, latency) that drives the dashboard KPI cards
      // and topbar activity sparkline. It carries no SIM-level context
      // — excluding it here keeps the event stream focused on
      // per-subscriber actions (auth, usage, disconnect, SIM state).
      if (msg.type === 'metrics.realtime') return
      const d = msg.data || {}
      const envelope = rawMsg as { id?: string; type?: string; data?: Record<string, unknown> }

      // FIX-213 T3 — envelope-aware normalizer. Read FIX-212 envelope
      // fields first; fall back to legacy flat shape. Envelope wins on
      // overlap because that's the canonical future shape.
      const title = pickString(d.title) || pickString(d.message) || msg.type.replace(/\./g, ' ')
      const messageBody = pickString(d.message)
      const source = pickString(d.source)
      const entity = pickEntity(d.entity)
      const meta = pickObject(d.meta) || {}
      const dedup_key = pickString(d.dedup_key)
      const event_version = pickNumber(d.event_version)

      if (import.meta.env.DEV && event_version === undefined) {
        // Surface publisher-migration gaps during development.
        // eslint-disable-next-line no-console
        console.debug('[events] legacy shape', msg.type)
      }

      // Flat-field merge — envelope wins, legacy second. Always check
      // both sources so per-operator histograms (stores/events.ts)
      // still fire for envelope-shaped session events where operator_id
      // lives in meta.*.
      const operator_id = pickString(meta.operator_id) || pickString(d.operator_id)
      const apn_id = pickString(meta.apn_id) || pickString(d.apn_id)
      const sim_id =
        (entity?.type === 'sim' ? entity.id : undefined) ||
        pickString(meta.sim_id) ||
        pickString(d.sim_id)
      const imsi =
        (entity?.type === 'sim' ? entity.display_name : undefined) ||
        pickString(meta.imsi) ||
        pickString(d.imsi)
      const msisdn = pickString(meta.msisdn) || pickString(d.msisdn)
      const framed_ip = pickString(meta.framed_ip) || pickString(d.framed_ip)
      const nas_ip = pickString(meta.nas_ip) || pickString(d.nas_ip)
      const policy_id = pickString(meta.policy_id) || pickString(d.policy_id)
      const job_id = pickString(meta.job_id) || pickString(d.job_id)
      const progress_pct = pickNumber(meta.progress_pct) ?? pickNumber(d.progress_pct)
      const bytes_in = pickNumber(meta.bytes_in) ?? pickNumber(d.bytes_in)
      const bytes_out = pickNumber(meta.bytes_out) ?? pickNumber(d.bytes_out)

      const evt: LiveEvent = {
        id: envelope.id || `fallback-${Date.now()}`,
        type: msg.type,
        title,
        message: messageBody || title,
        severity: normalizeSeverity(d.severity),
        timestamp: pickString(d.timestamp) || new Date().toISOString(),
        source,
        entity,
        meta,
        dedup_key,
        event_version,
        entity_type: entity?.type || pickString(d.entity_type),
        entity_id: entity?.id || pickString(d.entity_id),
        imsi,
        msisdn,
        framed_ip,
        nas_ip,
        operator_id,
        apn_id,
        policy_id,
        job_id,
        sim_id,
        tenant_id: pickString(d.tenant_id),
        progress_pct,
        bytes_in,
        bytes_out,
      }
      addEvent(evt)
    })
    return unsub
  }, [addEvent])
}

export function DashboardLayout() {
  const { sidebarCollapsed } = useUIStore()
  const location = useLocation()
  useKeyboardNav()
  useGlobalEventListener()

  return (
    <div className="min-h-screen ambient-bg">
      <Sidebar />
      <Topbar />
      <CommandPalette />
      <NotificationDrawer />
      <EventStreamDrawer />
      <main
        className={cn(
          'pt-14 pb-10 transition-all duration-200',
          sidebarCollapsed ? 'pl-16' : 'pl-60',
        )}
      >
        <ImpersonationBanner />
        <AnnouncementBanner />
        <div className="p-6">
          <ErrorBoundary key={location.pathname}>
            <Outlet />
          </ErrorBoundary>
        </div>
      </main>
      <StatusBar />
      <KeyboardShortcuts />
    </div>
  )
}
