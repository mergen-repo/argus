import { useState, useMemo, useEffect, useRef } from 'react'
import {
  Search,
  Filter,
  X,
  Check,
  RefreshCw,
  AlertCircle,
  Shield,
  ShieldCheck,
  ShieldAlert,
  ChevronDown,
  ChevronRight,
  Loader2,
  Calendar,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
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
import { Spinner } from '@/components/ui/spinner'
import { useAuditList, useVerifyAuditChain } from '@/hooks/use-audit'
import type { AuditFilters } from '@/hooks/use-audit'
import { useUserList } from '@/hooks/use-settings'
import type { AuditLog } from '@/types/audit'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { timeAgo } from '@/lib/format'

const ACTION_OPTIONS = [
  { value: '', label: 'All Actions' },
  { value: 'sim.create', label: 'SIM Create' },
  { value: 'sim.activate', label: 'SIM Activate' },
  { value: 'sim.suspend', label: 'SIM Suspend' },
  { value: 'sim.resume', label: 'SIM Resume' },
  { value: 'sim.terminate', label: 'SIM Terminate' },
  { value: 'policy.create', label: 'Policy Create' },
  { value: 'policy.update', label: 'Policy Update' },
  { value: 'esim_profile.enable', label: 'eSIM Enable' },
  { value: 'esim_profile.disable', label: 'eSIM Disable' },
  { value: 'esim_profile.switch', label: 'eSIM Switch' },
  { value: 'session.disconnect', label: 'Session Disconnect' },
  { value: 'user.create', label: 'User Create' },
  { value: 'user.update', label: 'User Update' },
]

const ENTITY_TYPE_OPTIONS = [
  { value: '', label: 'All Entities' },
  { value: 'sim', label: 'SIM' },
  { value: 'policy', label: 'Policy' },
  { value: 'esim_profile', label: 'eSIM Profile' },
  { value: 'session', label: 'Session' },
  { value: 'operator', label: 'Operator' },
  { value: 'apn', label: 'APN' },
  { value: 'user', label: 'User' },
  { value: 'tenant', label: 'Tenant' },
]

function actionVariant(action: string): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  if (action.includes('create') || action.includes('activate') || action.includes('enable')) return 'success'
  if (action.includes('suspend') || action.includes('disable')) return 'warning'
  if (action.includes('terminate') || action.includes('delete') || action.includes('disconnect')) return 'danger'
  return 'secondary'
}

function JsonDiffView({ data }: { data: unknown }) {
  if (!data) return <span className="text-xs text-text-tertiary">No data</span>

  const formatted = typeof data === 'string' ? data : JSON.stringify(data, null, 2)

  return (
    <pre className="text-[11px] font-mono bg-bg-primary p-3 rounded-[var(--radius-sm)] border border-border overflow-x-auto max-h-64 text-text-secondary whitespace-pre-wrap break-all">
      {formatted}
    </pre>
  )
}

