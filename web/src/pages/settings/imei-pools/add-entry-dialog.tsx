import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Spinner } from '@/components/ui/spinner'
import { useIMEIPoolAdd } from '@/hooks/use-imei-pools'
import {
  type AddEntryFormState,
  type IMEIEntryKind,
  type IMEIImportedFrom,
  type IMEIPool,
  IMEI_IMPORTED_FROM,
  IMPORTED_FROM_LABEL,
  INITIAL_ADD_ENTRY_FORM,
  POOL_LABEL,
  validateAddEntry,
} from '@/types/imei-pool'

interface AddEntryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  pool: IMEIPool
  initialIMEI?: string
}

interface ApiErrorShape {
  response?: {
    status?: number
    data?: { error?: { code?: string; message?: string } }
  }
}

export function AddEntryDialog({ open, onOpenChange, pool, initialIMEI }: AddEntryDialogProps) {
  const [form, setForm] = useState<AddEntryFormState>(() => ({
    ...INITIAL_ADD_ENTRY_FORM,
    imei_or_tac: initialIMEI ?? '',
    kind: initialIMEI && initialIMEI.length === 8 ? 'tac_range' : 'full_imei',
  }))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const add = useIMEIPoolAdd(pool)

  useEffect(() => {
    if (open) {
      setForm({
        ...INITIAL_ADD_ENTRY_FORM,
        imei_or_tac: initialIMEI ?? '',
        kind: initialIMEI && initialIMEI.length === 8 ? 'tac_range' : 'full_imei',
      })
      setErrors({})
    }
  }, [open, initialIMEI])

  const setField = <K extends keyof AddEntryFormState>(key: K, value: AddEntryFormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    setErrors((e) => {
      if (!e[key as string]) return e
      const next = { ...e }
      delete next[key as string]
      return next
    })
  }

  const handleSubmit = async () => {
    const validation = validateAddEntry(pool, form)
    if (Object.keys(validation).length > 0) {
      setErrors(validation)
      return
    }
    try {
      await add.mutateAsync({
        kind: form.kind,
        imei_or_tac: form.imei_or_tac.trim(),
        device_model: form.device_model.trim() || null,
        description: form.description.trim() || null,
        quarantine_reason: pool === 'greylist' ? form.quarantine_reason.trim() : null,
        block_reason: pool === 'blacklist' ? form.block_reason.trim() : null,
        imported_from: pool === 'blacklist' ? form.imported_from : null,
      })
      toast.success(`Added to ${POOL_LABEL[pool]}`)
      onOpenChange(false)
    } catch (err) {
      const e = err as ApiErrorShape
      const code = e?.response?.data?.error?.code
      const msg = e?.response?.data?.error?.message
      if (code === 'IMEI_POOL_DUPLICATE') {
        setErrors((s) => ({ ...s, imei_or_tac: 'This IMEI/TAC is already in this pool' }))
      } else if (code === 'INVALID_IMEI' || code === 'INVALID_TAC') {
        setErrors((s) => ({ ...s, imei_or_tac: msg ?? 'Invalid IMEI/TAC format' }))
      } else if (code === 'MISSING_QUARANTINE_REASON') {
        setErrors((s) => ({ ...s, quarantine_reason: msg ?? 'Quarantine reason is required' }))
      } else if (code === 'MISSING_BLOCK_REASON') {
        setErrors((s) => ({ ...s, block_reason: msg ?? 'Block reason is required' }))
      } else if (code === 'INVALID_IMPORTED_FROM') {
        setErrors((s) => ({ ...s, imported_from: msg ?? 'Invalid source' }))
      } else if (msg) {
        toast.error(msg)
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent onClose={() => onOpenChange(false)}>
        <DialogHeader>
          <DialogTitle>Add IMEI Entry — {POOL_LABEL[pool]}</DialogTitle>
          <DialogDescription>
            Register a new device or TAC range in the {POOL_LABEL[pool].toLowerCase()}.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <fieldset className="space-y-2">
            <legend className="text-xs font-medium text-text-secondary">Type</legend>
            <div className="grid grid-cols-2 gap-2">
              {(['full_imei', 'tac_range'] as IMEIEntryKind[]).map((k) => {
                const active = form.kind === k
                return (
                  <Button
                    key={k}
                    type="button"
                    variant="ghost"
                    onClick={() => setField('kind', k)}
                    aria-pressed={active}
                    className={
                      'flex h-auto flex-col items-start rounded-[var(--radius-sm)] border px-3 py-2 text-left transition-colors justify-start gap-0 ' +
                      (active
                        ? 'border-accent bg-accent-dim text-text-primary'
                        : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary')
                    }
                  >
                    <span className="text-sm font-medium">
                      {k === 'full_imei' ? 'Full IMEI' : 'TAC range'}
                    </span>
                    <span className="text-[11px] text-text-tertiary mt-0.5 font-mono">
                      {k === 'full_imei' ? '15 digits' : '8-digit prefix'}
                    </span>
                  </Button>
                )
              })}
            </div>
          </fieldset>

          <div>
            <label htmlFor="imei-or-tac" className="text-xs font-medium text-text-secondary block mb-1.5">
              {form.kind === 'full_imei' ? 'IMEI' : 'TAC'} *
            </label>
            <Input
              id="imei-or-tac"
              value={form.imei_or_tac}
              onChange={(e) => setField('imei_or_tac', e.target.value.replace(/\D/g, ''))}
              placeholder={form.kind === 'full_imei' ? '15-digit IMEI' : '8-digit TAC'}
              inputMode="numeric"
              maxLength={form.kind === 'full_imei' ? 15 : 8}
              className="font-mono"
              aria-invalid={!!errors.imei_or_tac}
              aria-describedby={errors.imei_or_tac ? 'imei-or-tac-error' : undefined}
            />
            {errors.imei_or_tac && (
              <p id="imei-or-tac-error" className="mt-1 text-xs text-danger">
                {errors.imei_or_tac}
              </p>
            )}
          </div>

          <div>
            <label htmlFor="device-model" className="text-xs font-medium text-text-secondary block mb-1.5">
              Device Model
            </label>
            <Input
              id="device-model"
              value={form.device_model}
              onChange={(e) => setField('device_model', e.target.value)}
              placeholder="e.g. Quectel BG95"
              maxLength={64}
              aria-invalid={!!errors.device_model}
            />
            {errors.device_model && (
              <p className="mt-1 text-xs text-danger">{errors.device_model}</p>
            )}
          </div>

          <div>
            <label htmlFor="description" className="text-xs font-medium text-text-secondary block mb-1.5">
              Description
            </label>
            <Textarea
              id="description"
              value={form.description}
              onChange={(e) => setField('description', e.target.value)}
              placeholder="Optional context — fleet, sourcing notes, etc."
              rows={2}
              aria-invalid={!!errors.description}
            />
            {errors.description && (
              <p className="mt-1 text-xs text-danger">{errors.description}</p>
            )}
          </div>

          {pool === 'greylist' && (
            <div>
              <label htmlFor="quarantine-reason" className="text-xs font-medium text-text-secondary block mb-1.5">
                Quarantine Reason *
              </label>
              <Textarea
                id="quarantine-reason"
                value={form.quarantine_reason}
                onChange={(e) => setField('quarantine_reason', e.target.value)}
                placeholder="Why is this entry being quarantined?"
                rows={2}
                aria-invalid={!!errors.quarantine_reason}
              />
              {errors.quarantine_reason && (
                <p className="mt-1 text-xs text-danger">{errors.quarantine_reason}</p>
              )}
            </div>
          )}

          {pool === 'blacklist' && (
            <>
              <div>
                <label htmlFor="block-reason" className="text-xs font-medium text-text-secondary block mb-1.5">
                  Block Reason *
                </label>
                <Textarea
                  id="block-reason"
                  value={form.block_reason}
                  onChange={(e) => setField('block_reason', e.target.value)}
                  placeholder="e.g. Reported stolen 02/16, CEIR ban Q1 2026"
                  rows={2}
                  aria-invalid={!!errors.block_reason}
                />
                {errors.block_reason && (
                  <p className="mt-1 text-xs text-danger">{errors.block_reason}</p>
                )}
              </div>
              <div>
                <label htmlFor="imported-from" className="text-xs font-medium text-text-secondary block mb-1.5">
                  Source *
                </label>
                <Select
                  id="imported-from"
                  value={form.imported_from}
                  onChange={(e) => setField('imported_from', e.target.value as IMEIImportedFrom)}
                  options={IMEI_IMPORTED_FROM.map((v) => ({ value: v, label: IMPORTED_FROM_LABEL[v] }))}
                  aria-invalid={!!errors.imported_from}
                />
                {errors.imported_from && (
                  <p className="mt-1 text-xs text-danger">{errors.imported_from}</p>
                )}
              </div>
            </>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 pt-4 mt-6 border-t border-border">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={add.isPending}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={add.isPending} className="gap-1.5">
            {add.isPending && <Spinner className="h-3.5 w-3.5" />}
            {add.isPending ? 'Adding…' : 'Add Entry'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
