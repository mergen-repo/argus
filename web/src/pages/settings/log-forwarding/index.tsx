// STORY-098 Task 6 — SCR-198 Settings → Log Forwarding (Syslog).
// List page with table layout (mirrors api-keys / imei-pools), per-row
// RowActionsMenu (Edit · Test · Disable/Enable · Delete), Add slide-panel,
// compact Delete confirm Dialog.

import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import {
  Plus,
  AlertCircle,
  RefreshCw,
  Loader2,
  Pencil,
  FlaskConical,
  Power,
  PowerOff,
  Trash2,
  Radio,
  Lock,
  Wifi,
  Network as NetworkIcon,
  CheckCircle2,
  CircleAlert,
  ChevronLeft,
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip } from '@/components/ui/tooltip'
import { EmptyState } from '@/components/shared/empty-state'
import { RowActionsMenu, type RowAction } from '@/components/shared/row-actions-menu'
import { cn } from '@/lib/utils'
import {
  type SyslogCategory,
  type SyslogDestination,
  type SyslogFormat,
  type SyslogTransport,
  SYSLOG_CATEGORY_LABEL,
  formToUpsertRequest,
  destinationToForm,
} from '@/types/log-forwarding'
import {
  useLogForwardingDelete,
  useLogForwardingList,
  useLogForwardingSetEnabled,
  useLogForwardingTest,
} from '@/hooks/use-log-forwarding'
import { DestinationFormPanel } from './destination-form-panel'

const TRANSPORT_ICON: Record<SyslogTransport, React.ElementType> = {
  udp: Wifi,
  tcp: NetworkIcon,
  tls: Lock,
}

function transportBadgeClass(t: SyslogTransport): string {
  switch (t) {
    case 'udp':
      return 'bg-bg-elevated text-text-secondary border-border'
    case 'tcp':
      return 'bg-accent-dim text-accent border-accent/30'
    case 'tls':
      return 'bg-success-dim text-success border-success/30'
  }
}

