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

    const unsub = wsClient.on('*', (rawMsg: unknown) => {
      const msg = rawMsg as { type?: string; data?: Record<string, unknown> }
      if (!msg.type) return
      const d = msg.data || {}
      const envelope = rawMsg as { id?: string; type?: string; data?: Record<string, unknown> }
      const evt: LiveEvent = {
        id: envelope.id || `fallback-${Date.now()}`,
        type: msg.type,
        message: pickString(d.message) || msg.type.replace(/\./g, ' '),
        severity: (pickString(d.severity) as LiveEvent['severity']) || 'info',
        timestamp: new Date().toISOString(),
        entity_type: pickString(d.entity_type),
        entity_id: pickString(d.entity_id),
        // Source context — copy every known field from the payload so the
        // drawer can render IMSI / IP / operator chips without round-tripping
        // to the API.
        imsi: pickString(d.imsi),
        msisdn: pickString(d.msisdn),
        framed_ip: pickString(d.framed_ip),
        nas_ip: pickString(d.nas_ip),
        operator_id: pickString(d.operator_id),
        apn_id: pickString(d.apn_id),
        policy_id: pickString(d.policy_id),
        job_id: pickString(d.job_id),
        sim_id: pickString(d.sim_id),
        tenant_id: pickString(d.tenant_id),
        progress_pct: pickNumber(d.progress_pct),
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
