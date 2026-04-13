import { useState } from 'react'
import type React from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { useNavigate } from 'react-router-dom'
import {
  RefreshCw,
  AlertCircle,
  Radio,
  Plus,
  Loader2,
  Download,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { SlidePanel } from '@/components/ui/slide-panel'
import { RowActionsMenu } from '@/components/shared/row-actions-menu'
import { EmptyState } from '@/components/shared/empty-state'
import { SavedViewsMenu } from '@/components/shared/saved-views-menu'
import { useExport } from '@/hooks/use-export'
import { useOperatorList, useRealtimeOperatorHealth, useCreateOperator, useOperatorGrants, useAssignOperator, useRemoveOperatorGrant } from '@/hooks/use-operators'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { toast } from 'sonner'
import type { Operator } from '@/types/operator'
import { Skeleton } from '@/components/ui/skeleton'
import { RAT_DISPLAY } from '@/lib/constants'
import { formatBytes, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

const ADAPTER_DISPLAY: Record<string, string> = {
  mock: 'Mock',
  radius: 'RADIUS',
  diameter: 'Diameter',
  sba: '5G SBA',
}

const FAILOVER_DISPLAY: Record<string, string> = {
  reject: 'reject',
  fallback_to_next: 'fallback',
  queue_with_timeout: 'queue',
}

function healthColor(status: string) {
  switch (status) {
    case 'healthy': return 'var(--color-success)'
    case 'degraded': return 'var(--color-warning)'
    case 'down': return 'var(--color-danger)'
    default: return 'var(--color-text-tertiary)'
  }
}

function healthGlow(status: string) {
  switch (status) {
    case 'healthy': return '0 0 8px rgba(0,255,136,0.4)'
    case 'degraded': return '0 0 8px rgba(255,184,0,0.4)'
    case 'down': return '0 0 8px rgba(255,68,102,0.4)'
    default: return 'none'
  }
}

function healthVariant(status: string): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'down': return 'danger'
    default: return 'secondary'
  }
}

function MetricBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex-1 rounded-md bg-bg-hover px-3 py-2 text-center min-w-0">
      <div className="font-mono text-sm font-semibold text-text-primary truncate">{value}</div>
      <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-0.5">{label}</div>
    </div>
  )
}

