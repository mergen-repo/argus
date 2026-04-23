import { useState, useMemo } from 'react'
import type React from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { useNavigate } from 'react-router-dom'
import {
  Search,
  Plus,
  Shield,
  X,
  Filter,
  Check,
  AlertCircle,
  RefreshCw,
  Loader2,
  Trash2,
  Edit,
  Download,
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
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { usePolicyList, useCreatePolicy, useDeletePolicy } from '@/hooks/use-policies'
import { useUndo } from '@/hooks/use-undo'
import type { PolicyListItem } from '@/types/policy'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { RowActionsMenu } from '@/components/shared/row-actions-menu'
import { RowQuickPeek } from '@/components/shared/row-quick-peek'
import { EmptyState } from '@/components/shared/empty-state'
import { useExport } from '@/hooks/use-export'

const STATUS_OPTIONS = [
  { value: '', label: 'All Status' },
  { value: 'active', label: 'Active' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'archived', label: 'Archived' },
]

const SCOPE_OPTIONS = [
  { value: 'global', label: 'Global' },
  { value: 'operator', label: 'Operator' },
  { value: 'apn', label: 'APN' },
  { value: 'sim', label: 'SIM' },
]

function stateVariant(state: string): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state) {
    case 'active': return 'success'
    case 'disabled': return 'warning'
    case 'archived': return 'secondary'
    default: return 'default'
  }
}

const DEFAULT_DSL = `POLICY "new-policy" {
    MATCH {
        apn = "default"
    }

    RULES {
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps
        session_timeout = 24h
        idle_timeout = 1h
        max_sessions = 1
    }
}
`