function ExpandableRow({ entry }: { entry: AuditLog }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-bg-hover"
        onClick={() => setExpanded(!expanded)}
      >
        <TableCell className="w-8">
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5 text-text-tertiary" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 text-text-tertiary" />
          )}
        </TableCell>
        <TableCell>
          <Badge variant={actionVariant(entry.action)} className="text-[10px]">
            {entry.action}
          </Badge>
        </TableCell>
        <TableCell>
          <span className="text-xs text-text-secondary">
            {entry.user_id?.slice(0, 8) ?? 'system'}
          </span>
        </TableCell>
        <TableCell>
          <span className="text-xs text-text-secondary">{entry.entity_type}</span>
        </TableCell>
        <TableCell>
          <span className="font-mono text-xs text-text-tertiary">{entry.entity_id.slice(0, 8)}</span>
        </TableCell>
        <TableCell>
          <span className="text-xs text-text-secondary" title={new Date(entry.created_at).toLocaleString()}>
            {timeAgo(entry.created_at)}
          </span>
        </TableCell>
        <TableCell>
          <span className="font-mono text-xs text-text-tertiary">{entry.ip_address ?? '-'}</span>
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="bg-bg-surface">
          <TableCell colSpan={7}>
            <div className="px-4 py-3 space-y-3">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <p className="text-[10px] uppercase tracking-[1px] text-text-tertiary mb-1.5 font-medium">Full Details</p>
                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div>
                      <span className="text-text-tertiary">ID:</span>
                      <span className="ml-1 font-mono text-text-secondary">{entry.id}</span>
                    </div>
                    <div>
                      <span className="text-text-tertiary">Entity ID:</span>
                      <span className="ml-1 font-mono text-text-secondary">{entry.entity_id}</span>
                    </div>
                    <div>
                      <span className="text-text-tertiary">User:</span>
                      <span className="ml-1 font-mono text-text-secondary">{entry.user_id ?? 'system'}</span>
                    </div>
                    <div>
                      <span className="text-text-tertiary">Time:</span>
                      <span className="ml-1 text-text-secondary">{new Date(entry.created_at).toLocaleString()}</span>
                    </div>
                  </div>
                </div>
              </div>
              {entry.diff != null && (
                <div>
                  <p className="text-[10px] uppercase tracking-[1px] text-text-tertiary mb-1.5 font-medium">Changes (JSON Diff)</p>
                  <JsonDiffView data={entry.diff} />
                </div>
              )}
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

