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
  HeartPulse,
  Building,
  ChevronLeft,
  ChevronRight,
  BookOpen,
  AlertTriangle,
  ShieldCheck,
  GitBranch,
  FileBarChart,
  HardDrive,
  Star,
  Clock,
  Handshake,
  Gauge,
  XCircle,
  Antenna,
  Server,
  Archive,
  Rocket,
  History,
  UserCheck,
  PackageSearch,
  MessageSquare,
  Settings,
} from 'lucide-react'
import { hasMinRole } from '@/lib/rbac'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'

interface NavItem {
  label: string
  icon: React.ElementType
  path: string
  minRole?: string
}

interface NavGroup {
  title: string
  items: NavItem[]
  minRole?: string
}

const navGroups: NavGroup[] = [
  {
    title: 'OVERVIEW',
    items: [
      { label: 'Dashboard', icon: LayoutDashboard, path: '/' },
      { label: 'Analytics', icon: BarChart3, path: '/analytics', minRole: 'analyst' },
      { label: 'Alerts', icon: AlertTriangle, path: '/alerts', minRole: 'analyst' },
      { label: 'SLA', icon: ShieldCheck, path: '/sla', minRole: 'analyst' },
    ],
  },
  {
    title: 'MANAGEMENT',
    items: [
      { label: 'SIM Cards', icon: CardSim, path: '/sims', minRole: 'sim_manager' },
      { label: 'APNs', icon: Network, path: '/apns', minRole: 'sim_manager' },
      { label: 'Operators', icon: Building2, path: '/operators' },
      { label: 'IP Pools', icon: Globe, path: '/settings/ip-pools', minRole: 'operator_manager' },
      { label: 'Sessions', icon: Radio, path: '/sessions', minRole: 'sim_manager' },
      { label: 'CDRs', icon: FileBarChart, path: '/cdrs', minRole: 'analyst' },
      { label: 'Policies', icon: Shield, path: '/policies', minRole: 'policy_editor' },
      { label: 'Violations', icon: AlertTriangle, path: '/violations', minRole: 'sim_manager' },
      { label: 'eSIM', icon: Smartphone, path: '/esim', minRole: 'sim_manager' },
      { label: 'Topology', icon: GitBranch, path: '/topology', minRole: 'operator_manager' },
    ],
  },
  {
    title: 'OPERATIONS',
    minRole: 'sim_manager',
    items: [
      { label: 'Jobs', icon: ListTodo, path: '/jobs', minRole: 'sim_manager' },
      { label: 'Audit Log', icon: ScrollText, path: '/audit', minRole: 'tenant_admin' },
      { label: 'Notifications', icon: Bell, path: '/notifications' },
      { label: 'Reports', icon: FileBarChart, path: '/reports', minRole: 'analyst' },
      { label: 'Capacity', icon: HardDrive, path: '/capacity', minRole: 'analyst' },
      { label: 'Roaming', icon: Handshake, path: '/roaming-agreements', minRole: 'operator_manager' },
    ],
  },
  {
    title: 'SETTINGS',
    minRole: 'tenant_admin',
    items: [
      { label: 'Settings', icon: Settings, path: '/settings' },
      { label: 'Users & Roles', icon: Users, path: '/settings/users' },
      { label: 'Knowledge Base', icon: BookOpen, path: '/settings/knowledgebase' },
    ],
  },
  {
    title: 'SYSTEM',
    minRole: 'super_admin',
    items: [
      { label: 'Health', icon: HeartPulse, path: '/system/health' },
      { label: 'Tenants', icon: Building, path: '/system/tenants' },
    ],
  },
  {
    title: 'ADMIN',
    minRole: 'tenant_admin',
    items: [
      { label: 'Tenant Usage', icon: Gauge, path: '/admin/tenant-usage' },
      { label: 'Security Events', icon: Shield, path: '/admin/security-events', minRole: 'super_admin' },
      { label: 'Sessions', icon: UserCheck, path: '/admin/sessions', minRole: 'super_admin' },
      { label: 'API Usage', icon: Key, path: '/admin/api-usage', minRole: 'super_admin' },
      { label: 'Purge History', icon: PackageSearch, path: '/admin/purge-history', minRole: 'super_admin' },
      { label: 'Delivery Status', icon: MessageSquare, path: '/admin/delivery', minRole: 'super_admin' },
    ],
  },
  {
    title: 'OPERATIONS — SRE',
    minRole: 'super_admin',
    items: [
      { label: 'Performance', icon: Gauge, path: '/ops/performance' },
      { label: 'Errors', icon: XCircle, path: '/ops/errors' },
      { label: 'AAA Live', icon: Antenna, path: '/ops/aaa-traffic' },
      { label: 'Infra', icon: Server, path: '/ops/infra' },
      { label: 'Job Queue', icon: ListTodo, path: '/ops/jobs' },
      { label: 'Backups', icon: Archive, path: '/ops/backup' },
      { label: 'Deploys', icon: Rocket, path: '/ops/deploys' },
      { label: 'Incidents', icon: History, path: '/ops/incidents' },
    ],
  },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, toggleSidebar, favorites, recentItems } = useUIStore()
  const userRole = useAuthStore((s) => s.user?.role)

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/'
    return location.pathname.startsWith(path)
  }

  const visibleGroups = navGroups
    .filter((group) => !group.minRole || hasMinRole(userRole, group.minRole))
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !item.minRole || hasMinRole(userRole, item.minRole)),
    }))
    .filter((group) => group.items.length > 0)

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

      {!sidebarCollapsed && (favorites.length > 0 || recentItems.length > 0) && (
        <div className="border-b border-border px-3 py-3">
          {favorites.length > 0 && (
            <div className="mb-3">
              <div className="mb-1.5 px-2 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary flex items-center gap-1.5">
                <Star className="h-3 w-3" />
                PINNED
              </div>
              <div className="space-y-0.5">
                {favorites.map((item) => (
                  <Link
                    key={item.id}
                    to={item.path}
                    className="flex items-center gap-2 rounded-md px-2 py-1 text-xs text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors truncate"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-accent shrink-0" />
                    {item.label}
                  </Link>
                ))}
              </div>
            </div>
          )}
          {recentItems.length > 0 && (
            <div>
              <div className="mb-1.5 px-2 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary flex items-center gap-1.5">
                <Clock className="h-3 w-3" />
                RECENT
              </div>
              <div className="space-y-0.5">
                {recentItems.slice(0, 5).map((item) => (
                  <Link
                    key={item.id}
                    to={item.path}
                    className="flex items-center gap-2 rounded-md px-2 py-1 text-xs text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors truncate"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-text-tertiary shrink-0" />
                    {item.label}
                  </Link>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      <nav className="flex-1 overflow-y-auto px-3 py-4">
        {visibleGroups.map((group) => (
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

      <div className="border-t border-border p-3">
        <Button
          type="button"
          variant="ghost"
          onClick={toggleSidebar}
          className="flex w-full items-center justify-start gap-3 h-auto px-2 py-1.5 text-sm"
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
        </Button>
      </div>
    </aside>
  )
}
