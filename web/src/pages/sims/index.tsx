import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
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
  ClipboardPaste,
  FileUp,
  Globe,
  SlidersHorizontal,
  Check,
  AlertCircle,
  RefreshCw,
  GitCompareArrows,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Spinner } from '@/components/ui/spinner'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import { useSIMList, useSegments, useSegmentCount, useBulkStateChange, useBulkPolicyAssign, useImportSIMs } from '@/hooks/use-sims'
import { usePolicyList } from '@/hooks/use-policies'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
import { useOperatorList } from '@/hooks/use-operators'
import { useAPNList } from '@/hooks/use-apns'
import type { SIM, SIMListFilters, SIMState } from '@/types/sim'
import { cn } from '@/lib/utils'
import { RAT_DISPLAY, RAT_OPTIONS } from '@/lib/constants'
import { stateVariant, stateLabel } from '@/lib/sim-utils'
import { RATBadge } from '@/components/ui/rat-badge'

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'ordered', label: 'Ordered' },
  { value: 'active', label: 'Active' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'terminated', label: 'Terminated' },
  { value: 'stolen_lost', label: 'Lost/Stolen' },
]

function detectSearchType(q: string): { field: string; label: string } | null {
  const cleaned = q.replace(/\s/g, '')
  if (/^\d{18,22}$/.test(cleaned)) return { field: 'iccid', label: 'ICCID' }
  if (/^\d{14,15}$/.test(cleaned)) return { field: 'imsi', label: 'IMSI' }
  if (/^\+?\d{10,15}$/.test(cleaned)) return { field: 'msisdn', label: 'MSISDN' }
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/.test(cleaned)) return { field: 'ip', label: 'IP' }
  return null
}

