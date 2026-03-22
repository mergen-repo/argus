import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Command } from 'cmdk'
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
} from 'lucide-react'
import { useUIStore } from '@/stores/ui'

interface CommandItem {
  label: string
  icon: React.ElementType
  path: string
  group: string
}

const commands: CommandItem[] = [
  { label: 'Dashboard', icon: LayoutDashboard, path: '/', group: 'Pages' },
  { label: 'Analytics', icon: BarChart3, path: '/analytics', group: 'Pages' },
  { label: 'Analytics — Cost', icon: BarChart3, path: '/analytics/cost', group: 'Pages' },
  { label: 'Analytics — Anomalies', icon: BarChart3, path: '/analytics/anomalies', group: 'Pages' },
  { label: 'SIM Cards', icon: CardSim, path: '/sims', group: 'Pages' },
  { label: 'APNs', icon: Network, path: '/apns', group: 'Pages' },
  { label: 'Operators', icon: Building2, path: '/operators', group: 'Pages' },
  { label: 'Sessions', icon: Radio, path: '/sessions', group: 'Pages' },
  { label: 'Policies', icon: Shield, path: '/policies', group: 'Pages' },
  { label: 'eSIM Profiles', icon: Smartphone, path: '/esim', group: 'Pages' },
  { label: 'Jobs', icon: ListTodo, path: '/jobs', group: 'Pages' },
  { label: 'Audit Log', icon: ScrollText, path: '/audit', group: 'Pages' },
  { label: 'Notifications', icon: Bell, path: '/notifications', group: 'Pages' },
  { label: 'Users & Roles', icon: Users, path: '/settings/users', group: 'Settings' },
  { label: 'API Keys', icon: Key, path: '/settings/api-keys', group: 'Settings' },
  { label: 'IP Pools', icon: Globe, path: '/settings/ip-pools', group: 'Settings' },
  { label: 'System Health', icon: HeartPulse, path: '/system/health', group: 'System' },
  { label: 'Tenants', icon: Building, path: '/system/tenants', group: 'System' },
]

export function CommandPalette() {
  const { commandPaletteOpen, setCommandPaletteOpen } = useUIStore()
  const navigate = useNavigate()

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setCommandPaletteOpen(!commandPaletteOpen)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [commandPaletteOpen, setCommandPaletteOpen])

  if (!commandPaletteOpen) return null

  const groups = [...new Set(commands.map((c) => c.group))]

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]">
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm"
        onClick={() => setCommandPaletteOpen(false)}
      />
      <Command
        className="relative z-50 w-full max-w-lg overflow-hidden rounded-xl border border-border bg-bg-elevated shadow-2xl"
        onKeyDown={(e: React.KeyboardEvent) => {
          if (e.key === 'Escape') setCommandPaletteOpen(false)
        }}
      >
        <Command.Input
          placeholder="Search pages, SIMs, commands..."
          className="w-full border-b border-border bg-transparent px-4 py-3 text-sm text-text-primary placeholder:text-text-tertiary outline-none"
          autoFocus
        />
        <Command.List className="max-h-80 overflow-y-auto p-2">
          <Command.Empty className="py-6 text-center text-sm text-text-tertiary">
            No results found.
          </Command.Empty>
          {groups.map((group) => (
            <Command.Group key={group} heading={group} className="mb-2">
              <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                {group}
              </div>
              {commands
                .filter((c) => c.group === group)
                .map((cmd) => {
                  const Icon = cmd.icon
                  return (
                    <Command.Item
                      key={cmd.path}
                      value={cmd.label}
                      onSelect={() => {
                        navigate(cmd.path)
                        setCommandPaletteOpen(false)
                      }}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                    >
                      <Icon className="h-4 w-4" />
                      <span>{cmd.label}</span>
                    </Command.Item>
                  )
                })}
            </Command.Group>
          ))}
        </Command.List>
      </Command>
    </div>
  )
}
