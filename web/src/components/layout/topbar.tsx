import { useMemo } from 'react'
import { Search, Bell, User, Moon, Sun, LogOut, Settings, Activity, Languages } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useNavigate } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useNotificationStore } from '@/stores/notification'
import { useEventStore } from '@/stores/events'
import { useUnreadCount, useRealtimeNotifications } from '@/hooks/use-notifications'
import { useLogout } from '@/hooks/use-logout'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { WSIndicator } from '@/components/layout/ws-indicator'
import { TenantSwitcher } from '@/components/layout/tenant-switcher'
import { cn } from '@/lib/utils'
import i18n from '@/lib/i18n'

function ActivitySparkline({ onClick }: { onClick: () => void }) {
  const histogram = useEventStore((s) => s.histogram)
  const totalCount = useEventStore((s) => s.totalCount)

  const bars = useMemo(() => {
    const now = Math.floor(Date.now() / 60_000)
    const result: number[] = []
    for (let i = 14; i >= 0; i--) {
      const min = now - i
      const bucket = histogram.find((b) => b.minute === min)
      result.push(bucket?.count ?? 0)
    }
    return result
  }, [histogram])

  const max = Math.max(...bars, 1)
  const recent = bars.slice(-3).reduce((a, b) => a + b, 0)
  const hasActivity = recent > 0

  return (
    <Button
      variant="ghost"
      onClick={onClick}
      className="flex items-center gap-2 rounded-md px-2 py-1.5 h-auto"
      title={`${totalCount} events — Click to open event stream`}
    >
      <Activity className={cn('h-3.5 w-3.5 shrink-0', hasActivity ? 'text-accent' : 'text-text-tertiary')} />
      <div className="flex items-end gap-[1px] h-4">
        {bars.map((v, i) => (
          <div
            key={i}
            className={cn(
              'w-[3px] rounded-t-[1px] transition-all duration-300',
              i === bars.length - 1 && hasActivity ? 'bg-accent' : v > 0 ? 'bg-accent/50' : 'bg-text-tertiary/20',
            )}
            style={{ height: `${Math.max((v / max) * 100, 8)}%` }}
          />
        ))}
      </div>
      {hasActivity && (
        <span className="text-[10px] font-mono text-accent tabular-nums">{recent}</span>
      )}
    </Button>
  )
}

export function Topbar() {
  const { sidebarCollapsed, setCommandPaletteOpen, darkMode, toggleDarkMode, locale, setLocale } = useUIStore()
  const { user } = useAuthStore()
  const { unreadCount, toggleDrawer } = useNotificationStore()
  const toggleEventDrawer = useEventStore((s) => s.toggleDrawer)
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
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          onClick={() => setCommandPaletteOpen(true)}
          className="flex items-center gap-2 h-auto px-3 py-1.5 text-sm text-text-tertiary hover:border-text-tertiary hover:text-text-secondary"
        >
          <Search className="h-4 w-4" />
          <span>Search...</span>
          <kbd className="ml-4 rounded border border-border bg-bg-elevated px-1.5 py-0.5 font-mono text-[10px] text-text-tertiary">
            Ctrl+K
          </kbd>
        </Button>
        <TenantSwitcher />
      </div>

      <div className="flex items-center gap-1">
        <WSIndicator />

        <Button
          variant="ghost"
          size="icon"
          onClick={toggleDarkMode}
          className="text-text-secondary hover:text-text-primary"
          title={darkMode ? 'Switch to light mode' : 'Switch to dark mode'}
        >
          {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </Button>

        <Button
          variant="ghost"
          onClick={() => {
            const next = locale === 'en' ? 'tr' : 'en'
            setLocale(next)
            i18n.changeLanguage(next)
          }}
          className="px-2 py-1.5 h-auto text-xs font-medium text-text-secondary hover:text-text-primary flex items-center gap-1"
          title="Toggle language"
        >
          <Languages className="h-3.5 w-3.5" />
          {locale.toUpperCase()}
        </Button>

        <ActivitySparkline onClick={toggleEventDrawer} />

        <Button
          variant="ghost"
          size="icon"
          onClick={toggleDrawer}
          className="relative text-text-secondary hover:text-text-primary"
          title="Notifications"
        >
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-danger text-[9px] font-bold text-white">
              {unreadCount > 9 ? '9+' : unreadCount}
            </span>
          )}
        </Button>

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
