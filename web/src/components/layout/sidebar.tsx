import { Link, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  BarChart3,
  CardSim,
  Network,
  Building2,
  Radio,
  Shield,
  Smartphone,
  ListTodo,
  ScrollText,
  Bell,
  Users,
  Key,
  Globe,
  BellRing,
  HeartPulse,
  Building,
  ChevronLeft,
  ChevronRight,
  Moon,
  Sun,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'

interface NavItem {
  label: string
  icon: React.ElementType
  path: string
}

interface NavGroup {
  title: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    title: 'OVERVIEW',
    items: [
      { label: 'Dashboard', icon: LayoutDashboard, path: '/' },
      { label: 'Analytics', icon: BarChart3, path: '/analytics' },
    ],
  },
  {
    title: 'MANAGEMENT',
    items: [
      { label: 'SIM Cards', icon: CardSim, path: '/sims' },
      { label: 'APNs', icon: Network, path: '/apns' },
      { label: 'Operators', icon: Building2, path: '/operators' },
      { label: 'Sessions', icon: Radio, path: '/sessions' },
      { label: 'Policies', icon: Shield, path: '/policies' },
      { label: 'eSIM', icon: Smartphone, path: '/esim' },
    ],
  },
  {
    title: 'OPERATIONS',
    items: [
      { label: 'Jobs', icon: ListTodo, path: '/jobs' },
      { label: 'Audit Log', icon: ScrollText, path: '/audit' },
      { label: 'Notifications', icon: Bell, path: '/notifications' },
    ],
  },
  {
    title: 'SETTINGS',
    items: [
      { label: 'Users & Roles', icon: Users, path: '/settings/users' },
      { label: 'API Keys', icon: Key, path: '/settings/api-keys' },
      { label: 'IP Pools', icon: Globe, path: '/settings/ip-pools' },
      { label: 'Notifications', icon: BellRing, path: '/settings/notifications' },
    ],
  },
  {
    title: 'SYSTEM',
    items: [
      { label: 'Health', icon: HeartPulse, path: '/system/health' },
      { label: 'Tenants', icon: Building, path: '/system/tenants' },
    ],
  },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, toggleSidebar, darkMode, toggleDarkMode } = useUIStore()

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/'
    return location.pathname.startsWith(path)
  }

  return (
    <aside
      className={cn(
        'fixed left-0 top-0 z-40 flex h-screen flex-col border-r border-border bg-bg-surface transition-all duration-200',
        sidebarCollapsed ? 'w-16' : 'w-60',
      )}
    >
      <div className="flex h-14 items-center border-b border-border px-4">
        <Link to="/" className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-accent neon-glow text-bg-primary font-bold text-sm">
            A
          </div>
          {!sidebarCollapsed && (
            <span className="font-semibold text-[15px] text-text-primary">Argus</span>
          )}
        </Link>
      </div>

      <nav className="flex-1 overflow-y-auto px-3 py-4">
        {navGroups.map((group) => (
          <div key={group.title} className="mb-6">
            {!sidebarCollapsed && (
              <div className="mb-2 px-2 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                {group.title}
              </div>
            )}
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const Icon = item.icon
                const active = isActive(item.path)
                return (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={cn(
                      'flex items-center gap-3 rounded-md px-2 py-1.5 text-sm transition-colors',
                      active
                        ? 'bg-accent-dim text-accent'
                        : 'text-text-secondary hover:bg-bg-hover hover:text-text-primary',
                      sidebarCollapsed && 'justify-center px-0',
                    )}
                    title={sidebarCollapsed ? item.label : undefined}
                  >
                    <Icon className="h-4 w-4 shrink-0" />
                    {!sidebarCollapsed && <span>{item.label}</span>}
                  </Link>
                )
              })}
            </div>
          </div>
        ))}
      </nav>

      <div className="border-t border-border p-3 space-y-1">
        <button
          onClick={toggleDarkMode}
          className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
          title={darkMode ? 'Switch to light mode' : 'Switch to dark mode'}
        >
          {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          {!sidebarCollapsed && <span>{darkMode ? 'Light mode' : 'Dark mode'}</span>}
        </button>
        <button
          onClick={toggleSidebar}
          className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
          title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {sidebarCollapsed ? (
            <ChevronRight className="h-4 w-4" />
          ) : (
            <>
              <ChevronLeft className="h-4 w-4" />
              <span>Collapse</span>
            </>
          )}
        </button>
      </div>
    </aside>
  )
}
