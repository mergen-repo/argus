import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Search,
  Filter,
  X,
  ChevronDown,
  MoreVertical,
  Pause,
  Play,
  XCircle,
  Shield,
  Loader2,
  Upload,
  SlidersHorizontal,
  Check,
  AlertCircle,
  RefreshCw,
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Spinner } from '@/components/ui/spinner'
import { useSIMList, useSegments, useBulkStateChange } from '@/hooks/use-sims'
import type { SIM, SIMListFilters, SIMState } from '@/types/sim'
import { cn } from '@/lib/utils'

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'ordered', label: 'Ordered' },
  { value: 'active', label: 'Active' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'terminated', label: 'Terminated' },
  { value: 'stolen_lost', label: 'Lost/Stolen' },
]

const RAT_OPTIONS = [
  { value: '', label: 'All RAT' },
  { value: 'nb_iot', label: 'NB-IoT' },
  { value: 'lte_m', label: 'LTE-M' },
  { value: 'lte', label: 'LTE' },
  { value: 'nr_5g', label: '5G NR' },
]

const RAT_DISPLAY: Record<string, string> = {
  nb_iot: 'NB-IoT',
  lte_m: 'LTE-M',
  lte: 'LTE',
  nr_5g: '5G NR',
}

function stateVariant(state: SIMState): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state) {
    case 'active': return 'success'
    case 'suspended': return 'warning'
    case 'terminated': return 'danger'
    case 'stolen_lost': return 'danger'
    case 'ordered': return 'default'
    default: return 'secondary'
  }
}

function stateLabel(state: string): string {
  switch (state) {
    case 'stolen_lost': return 'LOST'
    default: return state.toUpperCase()
  }
}

