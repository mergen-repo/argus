import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AlertCircle,
  ArrowRightLeft,
  ChevronDown,
  Plus,
  RefreshCw,
  ShieldCheck,
  Smartphone,
  Trash2,
  Upload,
} from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { EmptyState } from '@/components/shared/empty-state'
import { RowActionsMenu } from '@/components/shared/row-actions-menu'
import { useIMEIPoolAdd, useIMEIPoolDelete, useIMEIPoolList } from '@/hooks/use-imei-pools'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import {
  ENTRY_KIND_LABEL,
  IMEI_ENTRY_KINDS,
  IMEI_POOLS,
  IMPORTED_FROM_LABEL,
  POOL_LABEL,
  type IMEIEntryKind,
  type IMEIPool,
  type IMEIPoolEntry,
} from '@/types/imei-pool'
import { AddEntryDialog } from './add-entry-dialog'

interface PoolListTabProps {
  pool: IMEIPool
  onSwitchToImport?: () => void
}

const POOL_BORDER_TONE: Record<IMEIPool, string> = {
  whitelist: 'before:bg-success',
  greylist: 'before:bg-warning',
  blacklist: 'before:bg-danger',
}

const POOL_ICON: Record<IMEIPool, typeof ShieldCheck> = {
  whitelist: ShieldCheck,
  greylist: AlertCircle,
  blacklist: Smartphone,
}

function buildBoundFilterUrl(entry: IMEIPoolEntry): string {
  if (entry.kind === 'full_imei') {
    return `/sims?bound_imei=${encodeURIComponent(entry.imei_or_tac)}`
  }
  return `/sims?bound_tac=${encodeURIComponent(entry.imei_or_tac)}`
}

