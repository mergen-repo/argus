import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  AlertTriangle,
  AlertCircle,
  Database,
  RefreshCw,
  Search,
  X,
  ChevronDown,
  ExternalLink,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { useTenantUsage } from '@/hooks/use-tenant-usage'
import { TenantUsageCard } from '@/components/admin/tenant-usage-card'
import { TenantUsageDetailPanel } from '@/components/admin/tenant-usage-detail-panel'
import { useAuthStore } from '@/stores/auth'
import { hasMinRole } from '@/lib/rbac'
import { formatBytes, formatNumber, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { TenantUsageItem, TenantPlan, TenantState } from '@/types/admin'

// ── URL param keys ─────────────────────────────────────────────────────────────
const PARAM_VIEW = 'view'
const PARAM_Q = 'q'
const PARAM_SORT = 'sort'
const PARAM_STATE = 'state'
const PARAM_PLAN = 'plan'
const PARAM_BREACH = 'breach'

type ViewMode = 'cards' | 'table'
type SortKey = 'util' | 'name' | 'plan'
type BreachThreshold = 'all' | '50' | '80' | '95'

const SORT_LABELS: Record<SortKey, string> = {
  util: 'Utilization (high → low)',
  name: 'Name (A → Z)',
  plan: 'Plan',
}
const PLAN_LABELS: Record<string, string> = {
  all: 'All plans',
  starter: 'Starter',
  standard: 'Standard',
  enterprise: 'Enterprise',
}
const STATE_LABELS: Record<string, string> = {
  all: 'All states',
  active: 'Active',
  suspended: 'Suspended',
  trial: 'Trial',
}
const BREACH_LABELS: Record<BreachThreshold, string> = {
  all: 'All utilization',
  '50': '> 50%',
  '80': '> 80%',
  '95': '> 95%',
}

const PLAN_VARIANT: Record<TenantPlan, 'default' | 'success' | 'warning'> = {
  starter: 'default',
  standard: 'success',
  enterprise: 'warning',
}
const STATE_VARIANT: Record<TenantState, 'success' | 'danger' | 'warning'> = {
  active: 'success',
  suspended: 'danger',
  trial: 'warning',
}

function maxPct(item: TenantUsageItem): number {
  return Math.max(
    item.sims.pct,
    item.sessions.pct,
    item.api_rps.pct,
    item.storage_bytes.pct,
  )
}

function MetricDot({ pct }: { pct: number }) {
  if (pct >= 95)
    return <span className="h-2 w-2 rounded-full bg-danger animate-pulse inline-block" />
  if (pct >= 80)
    return <span className="h-2 w-2 rounded-full bg-warning animate-pulse inline-block" />
  if (pct >= 50)
    return <span className="h-2 w-2 rounded-full bg-warning/60 inline-block" />
  return <span className="h-2 w-2 rounded-full bg-success inline-block" />
}

// ── Breach history summary ──────────────────────────────────────────────────
function BreachSection({ items }: { items: TenantUsageItem[] }) {
  const breachingTenants = useMemo(
    () =>
      items
        .filter((t) => t.open_breach_count > 0)
        .sort((a, b) => b.open_breach_count - a.open_breach_count),
    [items],
  )
  const totalBreaches = breachingTenants.reduce((s, t) => s + t.open_breach_count, 0)

  if (totalBreaches === 0) {
    return (
      <div className="flex items-center gap-3 rounded-[var(--radius-card)] border border-success/20 bg-success-dim px-4 py-3">
        <span className="h-2 w-2 rounded-full bg-success shrink-0" />
        <p className="text-sm text-text-secondary">
          No active breach events in the last 30 days.
        </p>
      </div>
    )
  }

  return (
    <div className="rounded-[var(--radius-card)] border border-danger/30 bg-danger-dim overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3 border-b border-danger/20">
        <AlertTriangle className="h-4 w-4 text-danger shrink-0" />
        <span className="text-sm font-semibold text-danger">
          Past 30 days — {totalBreaches} active breach{totalBreaches > 1 ? 'es' : ''} across{' '}
          {breachingTenants.length} tenant{breachingTenants.length > 1 ? 's' : ''}
        </span>
        <Link
          to="/alerts?type=quota.breach"
          className={cn(
            buttonVariants({ variant: 'ghost', size: 'sm' }),
            'ml-auto h-auto px-2 py-0.5 text-xs text-accent hover:text-accent shrink-0',
          )}
        >
          View in Alerts <ExternalLink className="ml-0.5 h-3 w-3" />
        </Link>
      </div>
      <div className="divide-y divide-border">
        {breachingTenants.slice(0, 5).map((t) => (
          <div
            key={t.tenant_id}
            className="flex items-center gap-3 px-4 py-2.5 text-sm"
          >
            <span className="h-2 w-2 rounded-full bg-danger shrink-0" />
            <span className="font-medium text-text-primary truncate flex-1">
              {t.tenant_name}
            </span>
            <Badge variant={PLAN_VARIANT[t.plan]} className="capitalize shrink-0">
              {t.plan}
            </Badge>
            <span className="text-text-secondary tabular-nums shrink-0">
              {t.open_breach_count} open
            </span>
            <Link
              to={`/alerts?type=quota.breach&tenant_id=${t.tenant_id}`}
              onClick={(e) => e.stopPropagation()}
              className={cn(
                buttonVariants({ variant: 'ghost', size: 'sm' }),
                'h-auto px-1.5 py-0.5 text-xs text-accent hover:text-accent shrink-0',
              )}
            >
              Events <ExternalLink className="ml-0.5 h-3 w-3" />
            </Link>
          </div>
        ))}
      </div>
    </div>
  )
}

// ── Loading skeletons ───────────────────────────────────────────────────────
function CardSkeletons() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="rounded-[var(--radius-card)] border border-border bg-bg-surface p-4 space-y-3">
          <div className="flex items-start justify-between gap-2">
            <div className="space-y-2 flex-1">
              <Skeleton className="h-4 w-2/3" />
              <div className="flex gap-1.5">
                <Skeleton className="h-4 w-14 rounded-full" />
                <Skeleton className="h-4 w-14 rounded-full" />
              </div>
            </div>
            <Skeleton className="h-4 w-12" />
          </div>
          <div className="space-y-2.5">
            {Array.from({ length: 4 }).map((_, j) => (
              <div key={j} className="space-y-1">
                <div className="flex justify-between">
                  <Skeleton className="h-3 w-14" />
                  <Skeleton className="h-3 w-16" />
                </div>
                <Skeleton className="h-2 w-full rounded-full" />
              </div>
            ))}
          </div>
          <div className="flex items-center justify-between pt-1 border-t border-border">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-5 w-20 rounded" />
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Empty state ─────────────────────────────────────────────────────────────
function EmptyState({ hasFilters, onClear }: { hasFilters: boolean; onClear: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <div className="rounded-[var(--radius-card)] border border-border bg-bg-surface p-10">
        <Database className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
        <p className="text-sm font-medium text-text-primary mb-1">
          {hasFilters ? 'No tenants match your filters' : 'No tenants found'}
        </p>
        <p className="text-xs text-text-tertiary mb-4">
          {hasFilters
            ? 'Try adjusting the search or filter criteria.'
            : 'Tenant data will appear once tenants are provisioned.'}
        </p>
        {hasFilters && (
          <Button variant="outline" size="sm" onClick={onClear}>
            Clear filters
          </Button>
        )}
      </div>
    </div>
  )
}

// ── Error state ─────────────────────────────────────────────────────────────
function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex items-center gap-3 rounded-[var(--radius-card)] border border-danger/30 bg-danger-dim px-4 py-3">
      <AlertCircle className="h-4 w-4 text-danger shrink-0" />
      <p className="text-sm text-danger flex-1">Failed to load tenant usage data.</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

// ── Filter dropdown ─────────────────────────────────────────────────────────
function FilterDropdown<T extends string>({
  value,
  options,
  labels,
  onChange,
}: {
  value: T
  options: T[]
  labels: Record<string, string>
  onChange: (v: T) => void
}) {
  const isActive = value !== options[0]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className={cn(
            'h-8 text-xs gap-1.5',
            isActive && 'border-accent-primary/60 text-accent-primary',
          )}
        >
          {labels[value]}
          <ChevronDown className="h-3 w-3 text-text-tertiary" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-36">
        {options.map((o) => (
          <DropdownMenuItem
            key={o}
            onClick={() => onChange(o)}
            className={cn('text-xs', o === value && 'text-accent-primary font-medium')}
          >
            {labels[o]}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// ── Table view ─────────────────────────────────────────────────────────────
function TableView({
  items,
  onRowClick,
}: {
  items: TenantUsageItem[]
  onRowClick: (item: TenantUsageItem) => void
}) {
  return (
    <Card className="bg-bg-surface border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Tenant</TableHead>
            <TableHead>Plan</TableHead>
            <TableHead>State</TableHead>
            <TableHead className="text-right">SIMs</TableHead>
            <TableHead className="text-right">Sessions</TableHead>
            <TableHead className="text-right">API RPS</TableHead>
            <TableHead className="text-right">Storage</TableHead>
            <TableHead className="text-center">Breach</TableHead>
            <TableHead className="text-right">Edit</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((t) => {
            const pct = maxPct(t)
            return (
              <TableRow
                key={t.tenant_id}
                className="cursor-pointer hover:bg-bg-elevated transition-colors"
                onClick={() => onRowClick(t)}
              >
                <TableCell>
                  <span className="font-medium text-text-primary text-sm">{t.tenant_name}</span>
                </TableCell>
                <TableCell>
                  <Badge variant={PLAN_VARIANT[t.plan]} className="capitalize text-xs">
                    {t.plan}
                  </Badge>
                </TableCell>
                <TableCell>
                  <Badge variant={STATE_VARIANT[t.state]} className="uppercase text-xs">
                    {t.state}
                  </Badge>
                </TableCell>
                <TableCell className="text-right tabular-nums text-xs text-text-secondary">
                  {formatNumber(t.sims.current)}{' '}
                  <span className="text-text-tertiary">/ {formatNumber(t.sims.max)}</span>
                </TableCell>
                <TableCell className="text-right tabular-nums text-xs text-text-secondary">
                  {formatNumber(t.sessions.current)}{' '}
                  <span className="text-text-tertiary">/ {formatNumber(t.sessions.max)}</span>
                </TableCell>
                <TableCell className="text-right tabular-nums text-xs text-text-secondary">
                  {t.api_rps.current.toFixed(1)}{' '}
                  <span className="text-text-tertiary">/ {t.api_rps.max}</span>
                </TableCell>
                <TableCell className="text-right tabular-nums text-xs text-text-secondary">
                  {formatBytes(t.storage_bytes.current)}
                </TableCell>
                <TableCell className="text-center">
                  <div className="flex items-center justify-center gap-1.5">
                    <MetricDot pct={pct} />
                    {t.open_breach_count > 0 && (
                      <span className="text-xs text-danger tabular-nums">
                        {t.open_breach_count}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell className="text-right">
                  <Link
                    to={`/system/tenants/${t.tenant_id}`}
                    onClick={(e) => e.stopPropagation()}
                    className={cn(
                      buttonVariants({ variant: 'ghost', size: 'sm' }),
                      'h-auto px-1.5 py-0.5 text-xs text-accent hover:text-accent',
                    )}
                  >
                    Edit
                  </Link>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </Card>
  )
}

// ── Main page ───────────────────────────────────────────────────────────────
export default function TenantUsagePage() {
  const user = useAuthStore((s) => s.user)
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedTenant, setSelectedTenant] = useState<TenantUsageItem | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)

  const view = (searchParams.get(PARAM_VIEW) as ViewMode) ?? 'cards'
  const q = searchParams.get(PARAM_Q) ?? ''
  const sort = (searchParams.get(PARAM_SORT) as SortKey) ?? 'util'
  const stateFilter = (searchParams.get(PARAM_STATE) ?? 'all') as TenantState | 'all'
  const planFilter = (searchParams.get(PARAM_PLAN) ?? 'all') as TenantPlan | 'all'
  const breachFilter = (searchParams.get(PARAM_BREACH) ?? 'all') as BreachThreshold

  function setParam(key: string, value: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value === 'all' || value === '' || value === 'cards' || (key === PARAM_SORT && value === 'util')) {
          next.delete(key)
        } else {
          next.set(key, value)
        }
        return next
      },
      { replace: true },
    )
  }

  function clearFilters() {
    setSearchParams({}, { replace: true })
  }

  const { data, isLoading, isError, refetch } = useTenantUsage()

  const lastFetchedAt = useRef<Date | null>(null)
  const [lastUpdated, setLastUpdated] = useState<string | null>(null)

  useEffect(() => {
    if (!isLoading && !isError) {
      lastFetchedAt.current = new Date()
      setLastUpdated(timeAgo(lastFetchedAt.current.toISOString()))
    }
  }, [isLoading, isError, data])

  const handleRefetch = useCallback(async () => {
    await refetch()
  }, [refetch])

  const hasFilters =
    q !== '' ||
    stateFilter !== 'all' ||
    planFilter !== 'all' ||
    breachFilter !== 'all'

  const filteredAndSorted = useMemo(() => {
    let items = [...data]

    if (q) {
      const lower = q.toLowerCase()
      items = items.filter(
        (t) =>
          t.tenant_name.toLowerCase().includes(lower) ||
          t.tenant_id.toLowerCase().includes(lower),
      )
    }
    if (stateFilter !== 'all') {
      items = items.filter((t) => t.state === stateFilter)
    }
    if (planFilter !== 'all') {
      items = items.filter((t) => t.plan === planFilter)
    }
    if (breachFilter !== 'all') {
      const threshold = parseInt(breachFilter, 10)
      items = items.filter((t) => maxPct(t) > threshold)
    }

    items.sort((a, b) => {
      if (sort === 'util') return maxPct(b) - maxPct(a)
      if (sort === 'name') return a.tenant_name.localeCompare(b.tenant_name)
      if (sort === 'plan') {
        const order: Record<TenantPlan, number> = { starter: 0, standard: 1, enterprise: 2 }
        return order[b.plan] - order[a.plan]
      }
      return 0
    })
    return items
  }, [data, q, stateFilter, planFilter, breachFilter, sort])

  function openPanel(item: TenantUsageItem) {
    setSelectedTenant(item)
    setPanelOpen(true)
  }

  if (!hasMinRole(user?.role, 'super_admin')) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-[var(--radius-card)] border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">super_admin role required.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-5 p-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Tenant Usage</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Quota utilization and resource consumption across all tenants
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {lastUpdated && (
            <span className="text-xs text-text-tertiary hidden sm:inline">
              Updated {lastUpdated}
            </span>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => { void handleRefetch() }}
            aria-label="Refresh tenant usage data"
            className="h-8 w-8 p-0"
          >
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Breach history section (AC-12) */}
      {!isLoading && !isError && <BreachSection items={data} />}

      {/* Error state */}
      {isError && <ErrorState onRetry={() => { void handleRefetch() }} />}

      {/* Toolbar (AC-4) */}
      <div className="flex flex-wrap items-center gap-2">
        {/* Search */}
        <div className="relative flex-1 min-w-48 max-w-72">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
          <Input
            value={q}
            onChange={(e) => setParam(PARAM_Q, e.target.value)}
            placeholder="Search tenants…"
            className="pl-8 h-8 text-xs"
            aria-label="Search tenants"
          />
          {q && (
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setParam(PARAM_Q, '')}
              className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6 text-text-tertiary hover:text-text-secondary"
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>

        {/* Sort */}
        <FilterDropdown<SortKey>
          value={sort}
          options={['util', 'name', 'plan']}
          labels={SORT_LABELS}
          onChange={(v) => setParam(PARAM_SORT, v)}
        />

        {/* State filter */}
        <FilterDropdown<TenantState | 'all'>
          value={stateFilter}
          options={['all', 'active', 'suspended', 'trial']}
          labels={STATE_LABELS}
          onChange={(v) => setParam(PARAM_STATE, v)}
        />

        {/* Plan filter */}
        <FilterDropdown<TenantPlan | 'all'>
          value={planFilter}
          options={['all', 'starter', 'standard', 'enterprise']}
          labels={PLAN_LABELS}
          onChange={(v) => setParam(PARAM_PLAN, v)}
        />

        {/* Breach threshold filter */}
        <FilterDropdown<BreachThreshold>
          value={breachFilter}
          options={['all', '50', '80', '95']}
          labels={BREACH_LABELS}
          onChange={(v) => setParam(PARAM_BREACH, v)}
        />

        {/* Spacer */}
        <div className="flex-1" />

        {hasFilters && (
          <Button
            variant="ghost"
            size="sm"
            onClick={clearFilters}
            className="h-8 text-xs text-text-tertiary hover:text-text-secondary gap-1"
          >
            <X className="h-3 w-3" />
            Clear
          </Button>
        )}

        {/* View toggle */}
        <div className="flex rounded-lg border border-border overflow-hidden">
          {(['cards', 'table'] as const).map((v) => (
            <Button
              key={v}
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setParam(PARAM_VIEW, v)}
              className={cn(
                'rounded-none px-3 py-1.5 text-xs h-auto capitalize',
                view === v
                  ? 'bg-accent-dim text-accent'
                  : 'text-text-secondary hover:text-text-primary',
              )}
            >
              {v}
            </Button>
          ))}
        </div>
      </div>

      {/* Content area */}
      {isLoading ? (
        <CardSkeletons />
      ) : filteredAndSorted.length === 0 ? (
        <EmptyState hasFilters={hasFilters} onClear={clearFilters} />
      ) : view === 'cards' ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {filteredAndSorted.map((item) => (
            <TenantUsageCard
              key={item.tenant_id}
              item={item}
              onClick={() => openPanel(item)}
            />
          ))}
        </div>
      ) : (
        <TableView items={filteredAndSorted} onRowClick={openPanel} />
      )}

      {/* SlidePanel */}
      <TenantUsageDetailPanel
        tenant={selectedTenant}
        open={panelOpen}
        onClose={() => setPanelOpen(false)}
      />
    </div>
  )
}
