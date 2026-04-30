import { useState, useMemo, useEffect, useRef, useCallback } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  Filter,
  Check,
  RefreshCw,
  AlertCircle,
  Loader2,
  Smartphone,
  Power,
  PowerOff,
  ArrowRightLeft,
  Trash2,
  Download,
  Copy,
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Select } from '@/components/ui/select'
import { Tooltip } from '@/components/ui/tooltip'
import { Spinner } from '@/components/ui/spinner'
import {
  useESimList,
  useEnableProfile,
  useDisableProfile,
  useSwitchProfile,
  useDeleteProfile,
  useBulkSwitchEsim,
} from '@/hooks/use-esim'
import { useOperatorList } from '@/hooks/use-operators'
import type { ESimProfile, ESimProfileState } from '@/types/esim'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/shared/empty-state'
import { useExport } from '@/hooks/use-export'
import { formatEID } from '@/lib/format'
import { useUIStore } from '@/stores/ui'
import { toast } from 'sonner'

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'available', label: 'Available' },
  { value: 'enabled', label: 'Enabled' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'failed', label: 'Failed' },
  { value: 'deleted', label: 'Deleted' },
]

function stateVariant(state: string): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state as ESimProfileState) {
    case 'enabled': return 'success'
    case 'disabled': return 'warning'
    case 'available': return 'default'
    case 'deleted': return 'danger'
    default: return 'secondary'
  }
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-6 w-6 flex-shrink-0 text-text-tertiary hover:text-text-primary transition-colors"
      onClick={handleCopy}
      title="Copy EID"
    >
      {copied
        ? <Check className="h-3 w-3" style={{ color: 'var(--color-success)' }} />
        : <Copy className="h-3 w-3" />}
    </Button>
  )
}

