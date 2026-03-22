import { useState, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Search,
  Filter,
  X,
  ChevronDown,
  Check,
  RefreshCw,
  AlertCircle,
  Wifi,
  Plus,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { useAPNList } from '@/hooks/use-apns'
import { useOperatorList } from '@/hooks/use-operators'
import type { APN, APNListFilters } from '@/types/apn'
import { cn } from '@/lib/utils'

const RAT_DISPLAY: Record<string, string> = {
  nb_iot: 'NB-IoT',
  lte_m: 'LTE-M',
  lte: 'LTE',
  nr_5g: '5G NR',
}

const APN_TYPE_DISPLAY: Record<string, string> = {
  private_managed: 'Private',
  operator_managed: 'Operator',
  customer_managed: 'Customer',
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

function IPPoolBar({ used, total }: { used: number; total: number }) {
  const pct = total > 0 ? (used / total) * 100 : 0
  const color = pct >= 90 ? 'bg-danger' : pct >= 75 ? 'bg-warning' : 'bg-success'

  return (
    <div className="w-full">
      <div className="flex items-center justify-between text-[10px] mb-1">
        <span className="text-text-tertiary">IP Pool</span>
        <span className="font-mono text-text-secondary">{pct.toFixed(0)}%</span>
      </div>
      <div className="w-full h-1.5 bg-bg-hover rounded-full overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all', color)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
    </div>
  )
}

function APNCard({ apn, operatorName, onClick }: { apn: APN; operatorName: string; onClick: () => void }) {
  const mockSimCount = useMemo(() => Math.floor(Math.random() * 5000) + 100, [])
  const mockTrafficMB = useMemo(() => Math.floor(Math.random() * 50000) + 500, [])
  const mockPoolUsed = useMemo(() => Math.floor(Math.random() * 200) + 10, [])
  const mockPoolTotal = useMemo(() => mockPoolUsed + Math.floor(Math.random() * 100) + 20, [mockPoolUsed])

  return (
    <Card
      className="card-hover cursor-pointer p-4 space-y-3 relative overflow-hidden"
      onClick={onClick}
    >
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-text-primary truncate">{apn.display_name || apn.name}</h3>
          <p className="text-xs text-text-secondary mt-0.5 truncate">{operatorName}</p>
        </div>
        <Badge variant={apn.state === 'active' ? 'success' : 'secondary'} className="text-[10px] flex-shrink-0">
          {apn.state === 'active' && <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse mr-1" />}
          {apn.state.toUpperCase()}
        </Badge>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">SIM Count</span>
          <div className="font-mono text-sm font-semibold text-text-primary">{mockSimCount.toLocaleString()}</div>
        </div>
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">Traffic</span>
          <div className="font-mono text-sm font-semibold text-text-primary">
            {mockTrafficMB >= 1000 ? `${(mockTrafficMB / 1000).toFixed(1)} GB` : `${mockTrafficMB} MB`}
          </div>
        </div>
      </div>

      <IPPoolBar used={mockPoolUsed} total={mockPoolTotal} />

      <div className="flex items-center gap-1.5 flex-wrap">
        <Badge variant="outline" className="text-[10px]">
          {APN_TYPE_DISPLAY[apn.apn_type] ?? apn.apn_type}
        </Badge>
        {apn.supported_rat_types.map((rat) => (
          <span
            key={rat}
            className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium"
          >
            {RAT_DISPLAY[rat] ?? rat}
          </span>
        ))}
      </div>
    </Card>
  )
}

function APNCardSkeleton() {
  return (
    <Card className="p-4 space-y-3">
      <div className="flex justify-between">
        <div>
          <Skeleton className="h-4 w-32 mb-2" />
          <Skeleton className="h-3 w-20" />
        </div>
        <Skeleton className="h-5 w-14" />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <Skeleton className="h-2.5 w-16 mb-1" />
          <Skeleton className="h-4 w-12" />
        </div>
        <div>
          <Skeleton className="h-2.5 w-12 mb-1" />
          <Skeleton className="h-4 w-16" />
        </div>
      </div>
      <Skeleton className="h-3 w-full" />
      <div className="flex gap-1.5">
        <Skeleton className="h-5 w-14" />
        <Skeleton className="h-5 w-12" />
      </div>
    </Card>
  )
}

export default function ApnListPage() {
  const navigate = useNavigate()
  const [filters, setFilters] = useState<APNListFilters>({})
  const [searchInput, setSearchInput] = useState('')

  const { data: operators } = useOperatorList()
  const { data: apns, isLoading, isError, refetch } = useAPNList(filters)

  const operatorMap = useMemo(() => {
    const map = new Map<string, string>()
    operators?.forEach((op) => map.set(op.id, op.name))
    return map
  }, [operators])

  const filteredApns = useMemo(() => {
    if (!apns) return []
    if (!searchInput.trim()) return apns
    const q = searchInput.toLowerCase()
    return apns.filter(
      (a) =>
        a.name.toLowerCase().includes(q) ||
        (a.display_name && a.display_name.toLowerCase().includes(q)),
    )
  }, [apns, searchInput])

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load APNs</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch APN data. Please try again.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">APN Management</h1>
        <Button className="gap-2" size="sm">
          <Plus className="h-4 w-4" />
          Create APN
        </Button>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search by APN name..."
            className="pl-9 h-8 text-sm"
          />
          {searchInput && (
            <button
              onClick={() => setSearchInput('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-[var(--radius-sm)] border transition-colors',
            filters.operator_id
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>
              {filters.operator_id
                ? `Operator: ${operatorMap.get(filters.operator_id) ?? 'Unknown'}`
                : 'All Operators'}
            </span>
            <ChevronDown className="h-3 w-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuItem onClick={() => setFilters((f) => ({ ...f, operator_id: undefined }))}>
              <span className="flex-1">All Operators</span>
              {!filters.operator_id && <Check className="h-3.5 w-3.5 text-accent" />}
            </DropdownMenuItem>
            {operators?.map((op) => (
              <DropdownMenuItem
                key={op.id}
                onClick={() => setFilters((f) => ({ ...f, operator_id: op.id }))}
              >
                <span className="flex-1 truncate">{op.name}</span>
                {filters.operator_id === op.id && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {filters.operator_id && (
          <button
            onClick={() => setFilters({})}
            className="text-xs text-text-tertiary hover:text-accent transition-colors"
          >
            Clear filters
          </button>
        )}
      </div>

      {isLoading && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <APNCardSkeleton key={i} />
          ))}
        </div>
      )}

      {!isLoading && filteredApns.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
            <Wifi className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No APNs configured</h3>
            <p className="text-xs text-text-secondary mb-4">
              {searchInput || filters.operator_id
                ? 'Try adjusting your filters or search terms.'
                : 'Create your first APN to get started.'}
            </p>
            {searchInput || filters.operator_id ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setFilters({})
                  setSearchInput('')
                }}
              >
                Clear Filters
              </Button>
            ) : (
              <Button size="sm" className="gap-2">
                <Plus className="h-3.5 w-3.5" />
                Create APN
              </Button>
            )}
          </div>
        </div>
      )}

      {!isLoading && filteredApns.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {filteredApns.map((apn, i) => (
            <div key={apn.id} style={{ animationDelay: `${i * 50}ms` }} className="animate-in fade-in slide-in-from-bottom-1">
              <APNCard
                apn={apn}
                operatorName={operatorMap.get(apn.operator_id) ?? 'Unknown'}
                onClick={() => navigate(`/apns/${apn.id}`)}
              />
            </div>
          ))}
        </div>
      )}

      {!isLoading && filteredApns.length > 0 && (
        <p className="text-center text-xs text-text-tertiary">
          Showing {filteredApns.length} APN{filteredApns.length !== 1 ? 's' : ''}
        </p>
      )}
    </div>
  )
}
