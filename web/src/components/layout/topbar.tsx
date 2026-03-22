import { Search, Bell, User, Moon, Sun, LogOut, Settings } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useNotificationStore } from '@/stores/notification'
import { useUnreadCount, useRealtimeNotifications } from '@/hooks/use-notifications'
import { useLogout } from '@/hooks/use-logout'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export function Topbar() {
  const { sidebarCollapsed, setCommandPaletteOpen, darkMode, toggleDarkMode } = useUIStore()
  const { user } = useAuthStore()
  const { unreadCount, toggleDrawer } = useNotificationStore()
  const handleLogout = useLogout()
  const navigate = useNavigate()

  useUnreadCount()
  useRealtimeNotifications()

  return (
    <header
      className={cn(
        'fixed top-0 right-0 z-30 flex h-14 items-center justify-between border-b border-border glass px-6 transition-all duration-200',
        sidebarCollapsed ? 'left-16' : 'left-60',
      )}
    >
      <button
        onClick={() => setCommandPaletteOpen(true)}
        className="flex items-center gap-2 rounded-md border border-border bg-bg-surface px-3 py-1.5 text-sm text-text-tertiary hover:border-text-tertiary hover:text-text-secondary transition-colors"
      >
        <Search className="h-4 w-4" />
        <span>Search...</span>
        <kbd className="ml-4 rounded border border-border bg-bg-elevated px-1.5 py-0.5 font-mono text-[10px] text-text-tertiary">
          Ctrl+K
        </kbd>
      </button>

      <div className="flex items-center gap-4">
        <button
          onClick={toggleDrawer}
          className="relative rounded-md p-2 text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
          title="Notifications"
        >
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-danger text-[9px] font-bold text-white">
              {unreadCount > 9 ? '9+' : unreadCount}
            </span>
          )}
        </button>

        <DropdownMenu>
          <DropdownMenuTrigger className="flex items-center gap-2 rounded-md px-2 py-1 hover:bg-bg-hover transition-colors">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-bg-elevated text-text-secondary">
              <User className="h-4 w-4" />
            </div>
            {user && (
              <span className="text-sm text-text-secondary">{user.name || user.email}</span>
            )}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem onClick={toggleDarkMode}>
              {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
              {darkMode ? 'Light mode' : 'Dark mode'}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => navigate('/setup')}>
              <Settings className="h-4 w-4" />
              Setup Wizard
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleLogout}>
              <LogOut className="h-4 w-4" />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}
