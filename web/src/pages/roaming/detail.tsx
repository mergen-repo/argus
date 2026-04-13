import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  AlertTriangle,
  Calendar,
  RefreshCw,
  AlertCircle,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { InfoRow } from '@/components/ui/info-row'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  useRoamingAgreement,
  useUpdateRoamingAgreement,
  useTerminateRoamingAgreement,
} from '@/hooks/use-roaming-agreements'
import type { RoamingAgreement, AgreementType, AgreementState, CostTerms, SLATerms } from '@/types/roaming'

function agreementStateBadge(state: AgreementState) {
  switch (state) {
    case 'active': return <Badge variant="success">active</Badge>
    case 'draft': return <Badge variant="warning">draft</Badge>
    case 'expired': return <Badge variant="danger">expired</Badge>
    case 'terminated': return <Badge variant="secondary">terminated</Badge>
    default: return <Badge variant="secondary">{state}</Badge>
  }
}

function daysUntil(dateStr: string): number {
  const end = new Date(dateStr)
  const now = new Date()
  return Math.floor((end.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
}

function ValidityTimeline({ startDate, endDate }: { startDate: string; endDate: string }) {
  const start = new Date(startDate).getTime()
  const end = new Date(endDate).getTime()
  const now = Date.now()
  const total = end - start
  const elapsed = Math.max(0, Math.min(now - start, total))
  const pct = total > 0 ? Math.round((elapsed / total) * 100) : 0
  const days = daysUntil(endDate)
  const isExpiring = days >= 0 && days <= 30
  const isExpired = days < 0

  return (
    <div className="space-y-2">
      <div className="flex justify-between text-xs text-text-tertiary">
        <span>{startDate}</span>
        <span>{endDate}</span>
      </div>
      <div className="h-2.5 rounded-full bg-bg-elevated overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${isExpired ? 'bg-danger' : isExpiring ? 'bg-warning' : 'bg-accent'}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      {days >= 0 && (
        <div className="flex items-center gap-1 text-xs">
          {days <= 7 ? (
            <AlertTriangle className="h-3 w-3 text-danger" />
          ) : days <= 30 ? (
            <Calendar className="h-3 w-3 text-warning" />
          ) : null}
          <span className={days <= 7 ? 'text-danger' : days <= 30 ? 'text-warning' : 'text-text-secondary'}>
            {days === 0 ? 'Expires today' : `Expiring in ${days} days`}
          </span>
        </div>
      )}
      {days < 0 && (
        <span className="text-xs text-danger">Expired {Math.abs(days)} days ago</span>
      )}
    </div>
  )
}

interface EditFormData {
  partner_operator_name: string
  agreement_type: AgreementType
  start_date: string
  end_date: string
  auto_renew: boolean
  state: AgreementState
  cost_per_mb: string
  currency: string
  settlement_period: string
  uptime_pct: string
  latency_p95_ms: string
  max_incidents: string
  notes: string
}

function agreementToForm(ag: RoamingAgreement): EditFormData {
  const ct = ag.cost_terms as CostTerms
  const sla = ag.sla_terms as SLATerms
  return {
    partner_operator_name: ag.partner_operator_name,
    agreement_type: ag.agreement_type,
    start_date: ag.start_date,
    end_date: ag.end_date,
    auto_renew: ag.auto_renew,
    state: ag.state,
    cost_per_mb: String(ct?.cost_per_mb ?? 0),
    currency: ct?.currency ?? 'USD',
    settlement_period: ct?.settlement_period ?? 'monthly',
    uptime_pct: String(sla?.uptime_pct ?? 99.9),
    latency_p95_ms: String(sla?.latency_p95_ms ?? 100),
    max_incidents: String(sla?.max_incidents ?? 5),
    notes: ag.notes ?? '',
  }
}

const AGREEMENT_TYPE_OPTIONS = [
  { value: 'national', label: 'National' },
  { value: 'international', label: 'International' },
  { value: 'MVNO', label: 'MVNO' },
]

const STATE_OPTIONS = [
  { value: 'draft', label: 'Draft' },
  { value: 'active', label: 'Active' },
  { value: 'expired', label: 'Expired' },
  { value: 'terminated', label: 'Terminated' },
]

const SETTLEMENT_OPTIONS = [
  { value: 'monthly', label: 'Monthly' },
  { value: 'quarterly', label: 'Quarterly' },
  { value: 'annual', label: 'Annual' },
]

