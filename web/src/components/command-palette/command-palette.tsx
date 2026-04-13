import { useEffect, useState, useCallback } from 'react'
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
  BellRing,
  HeartPulse,
  Building,
  BookOpen,
  Gauge,
  XCircle,
  Antenna,
  Server,
  Archive,
  Rocket,
  History,
  Search,
  Clock,
  Star,
} from 'lucide-react'
import { useUIStore } from '@/stores/ui'
import { useSearch, type SearchResult } from '@/hooks/use-search'
import { useDebounce } from '@/hooks/use-debounce'

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
  { label: 'Notification Config', icon: BellRing, path: '/settings/notifications', group: 'Settings' },
  { label: 'Knowledge Base', icon: BookOpen, path: '/settings/knowledgebase', group: 'Settings' },
  { label: 'System Health', icon: HeartPulse, path: '/system/health', group: 'System' },
  { label: 'Tenants', icon: Building, path: '/system/tenants', group: 'System' },
  { label: 'SRE — Performance', icon: Gauge, path: '/ops/performance', group: 'SRE' },
  { label: 'SRE — Errors', icon: XCircle, path: '/ops/errors', group: 'SRE' },
  { label: 'SRE — AAA Live', icon: Antenna, path: '/ops/aaa-traffic', group: 'SRE' },
  { label: 'SRE — Infra', icon: Server, path: '/ops/infra', group: 'SRE' },
  { label: 'SRE — Job Queue', icon: ListTodo, path: '/ops/jobs', group: 'SRE' },
  { label: 'SRE — Backups', icon: Archive, path: '/ops/backup', group: 'SRE' },
  { label: 'SRE — Deploys', icon: Rocket, path: '/ops/deploys', group: 'SRE' },
  { label: 'SRE — Incidents', icon: History, path: '/ops/incidents', group: 'SRE' },
]

const ENTITY_DETAIL_ROUTES: Record<string, (id: string) => string> = {
  sim: (id) => `/sims/${id}`,
  apn: (id) => `/apns/${id}`,
  operator: (id) => `/operators/${id}`,
  policy: (id) => `/policies/${id}`,
  user: (id) => `/settings/users/${id}`,
}

const ENTITY_ICONS: Record<string, React.ElementType> = {
  sim: CardSim,
  apn: Network,
  operator: Building2,
  policy: Shield,
  user: Users,
}

const ENTITY_TYPE_LABELS: Record<string, string> = {
  sim: 'SIM Cards',
  apn: 'APNs',
  operator: 'Operators',
  policy: 'Policies',
  user: 'Users',
}

