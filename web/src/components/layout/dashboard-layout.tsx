import { Outlet } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { Topbar } from './topbar'
import { CommandPalette } from '@/components/command-palette/command-palette'
import { NotificationDrawer } from '@/components/notification/notification-drawer'
import { useUIStore } from '@/stores/ui'
import { cn } from '@/lib/utils'

export function DashboardLayout() {
  const { sidebarCollapsed } = useUIStore()

  return (
    <div className="min-h-screen ambient-bg">
      <Sidebar />
      <Topbar />
      <CommandPalette />
      <NotificationDrawer />
      <main
        className={cn(
          'pt-14 transition-all duration-200',
          sidebarCollapsed ? 'pl-16' : 'pl-60',
        )}
      >
        <div className="p-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