function detectSearchType(q: string): { field: string; label: string } | null {
  const cleaned = q.replace(/\s/g, '')
  if (/^\d{18,22}$/.test(cleaned)) return { field: 'iccid', label: 'ICCID' }
  if (/^\d{14,15}$/.test(cleaned)) return { field: 'imsi', label: 'IMSI' }
  if (/^\+?\d{10,15}$/.test(cleaned)) return { field: 'msisdn', label: 'MSISDN' }
  return null
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

export default function SimListPage() {
  const navigate = useNavigate()
  const [filters, setFilters] = useState<SIMListFilters>({})
  const [searchInput, setSearchInput] = useState('')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [bulkDialog, setBulkDialog] = useState<{ action: string; label: string } | null>(null)
  const [bulkReason, setBulkReason] = useState('')
  const [selectedSegmentId, setSelectedSegmentId] = useState<string>('')
  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data: segments } = useSegments()
  const bulkMutation = useBulkStateChange()

  const activeFilters = useMemo(() => {
    const applied: { key: string; label: string; value: string }[] = []
    if (filters.state) {
      const opt = STATE_OPTIONS.find((o) => o.value === filters.state)
      applied.push({ key: 'state', label: 'State', value: opt?.label ?? filters.state })
    }
    if (filters.rat_type) {
      applied.push({ key: 'rat_type', label: 'RAT', value: RAT_DISPLAY[filters.rat_type] ?? filters.rat_type })
    }
    if (filters.operator_id) {
      applied.push({ key: 'operator_id', label: 'Operator', value: filters.operator_id })
    }
    if (filters.apn_id) {
      applied.push({ key: 'apn_id', label: 'APN', value: filters.apn_id })
    }
    return applied
  }, [filters])

  const handleSearch = useCallback(() => {
    const trimmed = searchInput.trim()
    if (!trimmed) {
      setFilters((f) => ({ ...f, q: undefined, iccid: undefined, imsi: undefined, msisdn: undefined }))
      return
    }
    const detected = detectSearchType(trimmed)
    if (detected) {
      setFilters((f) => ({
        ...f,
        q: undefined,
        iccid: detected.field === 'iccid' ? trimmed : undefined,
        imsi: detected.field === 'imsi' ? trimmed : undefined,
        msisdn: detected.field === 'msisdn' ? trimmed : undefined,
      }))
    } else {
      setFilters((f) => ({ ...f, q: trimmed, iccid: undefined, imsi: undefined, msisdn: undefined }))
    }
  }, [searchInput])

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useSIMList(filters)

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

    return () => {
      observerRef.current?.disconnect()
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const allSims = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    if (selectedIds.size === allSims.length) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(allSims.map((s) => s.id)))
    }
  }

  const clearFilters = () => {
    setFilters({})
    setSearchInput('')
    setSelectedSegmentId('')
  }

  const removeFilter = (key: string) => {
    setFilters((f) => ({ ...f, [key]: undefined }))
  }

  const handleSegmentSelect = (segId: string) => {
    setSelectedSegmentId(segId)
    if (!segId) {
      setFilters({})
      return
    }
    const seg = segments?.find((s) => s.id === segId)
    if (seg?.filter_definition) {
      const fd = seg.filter_definition as Record<string, string>
      setFilters({
        state: fd.state || undefined,
        operator_id: fd.operator_id || undefined,
        apn_id: fd.apn_id || undefined,
        rat_type: fd.rat_type || undefined,
      })
    }
  }

  const handleBulkAction = async () => {
    if (!bulkDialog) return
    try {
      await bulkMutation.mutateAsync({
        simIds: Array.from(selectedIds),
        targetState: bulkDialog.action,
        reason: bulkReason || undefined,
      })
      setSelectedIds(new Set())
      setBulkDialog(null)
      setBulkReason('')
    } catch {
      // error handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load SIMs</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch SIM data. Please try again.</p>
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
        <h1 className="text-[16px] font-semibold text-text-primary">SIM Management</h1>
        <Button className="gap-2" size="sm">
          <Upload className="h-4 w-4" />
          Import SIMs
        </Button>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-3 flex-wrap">
        {/* Segment Dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger className="flex items-center gap-2 px-3 py-1.5 text-sm font-medium rounded-[var(--radius-sm)] border border-border bg-bg-elevated text-text-primary hover:border-accent transition-colors">
            <SlidersHorizontal className="h-3.5 w-3.5 text-text-tertiary" />
            <span>{selectedSegmentId ? segments?.find((s) => s.id === selectedSegmentId)?.name ?? 'Segment' : 'All SIMs'}</span>
            <ChevronDown className="h-3.5 w-3.5 text-text-tertiary" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuItem onClick={() => handleSegmentSelect('')}>
              All SIMs
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {segments?.map((seg) => (
              <DropdownMenuItem key={seg.id} onClick={() => handleSegmentSelect(seg.id)}>
                <span className="flex-1 truncate">{seg.name}</span>
                {selectedSegmentId === seg.id && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
            {(!segments || segments.length === 0) && (
              <div className="px-2 py-3 text-xs text-text-tertiary text-center">No saved segments</div>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Search */}
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            placeholder="Search ICCID, IMSI, or MSISDN..."
            className="pl-9 h-8 text-sm"
          />
          {searchInput && (
            <button
              onClick={() => {
                setSearchInput('')
                setFilters((f) => ({ ...f, q: undefined, iccid: undefined, imsi: undefined, msisdn: undefined }))
              }}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        {/* State Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.state
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>State{filters.state ? `: ${stateLabel(filters.state)}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {STATE_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setFilters((f) => ({ ...f, state: opt.value || undefined }))}
              >
                <span className="flex-1">{opt.label}</span>
                {filters.state === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* RAT Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.rat_type
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <span>RAT{filters.rat_type ? `: ${RAT_DISPLAY[filters.rat_type] ?? filters.rat_type}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {RAT_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setFilters((f) => ({ ...f, rat_type: opt.value || undefined }))}
              >
                <span className="flex-1">{opt.label}</span>
                {filters.rat_type === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Applied filter chips */}
        {activeFilters.map((af) => (
          <span
            key={af.key}
            className="flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border border-accent/30 bg-accent-dim text-accent"
          >
            {af.label}: {af.value}
            <button onClick={() => removeFilter(af.key)} className="hover:text-text-primary transition-colors">
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}

        {activeFilters.length > 0 && (
          <button
            onClick={clearFilters}
            className="text-xs text-text-tertiary hover:text-accent transition-colors"
          >
            Clear all ({activeFilters.length})
          </button>
        )}
      </div>

      {/* Data Table */}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-10">
                  <input
                    type="checkbox"
                    checked={allSims.length > 0 && selectedIds.size === allSims.length}
                    onChange={toggleSelectAll}
                    className="h-4 w-4 rounded border-border accent-accent cursor-pointer"
                  />
                </TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead>IMSI</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>RAT</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 10 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-28" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-14" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                  </TableRow>
                ))}

              {!isLoading && allSims.length === 0 && (
                <TableRow>
                  <TableCell colSpan={9}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Search className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No SIMs found</h3>
                        <p className="text-xs text-text-secondary mb-4">
                          {activeFilters.length > 0
                            ? 'Try adjusting your filters or search terms.'
                            : 'Import SIMs to get started.'}
                        </p>
                        {activeFilters.length > 0 ? (
                          <Button variant="outline" size="sm" onClick={clearFilters}>
                            Clear Filters
                          </Button>
                        ) : (
                          <Button size="sm" className="gap-2">
                            <Upload className="h-3.5 w-3.5" />
                            Import SIMs
                          </Button>
                        )}
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {allSims.map((sim) => (
                <TableRow
                  key={sim.id}
                  data-state={selectedIds.has(sim.id) ? 'selected' : undefined}
                  className="cursor-pointer"
                  onClick={() => navigate(`/sims/${sim.id}`)}
                >
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      checked={selectedIds.has(sim.id)}
                      onChange={() => toggleSelect(sim.id)}
                      className="h-4 w-4 rounded border-border accent-accent cursor-pointer"
                    />
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-accent hover:underline">
                      {sim.iccid}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{sim.imsi}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">
                      {sim.msisdn ?? '-'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(sim.state)} className="gap-1">
                      {sim.state === 'active' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {stateLabel(sim.state)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary capitalize">{sim.sim_type}</span>
                  </TableCell>
                  <TableCell>
                    {sim.rat_type ? (
                      <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium">
                        {RAT_DISPLAY[sim.rat_type] ?? sim.rat_type}
                      </span>
                    ) : (
                      <span className="text-text-tertiary text-xs">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(sim.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <DropdownMenu>
                      <DropdownMenuTrigger className="p-1 text-text-tertiary hover:text-text-primary transition-colors rounded-[var(--radius-sm)] hover:bg-bg-hover">
                        <MoreVertical className="h-4 w-4" />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent>
                        <DropdownMenuItem onClick={() => navigate(`/sims/${sim.id}`)}>
                          View Details
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        {sim.state === 'ordered' && (
                          <DropdownMenuItem onClick={() => navigate(`/sims/${sim.id}`)}>
                            Activate
                          </DropdownMenuItem>
                        )}
                        {sim.state === 'active' && (
                          <DropdownMenuItem onClick={() => navigate(`/sims/${sim.id}`)}>
                            Suspend
                          </DropdownMenuItem>
                        )}
                        {sim.state === 'suspended' && (
                          <DropdownMenuItem onClick={() => navigate(`/sims/${sim.id}`)}>
                            Resume
                          </DropdownMenuItem>
                        )}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {/* Bulk Action Bar */}
        {selectedIds.size > 0 && (
          <div className="flex items-center gap-3 px-4 py-2.5 bg-accent-dim border-t border-accent/20 animate-in slide-in-from-bottom-1">
            <span className="text-sm font-semibold text-accent">
              {selectedIds.size} selected
            </span>
            <div className="flex gap-2 ml-2">
              <Button
                variant="secondary"
                size="sm"
                className="text-xs gap-1.5"
                onClick={() => setBulkDialog({ action: 'suspended', label: 'Suspend' })}
              >
                <Pause className="h-3 w-3" /> Suspend
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="text-xs gap-1.5"
                onClick={() => setBulkDialog({ action: 'active', label: 'Resume' })}
              >
                <Play className="h-3 w-3" /> Resume
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="text-xs gap-1.5"
                onClick={() => navigate('/policies')}
              >
                <Shield className="h-3 w-3" /> Assign Policy
              </Button>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="text-xs gap-1.5 ml-auto border-danger/30 text-danger hover:bg-danger-dim"
              onClick={() => setBulkDialog({ action: 'terminated', label: 'Terminate' })}
            >
              <XCircle className="h-3 w-3" /> Terminate
            </Button>
          </div>
        )}

        {/* Load More / Infinite Scroll Trigger */}
        <div ref={loadMoreRef} className="px-4 py-3 border-t border-border-subtle">
          {isFetchingNextPage ? (
            <div className="flex items-center justify-center gap-2 text-text-tertiary text-xs">
              <Spinner className="h-3.5 w-3.5" />
              Loading more...
            </div>
          ) : hasNextPage ? (
            <button
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1"
            >
              Load more SIMs
            </button>
          ) : allSims.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allSims.length} SIMs
            </p>
          ) : null}
        </div>
      </Card>

      {/* Saved Segments */}
      {segments && segments.length > 0 && (
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-xs text-text-tertiary mr-1">Saved:</span>
          {segments.map((seg) => (
            <button
              key={seg.id}
              onClick={() => handleSegmentSelect(seg.id === selectedSegmentId ? '' : seg.id)}
              className={cn(
                'px-3 py-1 text-xs rounded-full border transition-colors',
                selectedSegmentId === seg.id
                  ? 'bg-accent-dim border-accent/30 text-accent'
                  : 'bg-bg-surface border-border text-text-secondary hover:border-accent hover:text-accent',
              )}
            >
              {seg.name}
            </button>
          ))}
        </div>
      )}

      {/* Bulk Action Confirmation Dialog */}
      <Dialog open={!!bulkDialog} onOpenChange={() => setBulkDialog(null)}>
        <DialogContent onClose={() => setBulkDialog(null)}>
          <DialogHeader>
            <DialogTitle>
              {bulkDialog?.label} {selectedIds.size} SIM{selectedIds.size !== 1 ? 's' : ''}?
            </DialogTitle>
            <DialogDescription>
              This action will {bulkDialog?.label.toLowerCase()} the selected SIMs. This may take a moment for large selections.
            </DialogDescription>
          </DialogHeader>
          <div className="py-2">
            <label className="text-xs font-medium text-text-secondary block mb-1.5">
              Reason (optional)
            </label>
            <Input
              value={bulkReason}
              onChange={(e) => setBulkReason(e.target.value)}
              placeholder="Enter reason..."
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBulkDialog(null)}>
              Cancel
            </Button>
            <Button
              variant={bulkDialog?.action === 'terminated' ? 'destructive' : 'default'}
              onClick={handleBulkAction}
              disabled={bulkMutation.isPending}
              className="gap-2"
            >
              {bulkMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              {bulkDialog?.label}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