export function PoolListTab({ pool, onSwitchToImport }: PoolListTabProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [tacFilter, setTacFilter] = useState('')
  const [deviceFilter, setDeviceFilter] = useState('')
  const [kindFilter, setKindFilter] = useState<IMEIEntryKind | ''>('')
  const [showAdd, setShowAdd] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<IMEIPoolEntry | null>(null)
  const [moveTarget, setMoveTarget] = useState<IMEIPoolEntry | null>(null)
  const [moveDestination, setMoveDestination] = useState<IMEIPool>('whitelist')
  const [movePending, setMovePending] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [bulkDeletePending, setBulkDeletePending] = useState(false)
  const [confirmBulkDelete, setConfirmBulkDelete] = useState(false)

  const filters = useMemo(
    () => ({
      kind: kindFilter || undefined,
      tac: tacFilter.trim() || undefined,
      device_model: deviceFilter.trim() || undefined,
    }),
    [kindFilter, tacFilter, deviceFilter],
  )

  const list = useIMEIPoolList(pool, filters)
  const del = useIMEIPoolDelete(pool)
  // Add hook for the destination pool (re-rendered on selection change).
  const addToDestination = useIMEIPoolAdd(moveDestination)

  const allRows: IMEIPoolEntry[] = useMemo(() => {
    if (!list.data) return []
    return list.data.pages.flatMap((p) => p.data)
  }, [list.data])

  // Reset selection when the list changes (filters / pool switch).
  useEffect(() => {
    setSelected(new Set())
  }, [pool, tacFilter, deviceFilter, kindFilter])

  const totalLoaded = allRows.length
  const Icon = POOL_ICON[pool]
  const selectedCount = selected.size
  const allVisibleSelected = totalLoaded > 0 && selectedCount === totalLoaded

  const toggleRow = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAll = () => {
    if (allVisibleSelected) {
      setSelected(new Set())
    } else {
      setSelected(new Set(allRows.map((r) => r.id)))
    }
  }

  const handleDelete = async () => {
    if (!confirmDelete) return
    try {
      await del.mutateAsync(confirmDelete.id)
      toast.success('Entry removed')
      setConfirmDelete(null)
    } catch {
      // surfaced by axios interceptor toast
    }
  }

  const handleBulkDelete = async () => {
    if (selectedCount === 0) return
    setBulkDeletePending(true)
    const targets = Array.from(selected)
    const results = await Promise.allSettled(targets.map((id) => api.delete(`/imei-pools/${pool}/${id}`)))
    const failed = results.filter((r) => r.status === 'rejected').length
    const ok = results.length - failed
    queryClient.invalidateQueries({ queryKey: ['imei-pools', 'list', pool] })
    setSelected(new Set())
    setConfirmBulkDelete(false)
    setBulkDeletePending(false)
    if (failed === 0) {
      toast.success(`Removed ${ok} ${ok === 1 ? 'entry' : 'entries'}`)
    } else {
      toast.error(`Removed ${ok}, failed ${failed}. Check Jobs / audit log.`)
    }
  }

  const handleMove = async () => {
    if (!moveTarget) return
    if (moveDestination === pool) {
      toast.error('Choose a different destination pool')
      return
    }
    setMovePending(true)
    try {
      // Sequenced DELETE-old + POST-new (no atomic move endpoint per AC-7).
      // Each step emits its own audit event (entry_removed + entry_added).
      const payload: Record<string, unknown> = {
        kind: moveTarget.kind,
        imei_or_tac: moveTarget.imei_or_tac,
      }
      if (moveTarget.device_model) payload.device_model = moveTarget.device_model
      if (moveTarget.description) payload.description = moveTarget.description
      // Quarantine / block reasons + imported_from MUST be supplied for grey/blacklist.
      if (moveDestination === 'greylist') {
        payload.quarantine_reason = moveTarget.quarantine_reason ?? `Moved from ${POOL_LABEL[pool]}`
      }
      if (moveDestination === 'blacklist') {
        payload.block_reason = moveTarget.block_reason ?? `Moved from ${POOL_LABEL[pool]}`
        payload.imported_from = moveTarget.imported_from ?? 'manual'
      }
      // 1. POST to destination first; if it fails, the source row is preserved.
      await addToDestination.mutateAsync(payload as never)
      // 2. DELETE from source.
      try {
        await del.mutateAsync(moveTarget.id)
        toast.success(`Moved to ${POOL_LABEL[moveDestination]}`)
      } catch {
        toast.error(
          `Moved to ${POOL_LABEL[moveDestination]} but source delete failed — entry now exists in BOTH pools. Remove manually.`,
        )
      }
      setMoveTarget(null)
    } catch (err) {
      const e = err as { response?: { data?: { error?: { message?: string } } } }
      toast.error(e?.response?.data?.error?.message ?? 'Move failed — source entry kept')
    } finally {
      setMovePending(false)
    }
  }

  const startMove = (entry: IMEIPoolEntry) => {
    // Default destination = first non-source pool.
    const next = (IMEI_POOLS.find((p) => p !== pool) ?? 'whitelist') as IMEIPool
    setMoveDestination(next)
    setMoveTarget(entry)
  }

  if (list.isError) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center gap-3 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-danger" />
          <div>
            <p className="text-sm font-semibold text-text-primary">
              Failed to load {POOL_LABEL[pool]}
            </p>
            <p className="text-xs text-text-secondary mt-1">
              The IMEI pool service did not respond. Try again in a moment.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => list.refetch()} className="gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-4">
      <AddEntryDialog open={showAdd} onOpenChange={setShowAdd} pool={pool} />

      <Dialog open={!!confirmDelete} onOpenChange={(o) => !o && setConfirmDelete(null)}>
        <DialogContent onClose={() => setConfirmDelete(null)}>
          <DialogHeader>
            <DialogTitle>Remove entry?</DialogTitle>
            <DialogDescription>
              This permanently removes <span className="font-mono text-text-primary">{confirmDelete?.imei_or_tac}</span> from {POOL_LABEL[pool]}. The action is audited and cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
            <Button variant="outline" onClick={() => setConfirmDelete(null)} disabled={del.isPending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={del.isPending} className="gap-1.5">
              <Trash2 className="h-3.5 w-3.5" />
              {del.isPending ? 'Removing…' : 'Remove'}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={!!moveTarget} onOpenChange={(o) => !o && setMoveTarget(null)}>
        <DialogContent onClose={() => setMoveTarget(null)}>
          <DialogHeader>
            <DialogTitle>Move entry to another pool</DialogTitle>
            <DialogDescription>
              Moving <span className="font-mono text-text-primary">{moveTarget?.imei_or_tac}</span> from {POOL_LABEL[pool]} to a different pool emits two audit events: <span className="font-mono">entry_removed</span> and <span className="font-mono">entry_added</span>. The destination is created first; if the source delete fails the entry will exist in both pools and must be cleared manually.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-3">
            <label htmlFor="move-destination" className="text-xs font-medium text-text-secondary block">
              Destination pool
            </label>
            <Select
              id="move-destination"
              value={moveDestination}
              onChange={(e) => setMoveDestination(e.target.value as IMEIPool)}
              options={IMEI_POOLS.filter((p) => p !== pool).map((p) => ({ value: p, label: POOL_LABEL[p] }))}
              disabled={movePending}
              className="w-full"
            />
          </div>
          <div className="flex items-center justify-end gap-2 pt-4 border-t border-border mt-2">
            <Button variant="outline" onClick={() => setMoveTarget(null)} disabled={movePending}>
              Cancel
            </Button>
            <Button onClick={handleMove} disabled={movePending} className="gap-1.5">
              <ArrowRightLeft className="h-3.5 w-3.5" />
              {movePending ? 'Moving…' : `Move to ${POOL_LABEL[moveDestination]}`}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmBulkDelete} onOpenChange={(o) => !o && setConfirmBulkDelete(false)}>
        <DialogContent onClose={() => setConfirmBulkDelete(false)}>
          <DialogHeader>
            <DialogTitle>Remove {selectedCount} {selectedCount === 1 ? 'entry' : 'entries'}?</DialogTitle>
            <DialogDescription>
              All selected entries will be permanently removed from {POOL_LABEL[pool]}. Each removal is audited; partial failures (e.g. concurrent delete) are reported.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
            <Button variant="outline" onClick={() => setConfirmBulkDelete(false)} disabled={bulkDeletePending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleBulkDelete} disabled={bulkDeletePending} className="gap-1.5">
              <Trash2 className="h-3.5 w-3.5" />
              {bulkDeletePending ? 'Removing…' : `Remove ${selectedCount}`}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Card
        className={
          'relative overflow-hidden before:content-[\'\'] before:absolute before:left-0 before:top-0 before:bottom-0 before:w-[3px] ' +
          POOL_BORDER_TONE[pool]
        }
      >
        <CardContent className="p-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex items-center gap-3">
              <span className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
                <Icon className="h-4 w-4 text-text-secondary" />
              </span>
              <div>
                <h2 className="text-[13px] font-semibold text-text-primary">{POOL_LABEL[pool]}</h2>
                <p className="text-[11px] text-text-tertiary font-mono">
                  {list.isLoading
                    ? 'Loading…'
                    : `${totalLoaded.toLocaleString()} entr${totalLoaded === 1 ? 'y' : 'ies'} loaded`}
                </p>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={tacFilter}
                onChange={(e) => setTacFilter(e.target.value.replace(/\D/g, ''))}
                placeholder="TAC"
                inputMode="numeric"
                maxLength={8}
                className="h-8 w-[120px] font-mono text-xs"
                aria-label="Filter by TAC"
              />
              <Input
                value={deviceFilter}
                onChange={(e) => setDeviceFilter(e.target.value)}
                placeholder="Device model"
                className="h-8 w-[180px] text-xs"
                aria-label="Filter by device model"
              />
              <Select
                value={kindFilter}
                onChange={(e) => setKindFilter(e.target.value as IMEIEntryKind | '')}
                options={[
                  { value: '', label: 'All types' },
                  ...IMEI_ENTRY_KINDS.map((k) => ({ value: k, label: ENTRY_KIND_LABEL[k] })),
                ]}
                className="h-8 w-[140px] text-xs"
                aria-label="Filter by entry type"
              />
              {onSwitchToImport && (
                <Button variant="outline" size="sm" onClick={onSwitchToImport} className="gap-1.5">
                  <Upload className="h-3.5 w-3.5" />
                  Bulk Import
                </Button>
              )}
              <Button size="sm" onClick={() => setShowAdd(true)} className="gap-1.5">
                <Plus className="h-3.5 w-3.5" />
                Add Entry
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {selectedCount > 0 && (
        <Card className="border-accent/40 bg-accent-dim/30">
          <CardContent className="p-3">
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs font-medium text-text-primary">
                {selectedCount} selected
              </span>
              <div className="flex items-center gap-2">
                <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
                  Clear
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setConfirmBulkDelete(true)}
                  disabled={bulkDeletePending}
                  className="gap-1.5"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  {`Delete ${selectedCount}`}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {list.isLoading ? (
            <div className="p-4 space-y-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : allRows.length === 0 ? (
            <EmptyState
              icon={Icon}
              title={`No entries in ${POOL_LABEL[pool]}`}
              description="Register your first device or import a CSV. Entries are scoped to your tenant and audited."
              ctaLabel="Add Entry"
              onCta={() => setShowAdd(true)}
            />
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[36px]">
                      <Checkbox
                        checked={allVisibleSelected}
                        onChange={toggleAll}
                        aria-label="Select all visible rows"
                      />
                    </TableHead>
                    <TableHead>IMEI / TAC</TableHead>
                    <TableHead>Device Model</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead className="text-right">Bound SIMs</TableHead>
                    {pool === 'greylist' && <TableHead>Quarantine Reason</TableHead>}
                    {pool === 'blacklist' && (
                      <>
                        <TableHead>Block Reason</TableHead>
                        <TableHead>Source</TableHead>
                      </>
                    )}
                    <TableHead>Description</TableHead>
                    <TableHead className="w-[50px]" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {allRows.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className="w-[36px]">
                        <Checkbox
                          checked={selected.has(entry.id)}
                          onChange={() => toggleRow(entry.id)}
                          aria-label={`Select ${entry.imei_or_tac}`}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-[12px] text-text-primary">
                        {entry.imei_or_tac}
                      </TableCell>
                      <TableCell className="text-text-secondary">
                        {entry.device_model ?? <span className="text-text-tertiary">—</span>}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-mono uppercase tracking-wider text-[10px]">
                          {ENTRY_KIND_LABEL[entry.kind]}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right font-mono">
                        {entry.bound_sims_count > 0 ? (
                          <Button
                            type="button"
                            variant="link"
                            size="xs"
                            onClick={() => navigate(buildBoundFilterUrl(entry))}
                            className="h-auto p-0 font-mono text-accent hover:underline"
                            aria-label={`View ${entry.bound_sims_count} bound SIMs`}
                          >
                            {entry.bound_sims_count.toLocaleString()}
                          </Button>
                        ) : (
                          <span className="text-text-tertiary">0</span>
                        )}
                      </TableCell>
                      {pool === 'greylist' && (
                        <TableCell className="text-text-secondary text-xs max-w-[260px] truncate">
                          {entry.quarantine_reason ?? <span className="text-text-tertiary">—</span>}
                        </TableCell>
                      )}
                      {pool === 'blacklist' && (
                        <>
                          <TableCell className="text-text-secondary text-xs max-w-[260px] truncate">
                            {entry.block_reason ?? <span className="text-text-tertiary">—</span>}
                          </TableCell>
                          <TableCell>
                            {entry.imported_from ? (
                              <Badge variant="secondary" className="text-[10px]">
                                {IMPORTED_FROM_LABEL[entry.imported_from]}
                              </Badge>
                            ) : (
                              <span className="text-text-tertiary">—</span>
                            )}
                          </TableCell>
                        </>
                      )}
                      <TableCell className="text-text-tertiary text-xs max-w-[200px] truncate">
                        {entry.description ?? '—'}
                      </TableCell>
                      <TableCell className="w-[50px]">
                        <RowActionsMenu
                          actions={[
                            {
                              label: 'Move to other pool…',
                              icon: ArrowRightLeft,
                              onClick: () => startMove(entry),
                            },
                            {
                              label: 'Delete',
                              icon: Trash2,
                              variant: 'destructive',
                              onClick: () => setConfirmDelete(entry),
                              separator: true,
                            },
                          ]}
                        />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {list.hasNextPage && (
                <div className="flex items-center justify-center border-t border-border-subtle p-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => list.fetchNextPage()}
                    disabled={list.isFetchingNextPage}
                    className="gap-1.5"
                  >
                    {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
                    {!list.isFetchingNextPage && <ChevronDown className="h-3.5 w-3.5" />}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