export default function AuditLogPage() {
  const [filters, setFilters] = useState<AuditFilters>({})
  const [searchInput, setSearchInput] = useState('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [verifying, setVerifying] = useState(false)
  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data: usersData } = useUserList()
  const users = usersData ?? []

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useAuditList(filters)

  const { data: verifyResult, isLoading: isVerifying, refetch: runVerify } = useVerifyAuditChain(verifying)

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el) return
    observerRef.current = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 },
    )
    observerRef.current.observe(el)
    return () => { observerRef.current?.disconnect() }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const allEntries = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  const handleSearch = () => {
    const trimmed = searchInput.trim()
    setFilters((f) => ({
      ...f,
      entity_id: trimmed || undefined,
    }))
  }

  const handleDateFilter = () => {
    setFilters((f) => ({
      ...f,
      from: dateFrom || undefined,
      to: dateTo || undefined,
    }))
  }

  const handleVerify = () => {
    setVerifying(true)
    runVerify()
  }

  const activeFilterCount = [filters.action, filters.entity_type, filters.entity_id, filters.from, filters.user_id].filter(Boolean).length

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load audit logs</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch audit data. Please try again.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Audit Log</h1>
        <Button
          variant="outline"
          size="sm"
          className="gap-2"
          onClick={handleVerify}
          disabled={isVerifying}
        >
          {isVerifying ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Shield className="h-3.5 w-3.5" />
          )}
          Verify Integrity
        </Button>
      </div>

      {/* Verify Result Banner */}
      {verifying && verifyResult && (
        <div className={cn(
          'flex items-center gap-3 px-4 py-3 rounded-[var(--radius-md)] border',
          verifyResult.verified
            ? 'bg-success-dim border-success/30'
            : 'bg-danger-dim border-danger/30',
        )}>
          {verifyResult.verified ? (
            <ShieldCheck className="h-5 w-5 text-success flex-shrink-0" />
          ) : (
            <ShieldAlert className="h-5 w-5 text-danger flex-shrink-0" />
          )}
          <div>
            <p className={cn('text-sm font-medium', verifyResult.verified ? 'text-success' : 'text-danger')}>
              {verifyResult.verified ? 'Hash chain valid' : 'Tamper detected!'}
            </p>
            <p className="text-xs text-text-secondary">
              {verifyResult.entries_checked.toLocaleString()} entries checked
              {verifyResult.first_invalid && ` (first invalid entry: #${verifyResult.first_invalid})`}
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Dismiss"
            onClick={() => setVerifying(false)}
            className="ml-auto text-text-tertiary hover:text-text-primary transition-colors h-6 w-6"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      )}

      {/* Filter Bar */}
      <div className="flex items-center gap-3 flex-wrap">
        {/* Search Entity ID */}
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            placeholder="Search entity ID..."
            className="pl-9 h-8 text-sm"
          />
          {searchInput && (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Clear search"
              onClick={() => { setSearchInput(''); setFilters((f) => ({ ...f, entity_id: undefined })) }}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors h-5 w-5"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>

        {/* Action Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.action
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>Action{filters.action ? `: ${filters.action}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="max-h-64 overflow-y-auto">
            {ACTION_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setFilters((f) => ({ ...f, action: opt.value || undefined }))}
              >
                <span className="flex-1">{opt.label}</span>
                {filters.action === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Entity Type Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.entity_type
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <span>Entity{filters.entity_type ? `: ${filters.entity_type}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {ENTITY_TYPE_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setFilters((f) => ({ ...f, entity_type: opt.value || undefined }))}
              >
                <span className="flex-1">{opt.label}</span>
                {filters.entity_type === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* User Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.user_id
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>
              User{filters.user_id
                ? `: ${users.find((u) => u.id === filters.user_id)?.name ?? filters.user_id.slice(0, 8)}`
                : ''}
            </span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="max-h-64 overflow-y-auto">
            <DropdownMenuItem
              onClick={() => setFilters((f) => ({ ...f, user_id: undefined }))}
            >
              <span className="flex-1">All Users</span>
              {!filters.user_id && <Check className="h-3.5 w-3.5 text-accent" />}
            </DropdownMenuItem>
            {users.map((u) => (
              <DropdownMenuItem
                key={u.id}
                onClick={() => setFilters((f) => ({ ...f, user_id: u.id }))}
              >
                <span className="flex-1">{u.name || u.email}</span>
                {filters.user_id === u.id && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Date Range */}
        <div className="flex items-center gap-1.5">
          <Calendar className="h-3.5 w-3.5 text-text-tertiary" />
          <Input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            className="h-7 px-2 text-xs w-auto"
          />
          <span className="text-text-tertiary text-xs">to</span>
          <Input
            type="date"
            value={dateTo}
            onChange={(e) => setDateTo(e.target.value)}
            className="h-7 px-2 text-xs w-auto"
          />
          {(dateFrom || dateTo) && (
            <Button variant="outline" size="sm" className="h-7 px-2 text-xs" onClick={handleDateFilter}>
              Apply
            </Button>
          )}
        </div>

        {activeFilterCount > 0 && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => { setFilters({}); setSearchInput(''); setDateFrom(''); setDateTo('') }}
            className="text-xs text-text-tertiary hover:text-accent h-auto py-0 px-1"
          >
            Clear all ({activeFilterCount})
          </Button>
        )}
      </div>

      {/* Audit Table */}
      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-8" />
                <TableHead>Action</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Entity Type</TableHead>
                <TableHead>Entity ID</TableHead>
                <TableHead>Time</TableHead>
                <TableHead>IP Address</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 8 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 7 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && allEntries.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Shield className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No audit logs found</h3>
                        <p className="text-xs text-text-secondary">
                          {activeFilterCount > 0 ? 'Try adjusting your filters.' : 'Audit entries will appear here as actions are performed.'}
                        </p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {allEntries.map((entry) => (
                <ExpandableRow key={entry.id} entry={entry} />
              ))}
            </TableBody>
          </Table>
        </div>

        <div ref={loadMoreRef} className="px-4 py-3 border-t border-border-subtle">
          {isFetchingNextPage ? (
            <div className="flex items-center justify-center gap-2 text-text-tertiary text-xs">
              <Spinner className="h-3.5 w-3.5" />
              Loading more...
            </div>
          ) : hasNextPage ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent py-1 h-auto"
            >
              Load more entries
            </Button>
          ) : allEntries.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allEntries.length} entries
            </p>
          ) : null}
        </div>
      </Card>
    </div>
  )
}
