import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useCreateProfile, useEsimStockSummary } from '@/hooks/use-esim'
import type { ESimCreateRequest } from '@/types/esim'

interface AllocateFromStockPanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  simId: string
}

interface AllocateForm {
  operator_id: string
  eid: string
  profile_id: string
  iccid_on_profile: string
}

const EMPTY_FORM: AllocateForm = {
  operator_id: '',
  eid: '',
  profile_id: '',
  iccid_on_profile: '',
}

export function AllocateFromStockPanel({ open, onOpenChange, simId }: AllocateFromStockPanelProps) {
  const [form, setForm] = useState<AllocateForm>(EMPTY_FORM)

  const { data: stockEntries = [] } = useEsimStockSummary()
  const createMutation = useCreateProfile()

  const handleClose = () => {
    setForm(EMPTY_FORM)
    onOpenChange(false)
  }

  const handleSubmit = async () => {
    const body: ESimCreateRequest = {
      sim_id: simId,
      eid: form.eid,
      operator_id: form.operator_id,
      iccid_on_profile: form.iccid_on_profile,
      profile_id: form.profile_id,
    }
    try {
      await createMutation.mutateAsync(body)
      toast.success('eSIM profile allocated successfully')
      handleClose()
    } catch {
      toast.error('Failed to allocate eSIM profile. Please try again.')
    }
  }

  const operatorOptions = stockEntries.map((e) => ({
    value: e.operator_id,
    label: `${e.operator_name} (${e.available} available)`,
    disabled: e.available === 0,
  }))

  const isValid = form.operator_id !== '' && form.eid !== ''

  return (
    <SlidePanel
      open={open}
      onOpenChange={handleClose}
      title="Allocate from Stock"
      description="Assign an eSIM profile from operator stock to this SIM card."
      width="md"
    >
      <div className="space-y-5">
        <div>
          <label className="text-xs font-medium text-text-secondary block mb-1.5">
            Operator <span className="text-danger">*</span>
          </label>
          <Select
            value={form.operator_id}
            onChange={(e) => setForm((f) => ({ ...f, operator_id: e.target.value }))}
            placeholder="Select operator..."
            options={operatorOptions}
          />
          {form.operator_id && stockEntries.find((e) => e.operator_id === form.operator_id)?.available === 0 && (
            <p className="text-xs text-warning mt-1">No profiles available for this operator.</p>
          )}
        </div>

        <div>
          <label className="text-xs font-medium text-text-secondary block mb-1.5">
            EID <span className="text-danger">*</span>
          </label>
          <Input
            value={form.eid}
            onChange={(e) => setForm((f) => ({ ...f, eid: e.target.value }))}
            placeholder="32-character EID..."
            className="font-mono text-sm"
          />
          <p className="text-xs text-text-tertiary mt-1">
            The eUICC Identifier of the physical eSIM chip.
          </p>
        </div>

        <div>
          <label className="text-xs font-medium text-text-secondary block mb-1.5">
            ICCID on Profile
            <span className="ml-1 text-xs text-text-tertiary">(optional)</span>
          </label>
          <Input
            value={form.iccid_on_profile}
            onChange={(e) => setForm((f) => ({ ...f, iccid_on_profile: e.target.value }))}
            placeholder="Up to 22 digits..."
            className="font-mono text-sm"
          />
        </div>

        <div>
          <label className="text-xs font-medium text-text-secondary block mb-1.5">
            Profile ID
            <span className="ml-1 text-xs text-text-tertiary">(optional — auto-allocated if blank)</span>
          </label>
          <Input
            value={form.profile_id}
            onChange={(e) => setForm((f) => ({ ...f, profile_id: e.target.value }))}
            placeholder="SM-DP+ profile identifier..."
            className="font-mono text-sm"
          />
        </div>
      </div>

      <SlidePanelFooter>
        <Button variant="outline" onClick={handleClose} disabled={createMutation.isPending}>
          Cancel
        </Button>
        <Button
          onClick={handleSubmit}
          disabled={!isValid || createMutation.isPending}
          className="gap-2"
        >
          {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
          Allocate Profile
        </Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}