export default function SimListPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo<SIMListFilters>(() => ({
    state: searchParams.get('state') ?? undefined,
    operator_id: searchParams.get('operator_id') ?? undefined,
    apn_id: searchParams.get('apn_id') ?? undefined,
    rat_type: searchParams.get('rat_type') ?? undefined,
    q: searchParams.get('q') ?? undefined,
    iccid: searchParams.get('iccid') ?? undefined,
    imsi: searchParams.get('imsi') ?? undefined,
    msisdn: searchParams.get('msisdn') ?? undefined,
    ip: searchParams.get('ip') ?? undefined,
  }), [searchParams])
  const setFilters = useCallback((updater: SIMListFilters | ((prev: SIMListFilters) => SIMListFilters)) => {
    const next = typeof updater === 'function' ? updater(filters) : updater
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      const keys: (keyof SIMListFilters)[] = ['state', 'operator_id', 'apn_id', 'rat_type', 'q', 'iccid', 'imsi', 'msisdn', 'ip']
      keys.forEach((k) => {
        const v = next[k]
        if (v) { p.set(k, v) } else { p.delete(k) }
      })
      return p
    }, { replace: false })
  }, [filters, setSearchParams])
  const [searchInput, setSearchInput] = useState('')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [bulkDialog, setBulkDialog] = useState<{ action: string; label: string } | null>(null)
  const [bulkReason, setBulkReason] = useState('')
  const [selectedSegmentId, setSelectedSegmentId] = useState<string>('')
  const [selectAllSegment, setSelectAllSegment] = useState(false)
  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data: segments } = useSegments()
  const { data: segmentCount } = useSegmentCount(selectedSegmentId)
  const { data: operators } = useOperatorList()
  const { data: apns } = useAPNList({})
  const { data: policiesData } = usePolicyList(undefined, 'active')

  const activePolicies = useMemo(() => {
    if (!policiesData?.pages) return []
    return policiesData.pages.flatMap((page) => page.data).filter((p) => p.current_version_id)
  }, [policiesData])
  const bulkMutation = useBulkStateChange()
  const bulkPolicyAssignMutation = useBulkPolicyAssign()
  const importMutation = useImportSIMs()
  const [policyDialogOpen, setPolicyDialogOpen] = useState(false)
  const [selectedPolicyVersionId, setSelectedPolicyVersionId] = useState('')
  const [importOpen, setImportOpen] = useState(false)
  const [importTab, setImportTab] = useState<'paste' | 'file'>('paste')
  const [importFile, setImportFile] = useState<File | null>(null)
  const [pasteContent, setPasteContent] = useState('')
  const [reserveOnImport, setReserveOnImport] = useState(false)
  const [importResult, setImportResult] = useState<{ job_id: string; rows_parsed: number; errors: string[] } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

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
      const op = operators?.find((o) => o.id === filters.operator_id)
      applied.push({ key: 'operator_id', label: 'Operator', value: op?.name ?? filters.operator_id.slice(0, 8) })
    }
    if (filters.apn_id) {
      const apn = apns?.find((a) => a.id === filters.apn_id)
      applied.push({ key: 'apn_id', label: 'APN', value: apn?.display_name ?? apn?.name ?? filters.apn_id.slice(0, 8) })
    }
    if (filters.ip) {
      applied.push({ key: 'ip', label: 'IP', value: filters.ip })
    }
    return applied
  }, [filters])

  const handleSearch = useCallback(() => {
    const trimmed = searchInput.trim()
    if (!trimmed) {
      setFilters((f) => ({ ...f, q: undefined, iccid: undefined, imsi: undefined, msisdn: undefined, ip: undefined }))
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
        ip: detected.field === 'ip' ? trimmed : undefined,
      }))
    } else {
      setFilters((f) => ({ ...f, q: trimmed, iccid: undefined, imsi: undefined, msisdn: undefined, ip: undefined }))
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
    setSelectAllSegment(false)
    setSelectedIds(new Set())
  }

  const removeFilter = (key: string) => {
    setFilters((f) => ({ ...f, [key]: undefined }))
  }

  const handleSegmentSelect = (segId: string) => {
    setSelectedSegmentId(segId)
    setSelectAllSegment(false)
    setSelectedIds(new Set())
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
        ...(selectAllSegment
          ? { segmentId: selectedSegmentId }
          : { simIds: Array.from(selectedIds) }),
        targetState: bulkDialog.action,
        reason: bulkReason || undefined,
      })
      setSelectedIds(new Set())
      setSelectAllSegment(false)
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
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">SIM Management</h1>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" className="gap-2" onClick={() => navigate('/sims/compare')}>
            <GitCompareArrows className="h-4 w-4" />
            Compare
          </Button>
          <Button className="gap-2" size="sm" onClick={() => { setImportOpen(true); setImportFile(null); setPasteContent(''); setImportResult(null); setImportTab('paste'); setReserveOnImport(false) }}>
            <Upload className="h-4 w-4" />
            Import SIMs
          </Button>
        </div>
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
            placeholder="Search ICCID, IMSI, MSISDN, or IP..."
            className="pl-9 h-8 text-sm"
          />
          {searchInput && (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Clear search"
              onClick={() => {
                setSearchInput('')
                setFilters((f) => ({ ...f, q: undefined, iccid: undefined, imsi: undefined, msisdn: undefined, ip: undefined }))
              }}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors h-5 w-5"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
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

        {/* Operator Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.operator_id
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <span>Operator{filters.operator_id ? `: ${operators?.find((o) => o.id === filters.operator_id)?.name ?? ''}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem onClick={() => setFilters((f) => ({ ...f, operator_id: undefined }))}>
              <span className="flex-1">All Operators</span>
              {!filters.operator_id && <Check className="h-3.5 w-3.5 text-accent" />}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {operators?.filter((o) => o.state === 'active').map((op) => (
              <DropdownMenuItem key={op.id} onClick={() => setFilters((f) => ({ ...f, operator_id: op.id }))}>
                <span className="flex-1">{op.name}</span>
                {filters.operator_id === op.id && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* APN Filter */}
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.apn_id
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <span>APN{filters.apn_id ? `: ${apns?.find((a) => a.id === filters.apn_id)?.display_name ?? apns?.find((a) => a.id === filters.apn_id)?.name ?? ''}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem onClick={() => setFilters((f) => ({ ...f, apn_id: undefined }))}>
              <span className="flex-1">All APNs</span>
              {!filters.apn_id && <Check className="h-3.5 w-3.5 text-accent" />}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {apns?.map((apn) => (
              <DropdownMenuItem key={apn.id} onClick={() => setFilters((f) => ({ ...f, apn_id: apn.id }))}>
                <span className="flex-1">{apn.display_name || apn.name}</span>
                {filters.apn_id === apn.id && <Check className="h-3.5 w-3.5 text-accent" />}
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
            <Button variant="ghost" size="icon" aria-label="Remove filter" onClick={() => removeFilter(af.key)} className="h-4 w-4 hover:text-text-primary">
              <X className="h-3 w-3" />
            </Button>
          </span>
        ))}

        {activeFilters.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            onClick={clearFilters}
            className="text-xs text-text-tertiary hover:text-accent h-auto py-0 px-1"
          >
            Clear all ({activeFilters.length})
          </Button>
        )}
      </div>

      {/* Select All in Segment Banner */}
      {selectedSegmentId && segmentCount && !selectAllSegment && (
        <div className="flex items-center gap-3 px-4 py-2.5 rounded-[var(--radius-md)] border border-accent/20 bg-accent-dim/50">
          <span className="text-sm text-text-secondary">
            {selectedIds.size > 0
              ? `${selectedIds.size} SIM${selectedIds.size !== 1 ? 's' : ''} selected on this page.`
              : `Segment contains ${segmentCount.count.toLocaleString()} SIMs.`}
          </span>
          <Button
            variant="outline"
            size="sm"
            className="text-xs gap-1.5 border-accent/30 text-accent hover:bg-accent-dim"
            onClick={() => { setSelectAllSegment(true); setSelectedIds(new Set()) }}
          >
            Select all {segmentCount.count.toLocaleString()} SIMs in segment
          </Button>
        </div>
      )}
      {selectAllSegment && segmentCount && (
        <div className="flex items-center gap-3 px-4 py-2.5 rounded-[var(--radius-md)] border border-accent/30 bg-accent-dim">
          <span className="text-sm font-semibold text-accent">
            {segmentCount.count.toLocaleString()} SIMs selected (entire segment)
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="text-xs text-text-tertiary hover:text-text-primary ml-auto"
            onClick={() => setSelectAllSegment(false)}
          >
            Clear
          </Button>
        </div>
      )}

      {/* Data Table */}
      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-10">
                  <Input
                    type="checkbox"
                    checked={allSims.length > 0 && selectedIds.size === allSims.length}
                    onChange={toggleSelectAll}
                    className="h-4 w-4 rounded border-border accent-accent cursor-pointer"
                  />
                </TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead>IMSI</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>IP Address</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Operator</TableHead>
                <TableHead>APN</TableHead>
                <TableHead>IP Pool</TableHead>
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
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-14" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                  </TableRow>
                ))}

              {!isLoading && allSims.length === 0 && (
                <TableRow>
                  <TableCell colSpan={12}>
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
                    <Input
                      type="checkbox"
                      checked={selectedIds.has(sim.id)}
                      onChange={() => toggleSelect(sim.id)}
                      className="h-4 w-4 rounded border-border accent-accent cursor-pointer w-4 flex-none"
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
                    <span className="font-mono text-xs text-text-secondary">
                      {sim.ip_address || '-'}
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
                    <span className="text-xs text-text-secondary truncate max-w-[100px] block">{sim.operator_name || <span className="text-text-tertiary">—</span>}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary truncate max-w-[100px] block">{sim.apn_name || <span className="text-text-tertiary">—</span>}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary truncate max-w-[100px] block">{sim.ip_pool_name || <span className="text-text-tertiary">—</span>}</span>
                  </TableCell>
                  <TableCell>
                    <RATBadge ratType={sim.rat_type} />
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(sim.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <DropdownMenu>
                      <DropdownMenuTrigger aria-label="Row actions" className="p-1 text-text-tertiary hover:text-text-primary transition-colors rounded-[var(--radius-sm)] hover:bg-bg-hover">
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
                        {sim.state === 'active' && !sim.ip_address && sim.apn_id && (
                          <>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem onClick={async () => {
                              try {
                                const poolsRes = await api.get<{ data: { id: string }[] }>(`/ip-pools?apn_id=${sim.apn_id}&limit=1`)
                                const pool = poolsRes.data.data?.[0]
                                if (!pool) { toast.error('No IP pool found for this APN'); return }
                                await api.post(`/ip-pools/${pool.id}/addresses/reserve`, { sim_id: sim.id })
                                toast.success('Static IP reserved')
                                refetch()
                              } catch (err) {
                                toast.error(err instanceof Error ? err.message : 'Failed to reserve static IP')
                              }
                            }}>
                              Reserve Static IP
                            </DropdownMenuItem>
                          </>
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
        {(selectedIds.size > 0 || selectAllSegment) && (
          <div className="flex items-center gap-3 px-4 py-2.5 bg-accent-dim border-t border-accent/20 animate-in slide-in-from-bottom-1">
            <span className="text-sm font-semibold text-accent">
              {selectAllSegment
                ? `${segmentCount?.count.toLocaleString() ?? '?'} selected (entire segment)`
                : `${selectedIds.size} selected`}
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
                onClick={() => setPolicyDialogOpen(true)}
              >
                <Shield className="h-3 w-3" /> Assign Policy
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="text-xs gap-1.5"
                onClick={async () => {
                  const sims = allSims.filter((s) => selectedIds.has(s.id) && s.state === 'active' && !s.ip_address && s.apn_id)
                  if (sims.length === 0) return
                  const poolCache: Record<string, string> = {}
                  let succeeded = 0
                  const failed: { iccid: string; error: string }[] = []
                  for (const sim of sims) {
                    try {
                      if (!poolCache[sim.apn_id!]) {
                        const res = await api.get<{ data: { id: string }[] }>(`/ip-pools?apn_id=${sim.apn_id}&limit=1`)
                        poolCache[sim.apn_id!] = res.data.data?.[0]?.id ?? ''
                      }
                      const poolId = poolCache[sim.apn_id!]
                      if (poolId) {
                        await api.post(`/ip-pools/${poolId}/addresses/reserve`, { sim_id: sim.id })
                        succeeded++
                      } else {
                        failed.push({ iccid: sim.iccid, error: 'No IP pool found for APN' })
                      }
                    } catch (err) {
                      const msg = err instanceof Error ? err.message : 'Reserve failed'
                      failed.push({ iccid: sim.iccid, error: msg })
                    }
                  }
                  if (failed.length === 0) {
                    toast.success(`Reserved IPs for ${succeeded} SIM${succeeded !== 1 ? 's' : ''}`)
                  } else {
                    toast.error(`${succeeded} succeeded, ${failed.length} failed — check each SIM's APN pool`)
                  }
                  setSelectedIds(new Set())
                  refetch()
                }}
              >
                <Globe className="h-3 w-3" /> Reserve IPs
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
            <Button
              variant="ghost"
              size="sm"
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent py-1 h-auto"
            >
              Load more SIMs
            </Button>
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
            <Button
              key={seg.id}
              variant="ghost"
              size="sm"
              onClick={() => handleSegmentSelect(seg.id === selectedSegmentId ? '' : seg.id)}
              className={cn(
                'px-3 py-1 h-auto text-xs rounded-full border transition-colors',
                selectedSegmentId === seg.id
                  ? 'bg-accent-dim border-accent/30 text-accent hover:bg-accent-dim hover:text-accent'
                  : 'bg-bg-surface border-border text-text-secondary hover:border-accent hover:text-accent',
              )}
            >
              {seg.name}
            </Button>
          ))}
        </div>
      )}

      {/* Bulk Action SlidePanel */}
      <SlidePanel
        open={!!bulkDialog}
        onOpenChange={() => setBulkDialog(null)}
        title={`${bulkDialog?.label} ${selectedIds.size} SIM${selectedIds.size !== 1 ? 's' : ''}?`}
        description={`This action will ${bulkDialog?.label.toLowerCase()} the selected SIMs. This may take a moment for large selections.`}
        width="md"
      >
        <div>
          <label className="text-xs font-medium text-text-secondary block mb-1.5">
            Reason (optional)
          </label>
          <Input
            value={bulkReason}
            onChange={(e) => setBulkReason(e.target.value)}
            placeholder="Enter reason..."
          />
        </div>
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
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
        </div>
      </SlidePanel>

      {/* Import SIMs SlidePanel */}
      <SlidePanel open={importOpen} onOpenChange={setImportOpen} title="Import SIMs" description="Paste CSV data or upload a file. A background job will process the import." width="lg">
        {importResult ? (
          <div className="rounded-[var(--radius-sm)] border border-success/30 bg-success-dim p-4 space-y-2">
            <div className="flex items-center gap-2">
              <Check className="h-4 w-4 text-success" />
              <span className="text-sm font-medium text-text-primary">Import job created</span>
            </div>
            <div className="text-xs text-text-secondary space-y-1">
              <p>Rows parsed: <span className="font-mono text-text-primary">{importResult.rows_parsed}</span></p>
              <p>Job ID: <span className="font-mono text-text-tertiary">{importResult.job_id.slice(0, 12)}...</span></p>
              {importResult.errors.length > 0 && (
                <div className="mt-2">
                  <p className="text-warning font-medium">{importResult.errors.length} validation errors:</p>
                  <ul className="list-disc pl-4 text-text-tertiary mt-1">
                    {importResult.errors.slice(0, 5).map((err, i) => <li key={i}>{err}</li>)}
                    {importResult.errors.length > 5 && <li>...and {importResult.errors.length - 5} more</li>}
                  </ul>
                </div>
              )}
            </div>
            <p className="text-xs text-text-tertiary">Check the Jobs page for progress.</p>
          </div>
        ) : (
          <Tabs value={importTab} onValueChange={(v) => setImportTab(v as 'paste' | 'file')}>
            <TabsList className="w-full">
              <TabsTrigger value="paste" className="flex-1 gap-1.5">
                <ClipboardPaste className="h-3.5 w-3.5" />
                Paste Data
              </TabsTrigger>
              <TabsTrigger value="file" className="flex-1 gap-1.5">
                <FileUp className="h-3.5 w-3.5" />
                Upload File
              </TabsTrigger>
            </TabsList>

            <div className="mt-3">
              <div className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-2.5 mb-3">
                <p className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium mb-1">Required Columns</p>
                <pre className="font-mono text-[11px] text-text-secondary">iccid, imsi, msisdn, operator_code, apn_name</pre>
                <p className="text-[10px] text-text-tertiary mt-1">Comma or tab delimited. First row must be headers.</p>
              </div>

              <label className="flex items-center gap-2 mb-3 cursor-pointer">
                <Input
                  type="checkbox"
                  checked={reserveOnImport}
                  onChange={(e) => setReserveOnImport(e.target.checked)}
                  className="h-4 w-4 rounded border-border accent-accent w-4 flex-none"
                />
                <span className="text-xs text-text-secondary">Reserve static IP for each SIM from APN's pool</span>
              </label>

              <TabsContent value="paste" className="mt-0">
                <Textarea
                  value={pasteContent}
                  onChange={(e) => setPasteContent(e.target.value)}
                  placeholder={`iccid,imsi,msisdn,operator_code,apn_name\n8990010000000001,286010000000001,905301000001,turkcell,iot.demo\n8990010000000002,286010000000002,905301000002,turkcell,m2m.demo`}
                  className="h-48 font-mono text-xs placeholder:text-text-tertiary/40"
                  spellCheck={false}
                />
                <div className="flex items-center justify-between mt-2">
                  <span className="text-[10px] text-text-tertiary">
                    {pasteContent ? `${pasteContent.trim().split('\n').length - 1} data rows` : 'Paste comma or tab delimited data'}
                  </span>
                </div>
              </TabsContent>

              <TabsContent value="file" className="mt-0">
                <Input
                  ref={fileInputRef}
                  type="file"
                  accept=".csv,.tsv,.txt"
                  className="hidden"
                  onChange={(e) => {
                    const f = e.target.files?.[0]
                    if (f) setImportFile(f)
                  }}
                />
                <Button
                  variant="ghost"
                  onClick={() => fileInputRef.current?.click()}
                  className={cn(
                    'w-full h-auto flex flex-col items-center justify-center py-10 rounded-[var(--radius-md)] border-2 border-dashed transition-colors cursor-pointer',
                    importFile
                      ? 'border-accent/30 bg-accent-dim hover:bg-accent-dim'
                      : 'border-border hover:border-accent/30 hover:bg-bg-hover',
                  )}
                >
                  <FileUp className="h-6 w-6 text-text-tertiary mb-2" />
                  {importFile ? (
                    <div className="text-center">
                      <p className="text-sm font-medium text-accent">{importFile.name}</p>
                      <p className="text-xs text-text-tertiary mt-1">{(importFile.size / 1024).toFixed(1)} KB</p>
                    </div>
                  ) : (
                    <>
                      <p className="text-sm text-text-secondary">Click to select CSV file</p>
                      <p className="text-[10px] text-text-tertiary mt-1">.csv, .tsv, .txt — max 50MB</p>
                    </>
                  )}
                </Button>
              </TabsContent>
            </div>
          </Tabs>
        )}

        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
          <Button variant="outline" onClick={() => setImportOpen(false)}>
            {importResult ? 'Close' : 'Cancel'}
          </Button>
          {!importResult && (
            <Button
              onClick={async () => {
                let file: File
                if (importTab === 'paste') {
                  if (!pasteContent.trim()) return
                  const normalized = pasteContent.includes('\t')
                    ? pasteContent.replace(/\t/g, ',')
                    : pasteContent
                  file = new File([normalized], 'import.csv', { type: 'text/csv' })
                } else {
                  if (!importFile) return
                  file = importFile
                }
                try {
                  const result = await importMutation.mutateAsync({ file, reserveStaticIP: reserveOnImport })
                  setImportResult(result)
                } catch {
                  // handled by api interceptor
                }
              }}
              disabled={(importTab === 'paste' ? !pasteContent.trim() : !importFile) || importMutation.isPending}
              className="gap-2"
            >
              {importMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
              Import
            </Button>
          )}
        </div>
      </SlidePanel>

      {/* Bulk Assign Policy Dialog */}
      <Dialog open={policyDialogOpen} onOpenChange={setPolicyDialogOpen}>
        <DialogContent onClose={() => setPolicyDialogOpen(false)}>
          <DialogHeader>
            <DialogTitle>
              Assign Policy to{' '}
              {selectAllSegment
                ? `${segmentCount?.count.toLocaleString() ?? '?'} SIMs (entire segment)`
                : `${selectedIds.size} SIM${selectedIds.size !== 1 ? 's' : ''}`}
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Select Policy
              </label>
              <Select
                value={selectedPolicyVersionId}
                onChange={(e) => setSelectedPolicyVersionId(e.target.value)}
                placeholder="Choose a policy..."
                options={activePolicies.map((p) => ({
                  value: p.current_version_id!,
                  label: `${p.name} v${p.active_version}`,
                }))}
              />
            </div>

            {selectedPolicyVersionId && (
              <div className="rounded-[var(--radius-sm)] border border-border bg-bg-primary p-3 space-y-1">
                <p className="text-xs text-text-tertiary">Preview</p>
                <p className="text-sm text-text-primary font-medium">
                  {activePolicies.find((p) => p.current_version_id === selectedPolicyVersionId)?.name ?? 'Policy'}{' '}
                  <span className="text-text-secondary font-normal">
                    v{activePolicies.find((p) => p.current_version_id === selectedPolicyVersionId)?.active_version}
                  </span>
                </p>
                <p className="text-xs text-text-secondary">
                  Will be assigned to{' '}
                  <span className="font-semibold text-text-primary">
                    {selectAllSegment
                      ? `${segmentCount?.count.toLocaleString() ?? '?'} SIMs (entire segment)`
                      : `${selectedIds.size} SIM${selectedIds.size !== 1 ? 's' : ''}`}
                  </span>
                </p>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => { setPolicyDialogOpen(false); setSelectedPolicyVersionId('') }}>
              Cancel
            </Button>
            <Button
              disabled={!selectedPolicyVersionId || bulkPolicyAssignMutation.isPending}
              className="gap-2"
              onClick={async () => {
                try {
                  await bulkPolicyAssignMutation.mutateAsync({
                    ...(selectAllSegment
                      ? { segmentId: selectedSegmentId }
                      : { simIds: Array.from(selectedIds) }),
                    policyVersionId: selectedPolicyVersionId,
                  })
                  setSelectedIds(new Set())
                  setSelectAllSegment(false)
                  setPolicyDialogOpen(false)
                  setSelectedPolicyVersionId('')
                } catch {
                  // error handled by api interceptor
                }
              }}
            >
              {bulkPolicyAssignMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Confirm
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
