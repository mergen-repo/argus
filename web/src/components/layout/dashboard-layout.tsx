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

function useGlobalEventListener() {
  const addEvent = useEventStore((s) => s.addEvent)

  useEffect(() => {
    const unsub = wsClient.on('*', (rawMsg: unknown) => {
      const msg = rawMsg as { type?: string; data?: Record<string, unknown> }
      if (!msg.type) return
      const d = msg.data || {}
      const evt: LiveEvent = {
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        type: msg.type,
        message: (d.message as string) || msg.type.replace(/\./g, ' '),
        severity: (d.severity as LiveEvent['severity']) || 'info',
        timestamp: new Date().toISOString(),
        entity_type: d.entity_type as string,
        entity_id: d.entity_id as string,
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
