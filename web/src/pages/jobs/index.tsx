import { useState, useMemo, useEffect, useRef } from 'react'
import {
  Search,
  Filter,
  X,
  ChevronDown,
  Check,
  RefreshCw,
  AlertCircle,
  Play,
  XCircle,
  Loader2,
  Clock,
  ChevronRight,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
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
import { Sheet, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import {
  useJobList,
  useJobDetail,
  useJobErrors,
  useRetryJob,
  useCancelJob,
  useRealtimeJobProgress,
} from '@/hooks/use-jobs'
import type { Job, JobState } from '@/types/job'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'queued', label: 'Queued' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'cancelled', label: 'Cancelled' },
]

const TYPE_OPTIONS = [
  { value: '', label: 'All Types' },
  { value: 'bulk_import', label: 'Bulk Import' },
  { value: 'bulk_state_change', label: 'State Change' },
  { value: 'bulk_policy_assign', label: 'Policy Assign' },
  { value: 'bulk_operator_switch', label: 'Operator Switch' },
  { value: 'bulk_session_disconnect', label: 'Session Disconnect' },
  { value: 'bulk_ota', label: 'OTA Command' },
]

function stateVariant(state: JobState): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state) {
    case 'completed': return 'success'
    case 'running': return 'default'
    case 'queued': return 'secondary'
    case 'failed': return 'danger'
    case 'cancelled': return 'warning'
    default: return 'secondary'
  }
}