export default function PolicyListPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState<string | null>(null)
  const [newPolicy, setNewPolicy] = useState({ name: '', description: '', scope: 'global' })
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const toggleSelect = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else if (next.size < 3) next.add(id)
      return next
    })
  }

  const createMutation = useCreatePolicy()
  const deleteMutation = useDeletePolicy()
  const { register: registerUndo } = useUndo([['policies']])
  const { exportCSV, exporting } = useExport('policies')

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = usePolicyList(search || undefined, statusFilter || undefined)

  const allPolicies = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  const handleSearch = () => {
    setSearch(searchInput.trim())
  }

  const handleCreate = async () => {
    if (!newPolicy.name.trim()) return
    try {
      const dslSource = DEFAULT_DSL.replace('"new-policy"', `"${newPolicy.name.toLowerCase().replace(/\s+/g, '-')}"`)
      const policy = await createMutation.mutateAsync({
        name: newPolicy.name,
        description: newPolicy.description || undefined,
        scope: newPolicy.scope,
        dsl_source: dslSource,
      })
      setCreateDialogOpen(false)
      setNewPolicy({ name: '', description: '', scope: 'global' })
      navigate(`/policies/${policy.id}`)
    } catch {
      // handled by api interceptor
    }
  }

  const handleDelete = async () => {
    if (!deleteDialogOpen) return
    try {
      const result = await deleteMutation.mutateAsync(deleteDialogOpen)
      if (result?.undoActionId) {
        registerUndo(result.undoActionId, 'Policy deleted')
      }
      setDeleteDialogOpen(null)
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load policies</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch policy data. Please try again.</p>
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
        <div className="flex items-center gap-3">
          <Shield className="h-5 w-5 text-accent" />
          <h1 className="text-[16px] font-semibold text-text-primary">Policies</h1>
        </div>
        <div className="flex items-center gap-2">
          {selectedIds.size >= 2 && (
            <Button
              variant="outline"
              size="sm"
              className="gap-2"
              onClick={() => navigate(`/policies/compare?ids=${Array.from(selectedIds).join(',')}`)}
            >
              Compare ({selectedIds.size})
            </Button>
          )}
          <Button variant="outline" size="sm" className="gap-2" onClick={() => exportCSV({ state: statusFilter, q: search })} disabled={exporting}>
            {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            Export
          </Button>
          <Button className="gap-2" size="sm" onClick={() => setCreateDialogOpen(true)}>
            <Plus className="h-4 w-4" />
            New Policy
          </Button>
        </div>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            placeholder="Search policies..."
            className="pl-9 h-8 text-sm"
          />
          {searchInput && (
            <Button
              variant="ghost"
              size="icon"
              onClick={() => { setSearchInput(''); setSearch('') }}
              className="absolute right-2 top-1/2 -translate-y-1/2 h-5 w-5 text-text-tertiary hover:text-text-primary"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            statusFilter
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>Status{statusFilter ? `: ${statusFilter.toUpperCase()}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {STATUS_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setStatusFilter(opt.value)}
              >
                <span className="flex-1">{opt.label}</span>
                {statusFilter === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {statusFilter && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setStatusFilter('')}
            className="text-xs text-text-tertiary hover:text-accent h-auto p-0"
          >
            Clear filter
          </Button>
        )}
      </div>

      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-8" />
                <TableHead>Name</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Active Version</TableHead>
                <TableHead>SIM Count</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last Modified</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 8 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-36" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-10" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                  </TableRow>
                ))}

              {!isLoading && allPolicies.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8}>
                    {search || statusFilter ? (
                      <EmptyState
                        icon={Search}
                        title="No policies match your filters"
                        description="Try adjusting your search or filter criteria."
                        ctaLabel="Clear Filters"
                        onCta={() => { setSearch(''); setSearchInput(''); setStatusFilter('') }}
                      />
                    ) : (
                      <EmptyState
                        icon={Shield}
                        title="No policies yet"
                        description="Create your first policy to start enforcing usage rules."
                        ctaLabel="New Policy"
                        onCta={() => setCreateDialogOpen(true)}
                      />
                    )}
                  </TableCell>
                </TableRow>
              )}

              {allPolicies.map((policy, idx) => (
                <TableRow
                  key={policy.id}
                  data-row-index={idx}
                  data-href={`/policies/${policy.id}`}
                  className="cursor-pointer"
                  onClick={() => navigate(`/policies/${policy.id}`)}
                >
                  <TableCell onClick={(e) => toggleSelect(policy.id, e)}>
                    <Checkbox
                      checked={selectedIds.has(policy.id)}
                      onClick={(e: React.MouseEvent) => toggleSelect(policy.id, e)}
                      disabled={!selectedIds.has(policy.id) && selectedIds.size >= 3}
                      aria-label={`Select ${policy.name}`}
                    />
                  </TableCell>
                  <TableCell>
                    <RowQuickPeek
                      title={policy.name}
                      fields={[
                        { label: 'Scope', value: policy.scope },
                        { label: 'State', value: policy.state },
                        { label: 'Version', value: policy.active_version != null ? `v${policy.active_version}` : '—' },
                        { label: 'SIMs', value: policy.sim_count.toLocaleString() },
                        { label: 'Modified', value: new Date(policy.updated_at).toLocaleDateString() },
                      ]}
                    >
                      <div>
                        <span className="text-sm font-medium text-text-primary hover:text-accent transition-colors">
                          {policy.name}
                        </span>
                        {policy.description && (
                          <p className="text-xs text-text-tertiary truncate max-w-xs mt-0.5">
                            {policy.description}
                          </p>
                        )}
                      </div>
                    </RowQuickPeek>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs font-mono px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary">
                      {policy.scope}
                    </span>
                  </TableCell>
                  <TableCell>
                    {policy.active_version != null ? (
                      <span className="text-sm font-mono text-text-primary">v{policy.active_version}</span>
                    ) : (
                      <span className="text-xs text-text-tertiary">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="text-sm font-mono text-text-secondary">
                      {policy.sim_count.toLocaleString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(policy.state)} className="gap-1">
                      {policy.state === 'active' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {policy.state.toUpperCase()}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(policy.updated_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <RowActionsMenu
                      actions={[
                        { label: 'Edit', icon: Edit, onClick: () => navigate(`/policies/${policy.id}`) },
                        { label: 'Delete', icon: Trash2, onClick: () => setDeleteDialogOpen(policy.id), variant: 'destructive', separator: true },
                      ]}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        <div className="px-4 py-3 border-t border-border-subtle">
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
              Load more policies
            </Button>
          ) : allPolicies.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allPolicies.length} policies
            </p>
          ) : null}
        </div>
      </Card>

      {/* Create Policy SlidePanel */}
      <SlidePanel open={createDialogOpen} onOpenChange={setCreateDialogOpen} title="Create Policy" description="Define a new policy with DSL rules for SIM management." width="md">
        <div className="space-y-4">
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">Name</label>
            <Input
              value={newPolicy.name}
              onChange={(e) => setNewPolicy((p) => ({ ...p, name: e.target.value }))}
              placeholder="e.g. iot-fleet-standard"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">Description</label>
            <Input
              value={newPolicy.description}
              onChange={(e) => setNewPolicy((p) => ({ ...p, description: e.target.value }))}
              placeholder="Optional description..."
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">Scope</label>
            <Select
              value={newPolicy.scope}
              onChange={(e) => setNewPolicy((p) => ({ ...p, scope: e.target.value }))}
              options={SCOPE_OPTIONS}
            />
          </div>
        </div>
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
          <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>Cancel</Button>
          <Button
            onClick={handleCreate}
            disabled={!newPolicy.name.trim() || createMutation.isPending}
            className="gap-2"
          >
            {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
            Create Policy
          </Button>
        </div>
      </SlidePanel>

      {/* Delete Confirmation Dialog */}
      <Dialog open={!!deleteDialogOpen} onOpenChange={() => setDeleteDialogOpen(null)}>
        <DialogContent onClose={() => setDeleteDialogOpen(null)}>
          <DialogHeader>
            <DialogTitle>Delete Policy</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete this policy? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(null)}>Cancel</Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
              className="gap-2"
            >
              {deleteMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
