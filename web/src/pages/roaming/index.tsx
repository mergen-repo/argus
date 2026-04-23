import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Handshake,
  AlertTriangle,
  Calendar,
  RefreshCw,
  Plus,
  AlertCircle,
  Download,
  Loader2,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import { EmptyState } from '@/components/shared/empty-state'
import { EntityLink } from '@/components/shared'
import { useExport } from '@/hooks/use-export'
import { useRoamingAgreements, useCreateRoamingAgreement } from '@/hooks/use-roaming-agreements'
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

function typeBadge(type: AgreementType) {
  switch (type) {
    case 'international': return <Badge variant="default">international</Badge>
    case 'national': return <Badge variant="secondary">national</Badge>
    case 'MVNO': return <Badge className="bg-purple-dim text-purple border-transparent">MVNO</Badge>
    default: return <Badge variant="outline">{type}</Badge>
  }
}

function daysUntil(dateStr: string): number {
  const end = new Date(dateStr)
  const now = new Date()
  return Math.floor((end.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
}

function ExpiryIndicator({ endDate, state }: { endDate: string; state: AgreementState }) {
  if (state !== 'active') return null
  const days = daysUntil(endDate)
  if (days > 30) return null
  if (days <= 7) {
    return (
      <span className="flex items-center gap-1 text-xs text-danger">
        <AlertTriangle className="h-3 w-3" />
        {days}d
      </span>
    )
  }
  return (
    <span className="flex items-center gap-1 text-xs text-warning">
      <Calendar className="h-3 w-3" />
      {days}d
    </span>
  )
}

interface CreateFormData {
  operator_id: string
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

const defaultForm: CreateFormData = {
  operator_id: '',
  partner_operator_name: '',
  agreement_type: 'international',
  start_date: '',
  end_date: '',
  auto_renew: false,
  state: 'draft',
  cost_per_mb: '0',
  currency: 'USD',
  settlement_period: 'monthly',
  uptime_pct: '99.9',
  latency_p95_ms: '100',
  max_incidents: '5',
  notes: '',
}

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'draft', label: 'Draft' },
  { value: 'active', label: 'Active' },
  { value: 'expired', label: 'Expired' },
  { value: 'terminated', label: 'Terminated' },
]

const EXPIRY_OPTIONS = [
  { value: '', label: 'All Expiry' },
  { value: '7', label: 'Expiring in 7d' },
  { value: '30', label: 'Expiring in 30d' },
  { value: '90', label: 'Expiring in 90d' },
]

const AGREEMENT_TYPE_OPTIONS = [
  { value: 'national', label: 'National' },
  { value: 'international', label: 'International' },
  { value: 'MVNO', label: 'MVNO' },
]

const STATE_CREATE_OPTIONS = [
  { value: 'draft', label: 'Draft' },
  { value: 'active', label: 'Active' },
]

const SETTLEMENT_OPTIONS = [
  { value: 'monthly', label: 'Monthly' },
  { value: 'quarterly', label: 'Quarterly' },
  { value: 'annual', label: 'Annual' },
]

export default function RoamingAgreementsPage() {
  const navigate = useNavigate()
  const [cursor, setCursor] = useState<string | undefined>()
  const [stateFilter, setStateFilter] = useState('')
  const [expiringFilter, setExpiringFilter] = useState('')
  const [search, setSearch] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<CreateFormData>(defaultForm)
  const [formError, setFormError] = useState<string | null>(null)

  const expiringDays = expiringFilter ? parseInt(expiringFilter) : undefined

  const { exportCSV, exporting } = useExport('roaming-agreements')
  const { data, isLoading, isError, refetch } = useRoamingAgreements({
    limit: 50,
    cursor,
    state: stateFilter || undefined,
    expiring_within_days: expiringDays,
  })

  const createMutation = useCreateRoamingAgreement()

  const agreements = data?.data ?? []
  const filteredAgreements = search
    ? agreements.filter(
        (a) =>
          a.partner_operator_name.toLowerCase().includes(search.toLowerCase()) ||
          a.agreement_type.toLowerCase().includes(search.toLowerCase()),
      )
    : agreements

  function handleFormChange(key: keyof CreateFormData, value: string | boolean) {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  async function handleCreate() {
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
      await createMutation.mutateAsync({
        operator_id: form.operator_id,
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
      setCreateOpen(false)
      setForm(defaultForm)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to create agreement'
      setFormError(msg)
    }
  }

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <Handshake className="h-5 w-5 text-accent" />
          <h1 className="text-[22px] font-semibold text-text-primary">Roaming Agreements</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" className="gap-2" onClick={() => exportCSV()} disabled={exporting}>
            {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            Export
          </Button>
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-1" />
            New Agreement
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap gap-3">
        <Select
          options={STATE_OPTIONS}
          value={stateFilter}
          onChange={(e) => setStateFilter(e.target.value)}
          className="w-36 text-sm"
        />

        <Select
          options={EXPIRY_OPTIONS}
          value={expiringFilter}
          onChange={(e) => setExpiringFilter(e.target.value)}
          className="w-44 text-sm"
        />

        <Input
          placeholder="Search..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-48 text-sm"
        />
      </div>

      {isError && (
        <Card className="p-4 flex items-center gap-2 text-danger">
          <AlertCircle className="h-4 w-4 flex-shrink-0" />
          <span className="text-sm">Failed to load roaming agreements.</span>
          <Button variant="outline" size="sm" onClick={() => refetch()} className="ml-auto">
            Retry
          </Button>
        </Card>
      )}

      {isLoading && (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full rounded-md" />
          ))}
        </div>
      )}

      {!isLoading && !isError && filteredAgreements.length === 0 && (
        <EmptyState
          icon={Handshake}
          title="No roaming agreements"
          description="Create an agreement to customize SoR cost routing."
          ctaLabel="New Agreement"
          onCta={() => setCreateOpen(true)}
        />
      )}

      {!isLoading && !isError && filteredAgreements.length > 0 && (
        <Card className="overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="bg-bg-elevated hover:bg-bg-elevated">
                <TableHead>Partner</TableHead>
                <TableHead>Operator</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>State</TableHead>
                <TableHead>End Date</TableHead>
                <TableHead>Auto-Renew</TableHead>
                <TableHead className="text-right">Expiry</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredAgreements.map((ag: RoamingAgreement, idx: number) => (
                <TableRow
                  key={ag.id}
                  className="cursor-pointer"
                  onClick={() => navigate(`/roaming-agreements/${ag.id}`)}
                  data-row-index={idx}
                  data-href={`/roaming-agreements/${ag.id}`}
                >
                  <TableCell className="font-medium">{ag.partner_operator_name}</TableCell>
                  <TableCell className="font-mono text-xs text-text-secondary"><EntityLink entityType="operator" entityId={ag.operator_id} truncate /></TableCell>
                  <TableCell>{typeBadge(ag.agreement_type)}</TableCell>
                  <TableCell>{agreementStateBadge(ag.state)}</TableCell>
                  <TableCell className="text-text-secondary">{ag.end_date}</TableCell>
                  <TableCell className="text-text-secondary">
                    {ag.auto_renew ? <span className="text-success text-xs">on</span> : <span className="text-text-tertiary text-xs">off</span>}
                  </TableCell>
                  <TableCell className="text-right">
                    <ExpiryIndicator endDate={ag.end_date} state={ag.state} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          {data?.meta?.has_more && (
            <div className="p-3 border-t border-border text-center">
              <Button variant="outline" size="sm" onClick={() => setCursor(data.meta.cursor)}>
                Load more
              </Button>
            </div>
          )}
        </Card>
      )}

      <SlidePanel
        open={createOpen}
        onOpenChange={(open) => { if (!open) { setCreateOpen(false); setForm(defaultForm); setFormError(null) } }}
        title="New Roaming Agreement"
      >
        <div className="space-y-4">
          {formError && (
            <div className="rounded-md bg-danger-dim text-danger p-3 text-sm flex items-center gap-2">
              <AlertCircle className="h-4 w-4 flex-shrink-0" />
              {formError}
            </div>
          )}

          <div>
            <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Operator ID *</label>
            <Input
              placeholder="UUID"
              value={form.operator_id}
              onChange={(e) => handleFormChange('operator_id', e.target.value)}
            />
          </div>

          <div>
            <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Partner Name *</label>
            <Input
              placeholder="e.g. Vodafone Global Roaming"
              value={form.partner_operator_name}
              onChange={(e) => handleFormChange('partner_operator_name', e.target.value)}
            />
          </div>

          <div>
            <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Agreement Type *</label>
            <Select
              options={AGREEMENT_TYPE_OPTIONS}
              value={form.agreement_type}
              onChange={(e) => handleFormChange('agreement_type', e.target.value)}
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Start Date *</label>
              <Input
                type="date"
                value={form.start_date}
                onChange={(e) => handleFormChange('start_date', e.target.value)}
              />
            </div>
            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">End Date *</label>
              <Input
                type="date"
                value={form.end_date}
                onChange={(e) => handleFormChange('end_date', e.target.value)}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Cost/MB</label>
              <Input
                type="number"
                min="0"
                step="0.001"
                value={form.cost_per_mb}
                onChange={(e) => handleFormChange('cost_per_mb', e.target.value)}
              />
            </div>
            <div>
              <label className="text-xs uppercase tracking-wider text-text-tertiary mb-1 block">Currency</label>
              <Input
                placeholder="USD"
                maxLength={3}
                value={form.currency}
                onChange={(e) => handleFormChange('currency', e.target.value.toUpperCase())}
              />
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
              options={STATE_CREATE_OPTIONS}
              value={form.state}
              onChange={(e) => handleFormChange('state', e.target.value)}
            />
          </div>

          <div className="flex items-center gap-2">
            <Checkbox
              id="auto_renew"
              checked={form.auto_renew}
              onChange={(e) => handleFormChange('auto_renew', e.target.checked)}
              className="h-4 w-4"
            />
            <label htmlFor="auto_renew" className="text-sm text-text-secondary cursor-pointer">
              Auto-renew
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => { setCreateOpen(false); setForm(defaultForm) }}>
              Cancel
            </Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </div>
        </div>
      </SlidePanel>
    </div>
  )
}