export default function EsimListPage() {
  const sidebarCollapsed = useUIStore((s) => s.sidebarCollapsed)
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo<{ operator_id?: string; state?: string }>(() => ({
    state: searchParams.get('state') || undefined,
    operator_id: searchParams.get('operator_id') || undefined,
  }), [searchParams])
  const setFilters = useCallback(
    (next: { operator_id?: string; state?: string } | ((prev: { operator_id?: string; state?: string }) => { operator_id?: string; state?: string })) => {
      const current = {
        state: searchParams.get('state') || undefined,
        operator_id: searchParams.get('operator_id') || undefined,
      }
      const resolved = typeof next === 'function' ? next(current) : next
      const params = new URLSearchParams(searchParams)
      if (resolved.state) params.set('state', resolved.state); else params.delete('state')
      if (resolved.operator_id) params.set('operator_id', resolved.operator_id); else params.delete('operator_id')
      setSearchParams(params, { replace: false })
    },
    [searchParams, setSearchParams],
  )

  const [actionDialog, setActionDialog] = useState<{
    profile: ESimProfile
    action: 'enable' | 'disable' | 'switch' | 'delete'
  } | null>(null)
  const [switchTargetId, setSwitchTargetId] = useState('')

  // Bulk selection state
  const [selectedEids, setSelectedEids] = useState<Set<string>>(new Set())
  const [bulkSwitchOpen, setBulkSwitchOpen] = useState(false)
  const [bulkTargetOperatorId, setBulkTargetOperatorId] = useState('')
  const [bulkReason, setBulkReason] = useState('')

  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data: operatorsData } = useOperatorList()
  const operators = operatorsData ?? []

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useESimList(filters)

  const enableMutation = useEnableProfile()
  const disableMutation = useDisableProfile()
  const switchMutation = useSwitchProfile()
  const deleteMutation = useDeleteProfile()
  const bulkSwitchMutation = useBulkSwitchEsim()
  const { exportCSV, exporting } = useExport('esim-profiles')

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

  const allProfiles = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  // Clear selection when filters change
  useEffect(() => {
    setSelectedEids(new Set())
  }, [filters])

  const toggleSelect = useCallback((eid: string) => {
    setSelectedEids((prev) => {
      const next = new Set(prev)
      if (next.has(eid)) next.delete(eid)
      else next.add(eid)
      return next
    })
  }, [])

  const toggleSelectAll = useCallback(() => {
    if (selectedEids.size === allProfiles.length && allProfiles.length > 0) {
      setSelectedEids(new Set())
    } else {
      setSelectedEids(new Set(allProfiles.map((p) => p.eid)))
    }
  }, [selectedEids, allProfiles])

  const handleAction = async () => {
    if (!actionDialog) return
    try {
      if (actionDialog.action === 'enable') {
        await enableMutation.mutateAsync(actionDialog.profile.id)
      } else if (actionDialog.action === 'disable') {
        await disableMutation.mutateAsync(actionDialog.profile.id)
      } else if (actionDialog.action === 'switch' && switchTargetId) {
        await switchMutation.mutateAsync({
          profileId: actionDialog.profile.id,
          targetProfileId: switchTargetId,
        })
      } else if (actionDialog.action === 'delete') {
        await deleteMutation.mutateAsync(actionDialog.profile.id)
      }
      setActionDialog(null)
      setSwitchTargetId('')
    } catch {
      // handled by api interceptor
    }
  }

  const handleBulkSwitch = async () => {
    if (!bulkTargetOperatorId) return
    try {
      const result = await bulkSwitchMutation.mutateAsync({
        eids: Array.from(selectedEids),
        target_operator_id: bulkTargetOperatorId,
        reason: bulkReason || undefined,
      })
      toast.success(`Bulk switch queued — ${result.affected_count} profile${result.affected_count !== 1 ? 's' : ''} affected (job ${result.job_id.slice(0, 8)})`)
      setBulkSwitchOpen(false)
      setBulkTargetOperatorId('')
      setBulkReason('')
      setSelectedEids(new Set())
    } catch {
      // handled by api interceptor
    }
  }

  const isPending = enableMutation.isPending || disableMutation.isPending || switchMutation.isPending || deleteMutation.isPending

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load eSIM profiles</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch profile data. Please try again.</p>
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
        <h1 className="text-base font-semibold text-text-primary">eSIM Profiles</h1>
        <div className="flex items-center gap-2">
          {/* FIX-235 Gate (F-U1): list-page Allocate button removed.
              Allocation is initiated from SIM Detail → eSIM tab where the target SIM is
              already in scope; AC-9 explicitly anchors the flow to the SIM-detail panel. */}
          <Button variant="outline" size="sm" className="gap-2" onClick={() => exportCSV(Object.fromEntries(searchParams))} disabled={exporting}>
            {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            Export
          </Button>
        </div>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-3 flex-wrap">
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.state
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>State{filters.state ? `: ${filters.state}` : ''}</span>
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

        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.operator_id
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <span>
              Operator{filters.operator_id
                ? `: ${operators.find((o) => o.id === filters.operator_id)?.name ?? filters.operator_id.slice(0, 8)}`
                : ''}
            </span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="max-h-64 overflow-y-auto">
            <DropdownMenuItem
              onClick={() => setFilters((f) => ({ ...f, operator_id: undefined }))}
            >
              <span className="flex-1">All Operators</span>
              {!filters.operator_id && <Check className="h-3.5 w-3.5 text-accent" />}
            </DropdownMenuItem>
            {operators.map((op) => (
              <DropdownMenuItem
                key={op.id}
                onClick={() => setFilters((f) => ({ ...f, operator_id: op.id }))}
              >
                <span className="flex-1">{op.name}</span>
                {filters.operator_id === op.id && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {(filters.state || filters.operator_id) && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setFilters({})}
            className="text-xs text-text-tertiary hover:text-accent transition-colors h-7 px-2"
          >
            Clear all
          </Button>
        )}
      </div>

      {/* Profiles Table */}
      <Card className={cn('overflow-hidden density-compact', selectedEids.size > 0 && 'pb-16')}>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-10">
                  <Input
                    type="checkbox"
                    checked={allProfiles.length > 0 && selectedEids.size === allProfiles.length}
                    onChange={toggleSelectAll}
                    className="h-4 w-4 rounded border-border accent-accent cursor-pointer"
                  />
                </TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead>EID</TableHead>
                <TableHead>Operator</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Last Provisioned</TableHead>
                <TableHead>Error</TableHead>
                <TableHead className="w-32">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 6 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 8 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && allProfiles.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8}>
                    <EmptyState
                      icon={Smartphone}
                      title="No eSIM profiles found"
                      description={filters.state ? 'Try adjusting your filters.' : 'eSIM profiles will appear here when provisioned.'}
                    />
                  </TableCell>
                </TableRow>
              )}

              {allProfiles.map((profile, idx) => (
                <TableRow
                  key={profile.id}
                  data-row-index={idx}
                  data-state={selectedEids.has(profile.eid) ? 'selected' : undefined}
                >
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <Input
                      type="checkbox"
                      checked={selectedEids.has(profile.eid)}
                      onChange={() => toggleSelect(profile.eid)}
                      className="h-4 w-4 rounded border-border accent-accent cursor-pointer"
                    />
                  </TableCell>
                  <TableCell>
                    <Link
                      to={`/sims/${profile.sim_id}`}
                      className="font-mono text-xs text-accent hover:underline"
                    >
                      {profile.iccid_on_profile ?? '-'}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Tooltip content={profile.eid} side="top">
                      <div className="flex items-center gap-1">
                        <span className="font-mono text-xs text-text-secondary">{formatEID(profile.eid)}</span>
                        <CopyButton text={profile.eid} />
                      </div>
                    </Tooltip>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-primary">{profile.operator_name ?? '-'}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(profile.profile_state as ESimProfileState)} className="gap-1">
                      {profile.profile_state === 'enabled' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {profile.profile_state.toUpperCase()}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {profile.last_provisioned_at
                        ? new Date(profile.last_provisioned_at).toLocaleString()
                        : '-'}
                    </span>
                  </TableCell>
                  <TableCell>
                    {profile.last_error ? (
                      <span className="text-xs text-danger truncate max-w-48 block" title={profile.last_error}>
                        {profile.last_error}
                      </span>
                    ) : (
                      <span className="text-xs text-text-tertiary">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      {(profile.profile_state === 'disabled' || profile.profile_state === 'available') && (
                        <>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-6 px-2 text-xs gap-1"
                            onClick={() => setActionDialog({ profile, action: 'enable' })}
                          >
                            <Power className="h-3 w-3" />
                            Enable
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-6 px-2 text-xs gap-1 border-danger/30 text-danger hover:bg-danger-dim"
                            onClick={() => setActionDialog({ profile, action: 'delete' })}
                          >
                            <Trash2 className="h-3 w-3" />
                            Delete
                          </Button>
                        </>
                      )}
                      {profile.profile_state === 'enabled' && (
                        <>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-6 px-2 text-xs gap-1 border-warning/30 text-warning hover:bg-warning-dim"
                            onClick={() => setActionDialog({ profile, action: 'disable' })}
                          >
                            <PowerOff className="h-3 w-3" />
                            Disable
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-6 px-2 text-xs gap-1 border-purple/30 text-purple hover:bg-purple/10"
                            onClick={() => setActionDialog({ profile, action: 'switch' })}
                          >
                            <ArrowRightLeft className="h-3 w-3" />
                            Switch
                          </Button>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {/* Bulk Action Bar */}
        {selectedEids.size > 0 && (
          <div
            className={cn(
              'fixed bottom-0 right-0 z-30 bg-accent-dim border-t border-accent/20 shadow-[var(--shadow-card)] animate-in slide-in-from-bottom-2 duration-200 flex items-center gap-3 px-4 py-2.5 flex-wrap gap-y-2 transition-[left]',
              sidebarCollapsed ? 'left-16' : 'left-60',
            )}
          >
            <span className="text-sm font-semibold text-accent tabular-nums">
              {selectedEids.size} profile{selectedEids.size !== 1 ? 's' : ''} selected
            </span>
            <div className="flex gap-2 ml-2">
              <Button
                variant="secondary"
                size="sm"
                className="text-xs gap-1.5"
                onClick={() => setBulkSwitchOpen(true)}
              >
                <ArrowRightLeft className="h-3 w-3" /> Bulk Switch Operator
              </Button>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="text-xs gap-1.5 ml-auto text-text-tertiary hover:text-text-primary"
              onClick={() => setSelectedEids(new Set())}
            >
              Clear selection
            </Button>
          </div>
        )}

        <div ref={loadMoreRef} className="px-4 py-3 border-t border-border-subtle">
          {isFetchingNextPage ? (
            <div className="flex items-center justify-center gap-2 text-text-tertiary text-xs">
              <Spinner className="h-3.5 w-3.5" />
              Loading more...
            </div>
          ) : hasNextPage ? (
            <Button
              variant="ghost"
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent py-1"
            >
              Load more profiles
            </Button>
          ) : allProfiles.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allProfiles.length} profiles
            </p>
          ) : null}
        </div>
      </Card>

      {/* Single-Profile Action Dialog */}
      <Dialog open={!!actionDialog} onOpenChange={() => { setActionDialog(null); setSwitchTargetId('') }}>
        <DialogContent onClose={() => { setActionDialog(null); setSwitchTargetId('') }}>
          <DialogHeader>
            <DialogTitle>
              {actionDialog?.action === 'enable' && 'Enable Profile?'}
              {actionDialog?.action === 'disable' && 'Disable Profile?'}
              {actionDialog?.action === 'switch' && 'Switch Profile'}
              {actionDialog?.action === 'delete' && 'Delete Profile?'}
            </DialogTitle>
            <DialogDescription>
              {actionDialog?.action === 'enable' && (
                <>Enable eSIM profile for SIM <span className="font-mono text-accent">{actionDialog.profile.sim_id.slice(0, 8)}</span>. This will activate the profile on the device.</>
              )}
              {actionDialog?.action === 'disable' && (
                <>Disable the currently enabled eSIM profile. The device will lose connectivity until another profile is enabled.</>
              )}
              {actionDialog?.action === 'switch' && (
                <>Switch from the current profile to a different profile on the same SIM. Enter the target profile ID below.</>
              )}
              {actionDialog?.action === 'delete' && (
                <>Permanently delete eSIM profile for SIM <span className="font-mono text-accent">{actionDialog?.profile.sim_id.slice(0, 8)}</span>. This cannot be undone.</>
              )}
            </DialogDescription>
          </DialogHeader>
          {actionDialog?.action === 'switch' && (
            <div className="py-2">
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Target Profile ID
              </label>
              <Input
                value={switchTargetId}
                onChange={(e) => setSwitchTargetId(e.target.value)}
                placeholder="Enter target profile UUID..."
                className="font-mono text-sm"
              />
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => { setActionDialog(null); setSwitchTargetId('') }}>
              Cancel
            </Button>
            <Button
              variant={actionDialog?.action === 'disable' || actionDialog?.action === 'delete' ? 'destructive' : 'default'}
              onClick={handleAction}
              disabled={isPending || (actionDialog?.action === 'switch' && !switchTargetId)}
              className="gap-2"
            >
              {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              {actionDialog?.action === 'enable' && 'Enable'}
              {actionDialog?.action === 'disable' && 'Disable'}
              {actionDialog?.action === 'switch' && 'Switch'}
              {actionDialog?.action === 'delete' && 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Bulk Switch Operator Dialog */}
      <Dialog open={bulkSwitchOpen} onOpenChange={(o) => !o && setBulkSwitchOpen(false)}>
        <DialogContent onClose={() => setBulkSwitchOpen(false)}>
          <DialogHeader>
            <DialogTitle>Bulk Switch Operator</DialogTitle>
            <DialogDescription>
              Switch {selectedEids.size} selected profile{selectedEids.size !== 1 ? 's' : ''} to a new operator via OTA. This action is queued as a background job.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Target Operator
              </label>
              <Select
                value={bulkTargetOperatorId}
                onChange={(e) => setBulkTargetOperatorId(e.target.value)}
                options={operators.map((op) => ({ value: op.id, label: op.name }))}
                placeholder="Select operator..."
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Reason (optional)
              </label>
              <Input
                value={bulkReason}
                onChange={(e) => setBulkReason(e.target.value)}
                placeholder="e.g. Cost optimisation, coverage improvement..."
                className="text-sm"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBulkSwitchOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="default"
              onClick={handleBulkSwitch}
              disabled={!bulkTargetOperatorId || bulkSwitchMutation.isPending}
              className="gap-2"
            >
              {bulkSwitchMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Switch {selectedEids.size} Profile{selectedEids.size !== 1 ? 's' : ''}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
