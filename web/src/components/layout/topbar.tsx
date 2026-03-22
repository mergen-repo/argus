import { Search, Bell, User } from 'lucide-react'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'

export function Topbar() {
  const { sidebarCollapsed, setCommandPaletteOpen } = useUIStore()
  const { user } = useAuthStore()

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
        <button className="relative rounded-md p-2 text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors">
          <Bell className="h-4 w-4" />
          <span className="absolute -top-0.5 -right-0.5 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-danger text-[9px] font-bold text-white">
            3
          </span>
        </button>
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-bg-elevated text-text-secondary">
            <User className="h-4 w-4" />
          </div>
          {user && (
            <span className="text-sm text-text-secondary">{user.name || user.email}</span>
          )}
        </div>
      </div>
    </header>
  )
}