export function CommandPalette() {
  const { commandPaletteOpen, setCommandPaletteOpen, recentSearches, addRecentSearch, recentItems, favorites } = useUIStore()
  const navigate = useNavigate()
  const [inputValue, setInputValue] = useState('')
  const debouncedQuery = useDebounce(inputValue, 300)

  const isEntityMode = debouncedQuery.trim().length >= 2

  const { data: searchResults, isFetching } = useSearch({
    q: debouncedQuery,
    enabled: isEntityMode,
  })

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

  const handleClose = useCallback(() => {
    setCommandPaletteOpen(false)
    setInputValue('')
  }, [setCommandPaletteOpen])

  const handleSelect = useCallback(
    (path: string, searchTerm?: string) => {
      if (searchTerm) addRecentSearch(searchTerm)
      navigate(path)
      handleClose()
    },
    [navigate, handleClose, addRecentSearch],
  )

  if (!commandPaletteOpen) return null

  const staticGroups = [...new Set(commands.map((c) => c.group))]

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]">
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm"
        onClick={handleClose}
      />
      <Command
        shouldFilter={isEntityMode ? false : true}
        className="relative z-50 w-full max-w-lg overflow-hidden rounded-xl border border-border bg-bg-elevated shadow-2xl"
        onKeyDown={(e: React.KeyboardEvent) => {
          if (e.key === 'Escape') handleClose()
        }}
      >
        <div className="flex items-center border-b border-border px-4 gap-2">
          <Search className="h-4 w-4 text-text-tertiary shrink-0" />
          <Command.Input
            placeholder="Search entities, pages, commands..."
            className="flex-1 border-0 bg-transparent py-3 text-sm text-text-primary placeholder:text-text-tertiary outline-none"
            autoFocus
            value={inputValue}
            onValueChange={setInputValue}
          />
          {isFetching && (
            <div className="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-border border-t-accent" />
          )}
        </div>

        <Command.List className="max-h-[400px] overflow-y-auto p-2">
          <Command.Empty className="py-6 text-center text-sm text-text-tertiary">
            No results found.
          </Command.Empty>

          {isEntityMode ? (
            <>
              {searchResults && Object.entries(searchResults).map(([type, items]) => {
                if (!items || items.length === 0) return null
                const Icon = ENTITY_ICONS[type] ?? Search
                const groupLabel = ENTITY_TYPE_LABELS[type] ?? type
                return (
                  <Command.Group key={type} heading={groupLabel}>
                    <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                      {groupLabel}
                    </div>
                    {(items as SearchResult[]).map((item) => {
                      const routeFn = ENTITY_DETAIL_ROUTES[type]
                      if (!routeFn) return null
                      return (
                        <Command.Item
                          key={`${type}-${item.id}`}
                          value={`${type}-${item.id}-${item.label}`}
                          onSelect={() => handleSelect(routeFn(item.id), inputValue)}
                          className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                        >
                          <Icon className="h-4 w-4 shrink-0 text-text-tertiary" />
                          <div className="flex flex-col min-w-0">
                            <span className="truncate font-mono text-xs">{item.label}</span>
                            {item.sub && (
                              <span className="truncate text-[11px] text-text-tertiary">{item.sub}</span>
                            )}
                          </div>
                        </Command.Item>
                      )
                    })}
                  </Command.Group>
                )
              })}
              {searchResults && Object.values(searchResults).every((v) => !v || v.length === 0) && !isFetching && (
                <Command.Empty className="py-6 text-center text-sm text-text-tertiary">
                  No entities found for &quot;{debouncedQuery}&quot;
                </Command.Empty>
              )}
            </>
          ) : (
            <>
              {recentSearches.length > 0 && (
                <Command.Group heading="Recent Searches">
                  <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                    Recent Searches
                  </div>
                  {recentSearches.slice(0, 5).map((q) => (
                    <Command.Item
                      key={`recent-search-${q}`}
                      value={`recent-search-${q}`}
                      onSelect={() => setInputValue(q)}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                    >
                      <Clock className="h-4 w-4 shrink-0 text-text-tertiary" />
                      <span className="font-mono text-xs">{q}</span>
                    </Command.Item>
                  ))}
                </Command.Group>
              )}

              {favorites.length > 0 && (
                <Command.Group heading="Favorites">
                  <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                    Favorites
                  </div>
                  {favorites.map((fav) => {
                    const Icon = ENTITY_ICONS[fav.type] ?? Star
                    return (
                      <Command.Item
                        key={`fav-${fav.id}`}
                        value={`fav-${fav.id}-${fav.label}`}
                        onSelect={() => handleSelect(fav.path)}
                        className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                      >
                        <Icon className="h-4 w-4 shrink-0 text-accent" />
                        <div className="flex flex-col min-w-0">
                          <span className="truncate text-xs">{fav.label}</span>
                          <span className="truncate text-[11px] text-text-tertiary capitalize">{fav.type}</span>
                        </div>
                      </Command.Item>
                    )
                  })}
                </Command.Group>
              )}

              {recentItems.length > 0 && (
                <Command.Group heading="Recent">
                  <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
                    Recent
                  </div>
                  {recentItems.slice(0, 5).map((item) => {
                    const Icon = ENTITY_ICONS[item.type] ?? History
                    return (
                      <Command.Item
                        key={`recent-${item.id}`}
                        value={`recent-${item.id}-${item.label}`}
                        onSelect={() => handleSelect(item.path)}
                        className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                      >
                        <Icon className="h-4 w-4 shrink-0 text-text-tertiary" />
                        <span className="truncate text-xs">{item.label}</span>
                      </Command.Item>
                    )
                  })}
                </Command.Group>
              )}

              {staticGroups.map((group) => (
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
                          onSelect={() => handleSelect(cmd.path)}
                          className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-text-secondary aria-selected:bg-bg-hover aria-selected:text-text-primary"
                        >
                          <Icon className="h-4 w-4" />
                          <span>{cmd.label}</span>
                        </Command.Item>
                      )
                    })}
                </Command.Group>
              ))}
            </>
          )}
        </Command.List>
      </Command>
    </div>
  )
}