function typeLabel(type: string): string {
  const opt = TYPE_OPTIONS.find((o) => o.value === type)
  return opt?.label ?? type.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

function ProgressBar({ pct, state }: { pct: number; state: string }) {
  const color = state === 'failed' ? 'bg-danger' : state === 'completed' ? 'bg-success' : 'bg-accent'
  return (
    <div className="w-full h-2 bg-bg-hover rounded-full overflow-hidden">
      <div
        className={cn('h-full rounded-full transition-all duration-500', color)}
        style={{ width: `${Math.min(pct, 100)}%` }}
      />
    </div>
  )
}

export default function JobListPage() {
  const [filters, setFilters] = useState<{ type?: string; state?: string }>({})
  const [selectedJobId, setSelectedJobId] = useState<string>('')
  const [confirmAction, setConfirmAction] = useState<{ jobId: string; action: 'retry' | 'cancel' } | null>(null)
  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useJobList(filters)

  const { data: jobDetail } = useJobDetail(selectedJobId)
  const { data: jobErrors } = useJobErrors(selectedJobId)

  const retryMutation = useRetryJob()
  const cancelMutation = useCancelJob()

  useRealtimeJobProgress()

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

  const allJobs = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  const handleConfirmAction = async () => {
    if (!confirmAction) return
    try {
      if (confirmAction.action === 'retry') {
        await retryMutation.mutateAsync(confirmAction.jobId)
      } else {
        await cancelMutation.mutateAsync(confirmAction.jobId)
      }
      setConfirmAction(null)
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load jobs</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch job data. Please try again.</p>
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
        <h1 className="text-[16px] font-semibold text-text-primary">Jobs</h1>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-3 flex-wrap">
        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.type
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
            <Filter className="h-3 w-3" />
            <span>Type{filters.type ? `: ${typeLabel(filters.type)}` : ''}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {TYPE_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => setFilters((f) => ({ ...f, type: opt.value || undefined }))}
              >
                <span className="flex-1">{opt.label}</span>
                {filters.type === opt.value && <Check className="h-3.5 w-3.5 text-accent" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger className={cn(
            'flex items-center gap-1.5 px-3 py-1 text-xs rounded-full border transition-colors',
            filters.state
              ? 'border-accent/30 bg-accent-dim text-accent'
              : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary hover:text-text-primary',
          )}>
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

        {(filters.type || filters.state) && (
          <button
            onClick={() => setFilters({})}
            className="text-xs text-text-tertiary hover:text-accent transition-colors"
          >
            Clear all
          </button>
        )}
      </div>

      {/* Jobs Table */}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Type</TableHead>
                <TableHead>State</TableHead>
                <TableHead className="w-40">Progress</TableHead>
                <TableHead>Total</TableHead>
                <TableHead>Processed</TableHead>
                <TableHead>Failed</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-8" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 6 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 9 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && allJobs.length === 0 && (
                <TableRow>
                  <TableCell colSpan={9}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Clock className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No jobs found</h3>
                        <p className="text-xs text-text-secondary">
                          {filters.type || filters.state ? 'Try adjusting your filters.' : 'Jobs will appear here when bulk operations are started.'}
                        </p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {allJobs.map((job) => (
                <TableRow
                  key={job.id}
                  className="cursor-pointer hover:bg-bg-hover"
                  onClick={() => setSelectedJobId(job.id)}
                >
                  <TableCell>
                    <span className="text-xs font-medium text-text-primary">{typeLabel(job.type)}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(job.state)} className="gap-1">
                      {job.state === 'running' && (
                        <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                      )}
                      {job.state.toUpperCase()}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <ProgressBar pct={job.progress_pct} state={job.state} />
                      <span className="font-mono text-[11px] text-text-secondary w-10 text-right">
                        {Math.round(job.progress_pct)}%
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-primary">{job.total_items.toLocaleString()}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{job.processed_items.toLocaleString()}</span>
                  </TableCell>
                  <TableCell>
                    <span className={cn('font-mono text-xs', job.failed_items > 0 ? 'text-danger' : 'text-text-tertiary')}>
                      {job.failed_items.toLocaleString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">{job.duration ?? '-'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(job.created_at).toLocaleString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <ChevronRight className="h-3.5 w-3.5 text-text-tertiary" />
                  </TableCell>
                </TableRow>
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
            <button
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1"
            >
              Load more jobs
            </button>
          ) : allJobs.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allJobs.length} jobs
            </p>
          ) : null}
        </div>
      </Card>

      {/* Job Detail Panel */}
      <Sheet open={!!selectedJobId} onOpenChange={() => setSelectedJobId('')}>
        {jobDetail && (
          <div className="space-y-6">
            <SheetHeader>
              <SheetTitle>{typeLabel(jobDetail.type)}</SheetTitle>
            </SheetHeader>

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-xs text-text-secondary">State</span>
                <Badge variant={stateVariant(jobDetail.state)}>
                  {jobDetail.state.toUpperCase()}
                </Badge>
              </div>

              <div>
                <span className="text-xs text-text-secondary block mb-1.5">Progress</span>
                <ProgressBar pct={jobDetail.progress_pct} state={jobDetail.state} />
                <p className="text-xs text-text-tertiary mt-1">
                  {jobDetail.processed_items.toLocaleString()} / {jobDetail.total_items.toLocaleString()} items
                  {jobDetail.failed_items > 0 && (
                    <span className="text-danger ml-1">({jobDetail.failed_items} failed)</span>
                  )}
                </p>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <span className="text-xs text-text-secondary block mb-0.5">Duration</span>
                  <span className="text-sm text-text-primary font-mono">{jobDetail.duration ?? '-'}</span>
                </div>
                <div>
                  <span className="text-xs text-text-secondary block mb-0.5">Retries</span>
                  <span className="text-sm text-text-primary font-mono">{jobDetail.retry_count}/{jobDetail.max_retries}</span>
                </div>
                <div>
                  <span className="text-xs text-text-secondary block mb-0.5">Started</span>
                  <span className="text-[11px] text-text-secondary">
                    {jobDetail.started_at ? new Date(jobDetail.started_at).toLocaleString() : '-'}
                  </span>
                </div>
                <div>
                  <span className="text-xs text-text-secondary block mb-0.5">Completed</span>
                  <span className="text-[11px] text-text-secondary">
                    {jobDetail.completed_at ? new Date(jobDetail.completed_at).toLocaleString() : '-'}
                  </span>
                </div>
              </div>

              {/* Error Report */}
              {jobErrors && Array.isArray(jobErrors) && jobErrors.length > 0 && (
                <div>
                  <span className="text-xs text-text-secondary block mb-2">Error Report ({jobErrors.length} errors)</span>
                  <div className="max-h-48 overflow-y-auto rounded-[var(--radius-sm)] border border-border bg-bg-surface">
                    {jobErrors.map((err, idx) => (
                      <div key={idx} className="px-3 py-2 border-b border-border-subtle last:border-b-0 text-xs">
                        <p className="text-danger font-medium">{err.error_code ?? 'ERROR'}</p>
                        <p className="text-text-secondary mt-0.5">{err.error_message}</p>
                        {err.iccid && (
                          <p className="font-mono text-text-tertiary mt-0.5">ICCID: {err.iccid}</p>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Actions */}
              <div className="flex gap-2 pt-2 border-t border-border">
                {(jobDetail.state === 'failed' || jobDetail.state === 'completed') && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5 flex-1"
                    onClick={() => setConfirmAction({ jobId: jobDetail.id, action: 'retry' })}
                  >
                    <Play className="h-3.5 w-3.5" />
                    Retry
                  </Button>
                )}
                {(jobDetail.state === 'running' || jobDetail.state === 'queued') && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5 flex-1 border-danger/30 text-danger hover:bg-danger-dim"
                    onClick={() => setConfirmAction({ jobId: jobDetail.id, action: 'cancel' })}
                  >
                    <XCircle className="h-3.5 w-3.5" />
                    Cancel
                  </Button>
                )}
              </div>
            </div>
          </div>
        )}
      </Sheet>

      {/* Action Confirmation Dialog */}
      <Dialog open={!!confirmAction} onOpenChange={() => setConfirmAction(null)}>
        <DialogContent onClose={() => setConfirmAction(null)}>
          <DialogHeader>
            <DialogTitle>
              {confirmAction?.action === 'retry' ? 'Retry Job?' : 'Cancel Job?'}
            </DialogTitle>
            <DialogDescription>
              {confirmAction?.action === 'retry'
                ? 'This will create a new job that re-processes failed items.'
                : 'This will cancel the running job. Items already processed will not be rolled back.'}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>
              Go Back
            </Button>
            <Button
              variant={confirmAction?.action === 'cancel' ? 'destructive' : 'default'}
              onClick={handleConfirmAction}
              disabled={retryMutation.isPending || cancelMutation.isPending}
              className="gap-2"
            >
              {(retryMutation.isPending || cancelMutation.isPending) && <Loader2 className="h-4 w-4 animate-spin" />}
              {confirmAction?.action === 'retry' ? 'Retry' : 'Cancel Job'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