function OperatorCard({ operator, onClick, assigned }: { operator: Operator; onClick: () => void; assigned?: boolean }) {
  const failoverLabel = FAILOVER_DISPLAY[operator.failover_policy] ?? operator.failover_policy

  return (
    <Card
      className="card-hover cursor-pointer p-5 space-y-4 relative overflow-hidden"
      onClick={onClick}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <span
            className="h-3 w-3 rounded-full flex-shrink-0 pulse-dot"
            style={{
              backgroundColor: healthColor(operator.health_status),
              boxShadow: healthGlow(operator.health_status),
            }}
          />
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-text-primary truncate">{operator.name}</h3>
            <p className="font-mono text-[11px] text-text-tertiary">
              {operator.code} · {operator.mcc}/{operator.mnc}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          {assigned && <Badge variant="secondary" className="text-[10px]">Assigned</Badge>}
          <Badge variant={healthVariant(operator.health_status)} className="text-[10px] flex-shrink-0">
            {operator.health_status.toUpperCase()}
          </Badge>
        </div>
      </div>

      <div className="flex gap-2">
        <MetricBox
          label="SIMs"
          value={operator.sim_count > 0 ? operator.sim_count.toLocaleString() : '—'}
        />
        <MetricBox
          label="Sessions"
          value={operator.active_sessions > 0 ? operator.active_sessions.toString() : '0'}
        />
        <MetricBox
          label="Traffic"
          value={operator.total_traffic_bytes > 0 ? formatBytes(operator.total_traffic_bytes) : '—'}
        />
      </div>

      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1 flex-wrap">
          <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-secondary font-medium">
            {ADAPTER_DISPLAY[operator.adapter_type] ?? operator.adapter_type}
          </span>
          {operator.supported_rat_types.slice(0, 4).map((rat) => (
            <span
              key={rat}
              className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium"
            >
              {RAT_DISPLAY[rat] ?? rat}
            </span>
          ))}
          {operator.supported_rat_types.length > 4 && (
            <span className="text-[10px] text-text-tertiary">+{operator.supported_rat_types.length - 4}</span>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between text-[10px] text-text-tertiary border-t border-border pt-2">
        <span>
          Failover: <span className="font-mono text-text-secondary">{failoverLabel}</span>
        </span>
        <span>
          {operator.last_health_check
            ? `Last: ${timeAgo(operator.last_health_check)}`
            : 'No health check'}
        </span>
      </div>
    </Card>
  )
}

function OperatorCardSkeleton() {
  return (
    <Card className="p-5 space-y-4">
      <div className="flex justify-between">
        <div className="flex gap-3">
          <Skeleton className="h-3 w-3 rounded-full" />
          <div>
            <Skeleton className="h-4 w-28 mb-1" />
            <Skeleton className="h-3 w-20" />
          </div>
        </div>
        <Skeleton className="h-5 w-16" />
      </div>
      <div className="flex gap-2">
        <Skeleton className="flex-1 h-12 rounded-md" />
        <Skeleton className="flex-1 h-12 rounded-md" />
        <Skeleton className="flex-1 h-12 rounded-md" />
      </div>
      <Skeleton className="h-5 w-full" />
      <Skeleton className="h-4 w-full" />
    </Card>
  )
}

const ADAPTER_OPTIONS = [
  { value: 'mock', label: 'Mock' },
  { value: 'radius', label: 'RADIUS' },
  { value: 'diameter', label: 'Diameter' },
  { value: 'sba', label: '5G SBA' },
]

const PROTOCOL_OPTIONS = ['radius', 'diameter', 'sba'] as const

const RAT_TYPE_OPTIONS = ['nb_iot', 'lte_m', 'lte', 'nr_5g']

function CreateOperatorDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [form, setForm] = useState({
    name: '',
    code: '',
    mcc: '',
    mnc: '',
    adapter_type: 'mock',
    supported_protocols: [] as string[],
    supported_rat_types: [] as string[],
  })
  const [error, setError] = useState<string | null>(null)
  const createMutation = useCreateOperator()

  const toggleProtocol = (proto: string) => {
    setForm((f) => ({
      ...f,
      supported_protocols: f.supported_protocols.includes(proto)
        ? f.supported_protocols.filter((p) => p !== proto)
        : [...f.supported_protocols, proto],
    }))
  }

  const toggleRat = (rat: string) => {
    setForm((f) => ({
      ...f,
      supported_rat_types: f.supported_rat_types.includes(rat)
        ? f.supported_rat_types.filter((r) => r !== rat)
        : [...f.supported_rat_types, rat],
    }))
  }

  const handleSubmit = async () => {
    setError(null)
    if (!form.name.trim()) { setError('Operator name is required'); return }
    if (!form.code.trim()) { setError('Operator code is required'); return }
    if (!form.mcc.trim()) { setError('MCC is required'); return }
    if (!form.mnc.trim()) { setError('MNC is required'); return }
    try {
      await createMutation.mutateAsync({
        name: form.name.trim(),
        code: form.code.trim(),
        mcc: form.mcc.trim(),
        mnc: form.mnc.trim(),
        adapter_type: form.adapter_type,
        supported_rat_types: form.supported_rat_types,
      })
      setForm({ name: '', code: '', mcc: '', mnc: '', adapter_type: 'mock', supported_protocols: [], supported_rat_types: [] })
      onClose()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      setError(msg ?? 'Failed to create operator')
    }
  }

  return (
    <SlidePanel open={open} onOpenChange={(v) => { if (!v) onClose() }} title="Create Operator" description="Add a new mobile network operator." width="md">
      <div className="space-y-4">
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Name *</label>
          <Input
            placeholder="e.g. Turkcell"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            className="h-8 text-sm"
          />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Code *</label>
          <Input
            placeholder="e.g. TURKCELL"
            value={form.code}
            onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
            className="h-8 text-sm"
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">MCC *</label>
            <Input
              placeholder="e.g. 286"
              value={form.mcc}
              onChange={(e) => setForm((f) => ({ ...f, mcc: e.target.value }))}
              className="h-8 text-sm"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">MNC *</label>
            <Input
              placeholder="e.g. 01"
              value={form.mnc}
              onChange={(e) => setForm((f) => ({ ...f, mnc: e.target.value }))}
              className="h-8 text-sm"
            />
          </div>
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Adapter Type *</label>
          <Select
            value={form.adapter_type}
            onChange={(e) => setForm((f) => ({ ...f, adapter_type: e.target.value }))}
            className="h-8 text-sm"
            options={ADAPTER_OPTIONS}
          />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Supported Protocols</label>
          <div className="flex flex-wrap gap-2">
            {PROTOCOL_OPTIONS.map((proto) => (
              <Button
                key={proto}
                type="button"
                variant="outline"
                size="sm"
                onClick={() => toggleProtocol(proto)}
                className={cn(
                  'px-2.5 py-1 h-auto text-xs font-mono border transition-colors',
                  form.supported_protocols.includes(proto)
                    ? 'border-accent bg-accent-dim text-accent hover:bg-accent-dim hover:text-accent'
                    : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
                )}
              >
                {proto === 'sba' ? '5G SBA' : proto.toUpperCase()}
              </Button>
            ))}
          </div>
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Supported RAT Types</label>
          <div className="flex flex-wrap gap-2">
            {RAT_TYPE_OPTIONS.map((rat) => (
              <Button
                key={rat}
                type="button"
                variant="outline"
                size="sm"
                onClick={() => toggleRat(rat)}
                className={cn(
                  'px-2.5 py-1 h-auto text-xs font-mono border transition-colors',
                  form.supported_rat_types.includes(rat)
                    ? 'border-accent bg-accent-dim text-accent hover:bg-accent-dim hover:text-accent'
                    : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
                )}
              >
                {RAT_DISPLAY[rat] ?? rat}
              </Button>
            ))}
          </div>
        </div>
        {error && <p className="text-xs text-danger">{error}</p>}
      </div>
      <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
        <Button variant="outline" size="sm" onClick={onClose} disabled={createMutation.isPending}>
          Cancel
        </Button>
        <Button size="sm" onClick={handleSubmit} disabled={createMutation.isPending} className="gap-1.5">
          {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Create Operator
        </Button>
      </div>
    </SlidePanel>
  )
}

export default function OperatorListPage() {
  const navigate = useNavigate()
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const { data: operators, isLoading, isError, refetch } = useOperatorList()
  const { data: grants = [] } = useOperatorGrants()
  const assignMutation = useAssignOperator()
  const removeMutation = useRemoveOperatorGrant()
  useRealtimeOperatorHealth()
  const { exportCSV, exporting } = useExport('operators')

  const grantedOperatorIds = new Set(grants.map((g) => g.operator_id))
  const grantByOperatorId = Object.fromEntries(grants.map((g) => [g.operator_id, g]))

  const handleAssign = async (operatorId: string) => {
    try {
      await assignMutation.mutateAsync({ operator_id: operatorId })
      toast.success('Operator assigned to tenant')
    } catch { /* interceptor */ }
  }

  const handleUnassign = async (operatorId: string) => {
    const grant = grantByOperatorId[operatorId]
    if (!grant) return
    try {
      await removeMutation.mutateAsync(grant.id)
      toast.success('Operator removed from tenant')
    } catch { /* interceptor */ }
  }

  const toggleSelect = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else if (next.size < 3) next.add(id)
      return next
    })
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load operators</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch operator data. Please try again.</p>
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
        <h1 className="text-[16px] font-semibold text-text-primary">Operators</h1>
        <div className="flex items-center gap-2">
          {selectedIds.size >= 2 && (
            <Button
              variant="outline"
              size="sm"
              className="gap-2"
              onClick={() => navigate(`/operators/compare?ids=${Array.from(selectedIds).join(',')}`)}
            >
              Compare ({selectedIds.size})
            </Button>
          )}
          <SavedViewsMenu page="operators" />
          <Button variant="outline" size="sm" className="gap-2" onClick={() => exportCSV()} disabled={exporting}>
            {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            Export
          </Button>
          <Button className="gap-2" size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" />
            Create Operator
          </Button>
        </div>
      </div>

      {isLoading && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <OperatorCardSkeleton key={i} />
          ))}
        </div>
      )}

      {!isLoading && (!operators || operators.length === 0) && (
        <EmptyState
          icon={Radio}
          title="No operators configured"
          description="Create your first operator to get started."
          ctaLabel="Create Operator"
          onCta={() => setCreateOpen(true)}
        />
      )}

      {!isLoading && operators && operators.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {operators.map((op, i) => (
            <div key={op.id} style={{ animationDelay: `${i * 50}ms` }} className="animate-in fade-in slide-in-from-bottom-1 relative group" data-row-index={i} data-href={`/operators/${op.id}`}>
              <OperatorCard
                operator={op}
                onClick={() => navigate(`/operators/${op.id}`)}
                assigned={grantedOperatorIds.has(op.id)}
              />
              <div className="absolute top-2 right-2 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <Checkbox
                  checked={selectedIds.has(op.id)}
                  onClick={(e: React.MouseEvent) => toggleSelect(op.id, e)}
                  disabled={!selectedIds.has(op.id) && selectedIds.size >= 3}
                  aria-label={`Select ${op.name}`}
                />
                <RowActionsMenu
                  actions={[
                    { label: 'View Details', onClick: () => navigate(`/operators/${op.id}`) },
                    ...(grantedOperatorIds.has(op.id)
                      ? [{ label: 'Remove from Tenant', onClick: () => handleUnassign(op.id), variant: 'destructive' as const }]
                      : [{ label: 'Assign to Tenant', onClick: () => handleAssign(op.id) }]),
                  ]}
                />
              </div>
            </div>
          ))}
        </div>
      )}

      {!isLoading && operators && operators.length > 0 && (
        <p className="text-center text-xs text-text-tertiary">
          Showing {operators.length} operator{operators.length !== 1 ? 's' : ''}
        </p>
      )}

      <CreateOperatorDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </div>
  )
}