export default function RoamingAgreementDetailPage() {
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const [editOpen, setEditOpen] = useState(false)
  const [terminateOpen, setTerminateOpen] = useState(false)
  const [form, setForm] = useState<EditFormData | null>(null)
  const [formError, setFormError] = useState<string | null>(null)

  const { data: agreement, isLoading, isError, refetch } = useRoamingAgreement(id ?? '')
  const updateMutation = useUpdateRoamingAgreement(id ?? '')
  const terminateMutation = useTerminateRoamingAgreement(id ?? '')

  function handleEditOpen() {
    if (agreement) {
      setForm(agreementToForm(agreement))
    }
    setEditOpen(true)
  }

  function handleFormChange(key: keyof EditFormData, value: string | boolean) {
    setForm((prev) => prev ? { ...prev, [key]: value } : prev)
  }

  async function handleUpdate() {
    if (!form) return
    setFormError(null)
    const costTerms: CostTerms = {
      cost_per_mb: parseFloat(form.cost_per_mb) || 0,
      currency: form.currency,
      settlement_period: form.settlement_period as CostTerms['settlement_period'],
    }
    const slaTerms: SLATerms = {
      uptime_pct: parseFloat(form.uptime_pct) || 99.9,
      latency_p95_ms: parseInt(form.latency_p95_ms) || 100,
      max_incidents: parseInt(form.max_incidents) || 5,
    }
    try {
      await updateMutation.mutateAsync({
        partner_operator_name: form.partner_operator_name,
        agreement_type: form.agreement_type,
        sla_terms: slaTerms,
        cost_terms: costTerms,
        start_date: form.start_date,
        end_date: form.end_date,
        auto_renew: form.auto_renew,
        state: form.state,
        notes: form.notes || undefined,
      })
      setEditOpen(false)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to update agreement'
      setFormError(msg)
    }
  }

  async function handleTerminate() {
    try {
      await terminateMutation.mutateAsync()
      setTerminateOpen(false)
      navigate('/roaming-agreements')
    } catch {
      setTerminateOpen(false)
    }
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  if (isError || !agreement) {
    return (
      <div className="p-6">
        <Card className="p-6 text-center space-y-3">
          <AlertCircle className="h-8 w-8 text-danger mx-auto" />
          <p className="text-sm text-text-secondary">Failed to load agreement.</p>
          <Button variant="outline" size="sm" onClick={() => refetch()}>Retry</Button>
        </Card>
      </div>
    )
  }

  const ct = agreement.cost_terms as CostTerms
  const sla = agreement.sla_terms as SLATerms

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="sm" onClick={() => navigate('/roaming-agreements')}>
            <ArrowLeft className="h-4 w-4 mr-1" />
            Back
          </Button>
          <h1 className="text-[22px] font-semibold text-text-primary">{agreement.partner_operator_name}</h1>
          {agreementStateBadge(agreement.state)}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          {agreement.state !== 'terminated' && (
            <>
              <Button variant="outline" size="sm" onClick={handleEditOpen}>
                Edit
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-danger border-danger hover:bg-danger-dim"
                onClick={() => setTerminateOpen(true)}
              >
                Terminate
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card className="p-4 space-y-3">
          <h2 className="text-[15px] font-semibold text-text-primary">Overview</h2>
          <InfoRow label="Type" value={agreement.agreement_type} />
          <InfoRow label="Operator ID" value={<span className="font-mono text-xs text-text-secondary">{agreement.operator_id}</span>} />
          <InfoRow label="Auto-renew" value={agreement.auto_renew ? 'Yes' : 'No'} />
          <InfoRow label="Created At" value={new Date(agreement.created_at).toLocaleDateString()} />
        </Card>

        <Card className="p-4 space-y-3">
          <h2 className="text-[15px] font-semibold text-text-primary">Validity</h2>
          <ValidityTimeline startDate={agreement.start_date} endDate={agreement.end_date} />
        </Card>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card className="p-4 space-y-3">
          <h2 className="text-[15px] font-semibold text-text-primary">SLA Terms</h2>
          <InfoRow label="Uptime" value={`${sla?.uptime_pct ?? '-'}%`} />
          <InfoRow label="Latency p95" value={`${sla?.latency_p95_ms ?? '-'} ms`} />
          <InfoRow label="Max incidents" value={String(sla?.max_incidents ?? '-')} />
        </Card>

        <Card className="p-4 space-y-3">
          <h2 className="text-[15px] font-semibold text-text-primary">Cost Terms</h2>
          <InfoRow label="Base rate" value={`${ct?.cost_per_mb ?? '-'} ${ct?.currency ?? ''}/MB`} />
          <InfoRow label="Settlement" value={ct?.settlement_period ?? '-'} />
          {ct?.volume_tiers && ct.volume_tiers.length > 0 && (
            <div className="space-y-1">
              <span className="text-xs text-text-secondary">Volume tiers</span>
              {ct.volume_tiers.map((tier, i) => (
                <div key={i} className="flex justify-between text-xs text-text-secondary">
                  <span>&gt;{(tier.threshold_mb / 1024).toFixed(0)} GB</span>
                  <span>{tier.cost_per_mb} {ct.currency}/MB</span>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      {agreement.notes && (
        <Card className="p-4">
          <h2 className="text-[15px] font-semibold text-text-primary mb-2">Notes</h2>
          <p className="text-sm text-text-secondary whitespace-pre-wrap">{agreement.notes}</p>
        </Card>
      )}

      <SlidePanel
        open={editOpen}
        onOpenChange={(open) => { if (!open) { setEditOpen(false); setFormError(null) } }}
        title="Edit Roaming Agreement"
      >
        {form && (
          <div className="space-y-4">
            {formError && (
              <div className="rounded-md bg-danger-dim text-danger p-3 text-sm flex items-center gap-2">
                <AlertCircle className="h-4 w-4 flex-shrink-0" />
                {formError}
              </div>
            )}

            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Partner Name</label>
              <Input
                value={form.partner_operator_name}
                onChange={(e) => handleFormChange('partner_operator_name', e.target.value)}
              />
            </div>

            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Agreement Type</label>
              <Select
                options={AGREEMENT_TYPE_OPTIONS}
                value={form.agreement_type}
                onChange={(e) => handleFormChange('agreement_type', e.target.value)}
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Start Date</label>
                <Input type="date" value={form.start_date} onChange={(e) => handleFormChange('start_date', e.target.value)} />
              </div>
              <div>
                <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">End Date</label>
                <Input type="date" value={form.end_date} onChange={(e) => handleFormChange('end_date', e.target.value)} />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Cost/MB</label>
                <Input type="number" min="0" step="0.001" value={form.cost_per_mb} onChange={(e) => handleFormChange('cost_per_mb', e.target.value)} />
              </div>
              <div>
                <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Currency</label>
                <Input placeholder="USD" maxLength={3} value={form.currency} onChange={(e) => handleFormChange('currency', e.target.value.toUpperCase())} />
              </div>
            </div>

            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Settlement</label>
              <Select
                options={SETTLEMENT_OPTIONS}
                value={form.settlement_period}
                onChange={(e) => handleFormChange('settlement_period', e.target.value)}
              />
            </div>

            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">State</label>
              <Select
                options={STATE_OPTIONS}
                value={form.state}
                onChange={(e) => handleFormChange('state', e.target.value)}
              />
            </div>

            <div className="flex items-center gap-2">
              <Checkbox
                id="edit_auto_renew"
                checked={form.auto_renew}
                onChange={(e) => handleFormChange('auto_renew', e.target.checked)}
                className="h-4 w-4"
              />
              <label htmlFor="edit_auto_renew" className="text-sm text-text-secondary cursor-pointer">Auto-renew</label>
            </div>

            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Notes</label>
              <Textarea
                value={form.notes}
                onChange={(e) => handleFormChange('notes', e.target.value)}
                rows={3}
              />
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="outline" onClick={() => setEditOpen(false)}>Cancel</Button>
              <Button onClick={handleUpdate} disabled={updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving...' : 'Save'}
              </Button>
            </div>
          </div>
        )}
      </SlidePanel>

      <Dialog open={terminateOpen} onOpenChange={setTerminateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Terminate Agreement</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-text-secondary py-2">
            Are you sure you want to terminate the agreement with <strong>{agreement.partner_operator_name}</strong>? This action cannot be undone.
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setTerminateOpen(false)}>Cancel</Button>
            <Button
              variant="destructive"
              onClick={handleTerminate}
              disabled={terminateMutation.isPending}
            >
              {terminateMutation.isPending ? 'Terminating...' : 'Terminate'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
