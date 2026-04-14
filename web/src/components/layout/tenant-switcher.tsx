import { useMemo, useState } from 'react'
import { Building2, Globe, ChevronDown, Check, Search, LogOut } from 'lucide-react'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { useAuthStore } from '@/stores/auth'
import { useTenantList } from '@/hooks/use-settings'
import { useSwitchTenant, useExitTenantContext } from '@/hooks/use-tenant-switch'
import { cn } from '@/lib/utils'

// TenantSwitcher renders only for super_admin users. It lives in the topbar
// immediately right of the global search button and shows:
//   - "System View" when no active tenant context is set (default post-login)
//   - the current tenant's name when an active context is active
//
// Tenants are pulled from GET /api/v1/tenants (the existing super_admin
// endpoint — no new list API). A filter input shows up inside the dropdown
// once the tenant count exceeds 8, so small on-prem deployments with a
// handful of tenants stay friction-free.
export function TenantSwitcher() {
  const { user, activeTenantId } = useAuthStore()
  const activeId = activeTenantId()
  const [filter, setFilter] = useState('')

  const isSuperAdmin = user?.role === 'super_admin'

  const { data: tenants, isLoading } = useTenantList()

  const switchMut = useSwitchTenant()
  const exitMut = useExitTenantContext()

  const activeTenant = useMemo(
    () => tenants?.find((t) => t.id === activeId) ?? null,
    [tenants, activeId],
  )

  const filtered = useMemo(() => {
    if (!tenants) return []
    const q = filter.trim().toLowerCase()
    if (!q) return tenants
    return tenants.filter(
      (t) => t.name.toLowerCase().includes(q) || (t.domain ?? '').toLowerCase().includes(q),
    )
  }, [tenants, filter])

  if (!isSuperAdmin) return null

  const showFilter = (tenants?.length ?? 0) > 8
  const pending = switchMut.isPending || exitMut.isPending

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          'flex items-center gap-2 h-9 px-3 rounded-md border text-sm transition-colors',
          activeTenant
            ? 'border-accent/40 bg-accent/10 text-text-primary hover:bg-accent/15'
            : 'border-border bg-bg-surface text-text-secondary hover:text-text-primary hover:border-text-tertiary',
        )}
        disabled={pending}
        title={activeTenant ? `Viewing as tenant: ${activeTenant.name}` : 'System View — no tenant context'}
      >
        {activeTenant ? (
          <Building2 className="h-3.5 w-3.5 shrink-0 text-accent" />
        ) : (
          <Globe className="h-3.5 w-3.5 shrink-0" />
        )}
        <span className="max-w-[160px] truncate">
          {activeTenant ? activeTenant.name : 'System View'}
        </span>
        <ChevronDown className="h-3.5 w-3.5 shrink-0 opacity-60" />
      </DropdownMenuTrigger>

      <DropdownMenuContent align="start" className="w-72 max-h-[min(70vh,520px)] overflow-auto p-1">
        <div className="px-3 py-2 text-[10px] uppercase tracking-wide text-text-tertiary font-medium">
          Tenant Context
        </div>

        {showFilter && (
          <div className="px-2 pb-2">
            <div className="relative">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
              <Input
                type="text"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter tenants..."
                className="h-8 pl-7 text-sm"
              />
            </div>
          </div>
        )}

        {isLoading && (
          <div className="px-3 py-6 text-center text-sm text-text-tertiary">Loading tenants…</div>
        )}

        {!isLoading && filtered.length === 0 && (
          <div className="px-3 py-6 text-center text-sm text-text-tertiary">
            {filter ? 'No tenants match' : 'No tenants available'}
          </div>
        )}

        {!isLoading &&
          filtered.map((t) => {
            const isActive = t.id === activeId
            const isInactive = t.state !== 'active'
            return (
              <DropdownMenuItem
                key={t.id}
                disabled={isInactive || pending}
                onClick={() => {
                  if (!isActive && !isInactive) switchMut.mutate(t.id)
                }}
                className="flex items-start gap-2 px-3 py-2"
              >
                <Building2
                  className={cn(
                    'h-3.5 w-3.5 mt-0.5 shrink-0',
                    isActive ? 'text-accent' : 'text-text-tertiary',
                  )}
                />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className={cn('text-sm truncate', isActive && 'font-medium text-accent')}>
                      {t.name}
                    </span>
                    {isInactive && (
                      <span className="text-[10px] uppercase tracking-wide text-text-tertiary border border-border rounded px-1">
                        {t.state}
                      </span>
                    )}
                  </div>
                  {t.domain && (
                    <div className="text-[11px] text-text-tertiary truncate">{t.domain}</div>
                  )}
                </div>
                {isActive && <Check className="h-3.5 w-3.5 text-accent shrink-0 mt-1" />}
              </DropdownMenuItem>
            )
          })}

        {activeTenant && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={pending}
              onClick={() => exitMut.mutate()}
              className="flex items-center gap-2 px-3 py-2 text-text-secondary"
            >
              <LogOut className="h-3.5 w-3.5" />
              <span className="text-sm">Exit to System View</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