function formatBadgeClass(f: SyslogFormat): string {
  return f === 'rfc3164'
    ? 'bg-bg-elevated text-text-tertiary border-border'
    : 'bg-accent-dim text-accent border-accent/30'
}

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '—'
  const diff = Date.now() - then
  if (diff < 0) return 'just now'
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day}d ago`
  return new Date(iso).toLocaleDateString()
}

export default function LogForwardingPage() {
  const navigate = useNavigate()
  const { data, isLoading, isError, refetch, isFetching } = useLogForwardingList()
  const setEnabled = useLogForwardingSetEnabled()
  const deleteMutation = useLogForwardingDelete()
  const testMutation = useLogForwardingTest()

  const [panelOpen, setPanelOpen] = useState(false)
  const [editing, setEditing] = useState<SyslogDestination | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<SyslogDestination | null>(null)

  const destinations = useMemo<SyslogDestination[]>(() => data ?? [], [data])

  function openAdd() {
    setEditing(null)
    setPanelOpen(true)
  }

  function openEdit(d: SyslogDestination) {
    setEditing(d)
    setPanelOpen(true)
  }

  async function handleToggleEnabled(d: SyslogDestination) {
    try {
      await setEnabled.mutateAsync({ id: d.id, enabled: !d.enabled })
      toast.success(d.enabled ? 'Destination disabled' : 'Destination enabled')
    } catch {
      // global interceptor surfaces toast
    }
  }

  async function handleTestRow(d: SyslogDestination) {
    // Per-row Test rebuilds the request from the saved row. PEM material is
    // not returned by the API — the test will reuse server-side trust.
    const draft = destinationToForm(d)
    try {
      const result = await testMutation.mutateAsync(formToUpsertRequest(draft))
      if (result.ok) {
        toast.success(`"${d.name}" — connection OK`)
      } else {
        toast.error(`"${d.name}" — ${result.error ?? 'connection failed'}`)
      }
    } catch {
      // global interceptor surfaces toast
    }
  }

  async function handleConfirmDelete() {
    if (!confirmDelete) return
    try {
      await deleteMutation.mutateAsync(confirmDelete.id)
      toast.success(`"${confirmDelete.name}" deleted`)
      setConfirmDelete(null)
    } catch {
      // global interceptor surfaces toast
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-[var(--radius-lg)] border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">
            Failed to load destinations
          </h2>
          <p className="text-sm text-text-secondary mb-4">
            Unable to fetch syslog destinations.
          </p>
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
      {/* ── Header ─────────────────────────────────────────────── */}
      <div className="flex items-center justify-between mb-2 gap-3">
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={() => navigate('/settings')}
            aria-label="Back to Settings"
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <div>
            <h1 className="text-[16px] font-semibold text-text-primary leading-tight flex items-center gap-2">
              <Radio className="h-4 w-4 text-accent" />
              Log Forwarding
            </h1>
            <p className="text-xs text-text-tertiary mt-0.5">
              Forward audit, alert, and event-bus envelopes to your SIEM via RFC 3164 / RFC 5424.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="gap-2"
            onClick={() => refetch()}
            disabled={isFetching}
            aria-label="Refresh"
          >
            <RefreshCw className={cn('h-3.5 w-3.5', isFetching && 'animate-spin')} />
            Refresh
          </Button>
          <Button size="sm" className="gap-2" onClick={openAdd}>
            <Plus className="h-3.5 w-3.5" />
            Add Destination
          </Button>
        </div>
      </div>

      {/* ── Table ──────────────────────────────────────────────── */}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-48">Name</TableHead>
                <TableHead className="w-56">Endpoint</TableHead>
                <TableHead className="w-24">Transport</TableHead>
                <TableHead className="w-24">Format</TableHead>
                <TableHead>Categories</TableHead>
                <TableHead className="w-44">Status</TableHead>
                <TableHead className="w-12 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 7 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className="h-4 w-20" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && destinations.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7}>
                    <EmptyState
                      icon={Radio}
                      title="No syslog destinations configured"
                      description="Forward Argus events (audit, alerts, sessions, policy, IMEI) to your SIEM via RFC 3164 or RFC 5424 over UDP, TCP, or TLS."
                      ctaLabel="Add Destination"
                      onCta={openAdd}
                    />
                  </TableCell>
                </TableRow>
              )}

              {!isLoading &&
                destinations.map((d) => (
                  <DestinationRow
                    key={d.id}
                    destination={d}
                    onEdit={() => openEdit(d)}
                    onTest={() => handleTestRow(d)}
                    onToggleEnabled={() => handleToggleEnabled(d)}
                    onDelete={() => setConfirmDelete(d)}
                    pendingTest={testMutation.isPending}
                    pendingToggle={setEnabled.isPending}
                  />
                ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && destinations.length > 0 && (
          <div className="px-4 py-3 border-t border-border">
            <p className="text-center text-xs text-text-tertiary">
              {destinations.length} destination{destinations.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      {/* ── Add/Edit slide-panel ───────────────────────────────── */}
      <DestinationFormPanel
        open={panelOpen}
        onOpenChange={(open) => {
          setPanelOpen(open)
          if (!open) setEditing(null)
        }}
        editing={editing}
      />

      {/* ── Delete confirm dialog ──────────────────────────────── */}
      <Dialog
        open={!!confirmDelete}
        onOpenChange={(open) => {
          if (!open) setConfirmDelete(null)
        }}
      >
        <DialogContent onClose={() => setConfirmDelete(null)}>
          <DialogHeader>
            <DialogTitle>Delete destination?</DialogTitle>
            <DialogDescription>
              {`This permanently removes "${confirmDelete?.name ?? ''}" and stops forwarding to ${
                confirmDelete?.host ?? ''
              }:${confirmDelete?.port ?? ''}. Past events already delivered to the SIEM are retained there.`}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setConfirmDelete(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={handleConfirmDelete}
              disabled={deleteMutation.isPending}
              className="gap-2"
            >
              {deleteMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ─── Row component ──────────────────────────────────────────────────────────

interface DestinationRowProps {
  destination: SyslogDestination
  onEdit: () => void
  onTest: () => void
  onToggleEnabled: () => void
  onDelete: () => void
  pendingTest: boolean
  pendingToggle: boolean
}

function DestinationRow({
  destination: d,
  onEdit,
  onTest,
  onToggleEnabled,
  onDelete,
  pendingTest,
  pendingToggle,
}: DestinationRowProps) {
  const TIcon = TRANSPORT_ICON[d.transport]

  const actions: RowAction[] = [
    { label: 'Edit', icon: Pencil, onClick: onEdit },
    { label: 'Test connection', icon: FlaskConical, onClick: onTest, disabled: pendingTest },
    {
      label: d.enabled ? 'Disable' : 'Enable',
      icon: d.enabled ? PowerOff : Power,
      onClick: onToggleEnabled,
      disabled: pendingToggle,
      separator: true,
    },
    {
      label: 'Delete',
      icon: Trash2,
      onClick: onDelete,
      variant: 'destructive',
      separator: true,
    },
  ]

  return (
    <TableRow>
      <TableCell>
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-[12px] font-medium text-text-primary truncate max-w-44">
            {d.name}
          </span>
          {d.severity_floor !== null && (
            <span className="text-[10px] text-text-tertiary">
              floor: severity ≤ {d.severity_floor}
            </span>
          )}
        </div>
      </TableCell>
      <TableCell>
        <span className="font-mono text-[12px] text-text-secondary">
          {d.host}
          <span className="text-text-tertiary">:</span>
          <span className="text-text-primary">{d.port}</span>
        </span>
      </TableCell>
      <TableCell>
        <Badge
          className={cn(
            'gap-1 font-mono text-[10px] uppercase tracking-wide',
            transportBadgeClass(d.transport),
          )}
        >
          <TIcon className="h-3 w-3" />
          {d.transport}
        </Badge>
      </TableCell>
      <TableCell>
        <Badge className={cn('font-mono text-[10px]', formatBadgeClass(d.format))}>
          {d.format.toUpperCase()}
        </Badge>
      </TableCell>
      <TableCell>
        <CategoryChips categories={d.filter_categories} />
      </TableCell>
      <TableCell>
        <StatusCell destination={d} />
      </TableCell>
      <TableCell className="text-right">
        <RowActionsMenu actions={actions} />
      </TableCell>
    </TableRow>
  )
}

function CategoryChips({ categories }: { categories: SyslogCategory[] }) {
  if (categories.length === 0) {
    return <span className="text-[11px] text-text-tertiary italic">none</span>
  }
  const visible = categories.slice(0, 3)
  const overflow = categories.length - visible.length
  return (
    <div className="flex flex-wrap gap-1">
      {visible.map((c) => (
        <Badge
          key={c}
          variant="outline"
          className="text-[10px] font-mono lowercase border-border"
        >
          {SYSLOG_CATEGORY_LABEL[c].toLowerCase()}
        </Badge>
      ))}
      {overflow > 0 && (
        <Tooltip content={categories.slice(3).map((c) => SYSLOG_CATEGORY_LABEL[c]).join(', ')}>
          <Badge variant="secondary" className="text-[10px] font-mono">
            +{overflow}
          </Badge>
        </Tooltip>
      )}
    </div>
  )
}

function StatusCell({ destination: d }: { destination: SyslogDestination }) {
  if (!d.enabled) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-text-tertiary">
        <span className="h-1.5 w-1.5 rounded-full bg-text-tertiary" aria-hidden />
        Disabled
      </span>
    )
  }
  if (d.last_error) {
    return (
      <Tooltip content={d.last_error} side="left">
        <div className="inline-flex flex-col items-start gap-0.5">
          <span className="inline-flex items-center gap-1.5 text-[11px] text-danger font-medium">
            <CircleAlert className="h-3 w-3" />
            Last delivery failed
          </span>
          {d.last_delivery_at && (
            <span className="text-[10px] text-text-tertiary font-mono">
              {timeAgo(d.last_delivery_at)}
            </span>
          )}
        </div>
      </Tooltip>
    )
  }
  if (d.last_delivery_at) {
    return (
      <div className="inline-flex flex-col items-start gap-0.5">
        <span className="inline-flex items-center gap-1.5 text-[11px] text-success font-medium">
          <CheckCircle2 className="h-3 w-3" />
          Delivering
        </span>
        <span className="text-[10px] text-text-tertiary font-mono">
          last {timeAgo(d.last_delivery_at)}
        </span>
      </div>
    )
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-text-secondary">
      <span className="h-1.5 w-1.5 rounded-full bg-success" aria-hidden />
      Enabled — awaiting first event
    </span>
  )
}
