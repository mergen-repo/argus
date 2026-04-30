import { useEffect, useMemo, useState } from 'react'
import { BellOff, Info } from 'lucide-react'
import axios from 'axios'
import { toast } from 'sonner'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Radio } from '@/components/ui/radio'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { Alert } from '@/types/analytics'

const MAX_LOOKAHEAD_DAYS = 30
const MAX_REASON = 500
const MAX_RULE_NAME = 100

type ScopeType = 'this' | 'type' | 'operator' | 'dedup_key'
type DurationMode = '1h' | '24h' | '7d' | 'custom'

interface SuppressionResponse {
  status: string
  data: {
    id: string
    applied_count?: number
    rule_name?: string
  }
}

export interface MutePanelProps {
  open: boolean
  onClose: () => void
  anchorAlert?: Alert
  defaultFilters?: Record<string, string>
  onSuccess?: () => void
}

interface ScopeOption {
  value: ScopeType
  label: string
  description: string
  disabled: boolean
  resolvedValue: string
}

function buildScopeOptions(anchor: Alert | undefined, defaults: Record<string, string> | undefined): ScopeOption[] {
  if (anchor) {
    const operatorLabel = anchor.operator_id || 'unknown operator'
    return [
      {
        value: 'this',
        label: 'This alert only',
        description: `Suppress alert ID ${anchor.id.slice(0, 8)}…`,
        disabled: false,
        resolvedValue: anchor.id,
      },
      {
        value: 'type',
        label: `All alerts of type "${anchor.type}"`,
        description: 'Mute every future alert with this exact type',
        disabled: !anchor.type,
        resolvedValue: anchor.type ?? '',
      },
      {
        value: 'operator',
        label: `All alerts on operator "${operatorLabel}"`,
        description: 'Mute every alert tied to this operator',
        disabled: !anchor.operator_id,
        resolvedValue: anchor.operator_id ?? '',
      },
      {
        value: 'dedup_key',
        label: anchor.dedup_key
          ? `All alerts matching dedup_key "${anchor.dedup_key.slice(0, 32)}${anchor.dedup_key.length > 32 ? '…' : ''}"`
          : 'Match by dedup_key (unavailable)',
        description: 'Suppress alerts that share this signature',
        disabled: !anchor.dedup_key,
        resolvedValue: anchor.dedup_key ?? '',
      },
    ]
  }
  const filterType = defaults?.type ?? ''
  const filterOperator = defaults?.operator_id ?? ''
  return [
    {
      value: 'type',
      label: filterType ? `All alerts of type "${filterType}"` : 'Match by alert type',
      description: 'Mute every alert with this type',
      disabled: false,
      resolvedValue: filterType,
    },
    {
      value: 'operator',
      label: filterOperator ? `All alerts on operator "${filterOperator}"` : 'Match by operator',
      description: 'Mute every alert tied to this operator',
      disabled: false,
      resolvedValue: filterOperator,
    },
    {
      value: 'dedup_key',
      label: 'Match by dedup_key',
      description: 'Suppress alerts that share a custom signature',
      disabled: false,
      resolvedValue: defaults?.dedup_key ?? '',
    },
    {
      value: 'this',
      label: 'Specific alert ID',
      description: 'Provide a single alert UUID to mute',
      disabled: false,
      resolvedValue: '',
    },
  ]
}

function formatDateTimeLocal(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  )
}

export function MutePanel({ open, onClose, anchorAlert, defaultFilters, onSuccess }: MutePanelProps) {
  const scopeOptions = useMemo(
    () => buildScopeOptions(anchorAlert, defaultFilters),
    [anchorAlert, defaultFilters],
  )

  const initialScope: ScopeType = useMemo(() => {
    const firstEnabled = scopeOptions.find((o) => !o.disabled)
    return firstEnabled?.value ?? 'type'
  }, [scopeOptions])

  const lookaheadBounds = useMemo(() => {
    const now = new Date()
    const max = new Date(now.getTime() + MAX_LOOKAHEAD_DAYS * 24 * 60 * 60 * 1000)
    return { minIso: formatDateTimeLocal(now), maxIso: formatDateTimeLocal(max), maxMs: max.getTime() }
  }, [open])

  const [scopeType, setScopeType] = useState<ScopeType>(initialScope)
  const [scopeValue, setScopeValue] = useState<string>('')
  const [durationMode, setDurationMode] = useState<DurationMode>('24h')
  const [customExpiresAt, setCustomExpiresAt] = useState<string>('')
  const [reason, setReason] = useState('')
  const [saveAsRule, setSaveAsRule] = useState(false)
  const [ruleName, setRuleName] = useState('')
  const [ruleNameError, setRuleNameError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    setScopeType(initialScope)
    const seed = scopeOptions.find((o) => o.value === initialScope)
    setScopeValue(seed?.resolvedValue ?? '')
    setDurationMode('24h')
    setCustomExpiresAt('')
    setReason('')
    setSaveAsRule(false)
    setRuleName('')
    setRuleNameError(null)
    setSubmitting(false)
  }, [open, initialScope, scopeOptions])

  useEffect(() => {
    const opt = scopeOptions.find((o) => o.value === scopeType)
    if (opt && opt.resolvedValue) setScopeValue(opt.resolvedValue)
  }, [scopeType, scopeOptions])

  const trimmedScopeValue = scopeValue.trim()
  const trimmedRuleName = ruleName.trim()
  const customDate = customExpiresAt ? new Date(customExpiresAt) : null
  const customInvalid =
    durationMode === 'custom' &&
    (!customDate || Number.isNaN(customDate.getTime()) ||
      customDate.getTime() <= Date.now() ||
      customDate.getTime() > lookaheadBounds.maxMs)

  const submitDisabled =
    submitting ||
    !trimmedScopeValue ||
    customInvalid ||
    (saveAsRule && !trimmedRuleName)

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (submitDisabled) return
    setSubmitting(true)
    setRuleNameError(null)
    try {
      const payload: Record<string, unknown> = {
        scope_type: scopeType,
        scope_value: trimmedScopeValue,
      }
      if (durationMode === 'custom' && customDate) {
        payload.expires_at = customDate.toISOString()
      } else {
        payload.duration = durationMode
      }
      if (reason.trim()) payload.reason = reason.trim().slice(0, MAX_REASON)
      if (saveAsRule && trimmedRuleName) payload.rule_name = trimmedRuleName.slice(0, MAX_RULE_NAME)

      const res = await api.post<SuppressionResponse>('/alerts/suppressions', payload)
      const applied = res.data.data?.applied_count ?? 0
      toast.success(
        applied > 0
          ? `Suppression created — ${applied} alert${applied === 1 ? '' : 's'} already muted`
          : 'Suppression created',
      )
      onSuccess?.()
      onClose()
    } catch (err: unknown) {
      if (axios.isAxiosError(err)) {
        const code = err.response?.data?.error?.code
        const message = err.response?.data?.error?.message
        if (code === 'DUPLICATE' || err.response?.status === 409) {
          setRuleNameError('A rule with this name already exists')
        } else if (err.response?.status === 422 || err.response?.status === 400) {
          toast.error(message ?? 'Invalid suppression request')
        } else {
          toast.error(message ?? 'Failed to create suppression')
        }
      } else {
        toast.error('Failed to create suppression')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <SlidePanel
      open={open}
      onOpenChange={(v) => { if (!v) onClose() }}
      title="Mute alerts"
      description={anchorAlert
        ? 'Choose a scope and duration. A suppression rule will be created and applied immediately.'
        : 'Build a suppression rule from the current filter selection.'}
      width="lg"
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-5">
        {!anchorAlert && (
          <div className="flex items-start gap-2 rounded-[var(--radius-sm)] border border-accent/20 bg-accent-dim px-3 py-2.5">
            <Info className="mt-0.5 h-3.5 w-3.5 text-accent flex-shrink-0" />
            <p className="text-[12px] text-text-secondary">
              Will create a rule covering current filter selection.
            </p>
          </div>
        )}

        <fieldset className="flex flex-col gap-2">
          <legend className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary mb-1">Scope</legend>
          {scopeOptions.map((opt) => {
            const checked = scopeType === opt.value
            return (
              <label
                key={opt.value}
                className={cn(
                  'flex cursor-pointer items-start gap-3 rounded-[var(--radius-sm)] border px-3 py-2.5 transition-colors',
                  checked
                    ? 'border-accent bg-accent-dim'
                    : 'border-border bg-bg-elevated hover:bg-bg-hover',
                  opt.disabled && 'cursor-not-allowed opacity-50 hover:bg-bg-elevated',
                )}
              >
                <Radio
                  name="mute-scope"
                  value={opt.value}
                  checked={checked}
                  disabled={opt.disabled}
                  onChange={() => setScopeType(opt.value)}
                  className="mt-1"
                />
                <div className="flex-1 min-w-0">
                  <p className="text-[13px] font-medium text-text-primary truncate">{opt.label}</p>
                  <p className="text-[11px] text-text-tertiary mt-0.5">{opt.description}</p>
                </div>
              </label>
            )
          })}
        </fieldset>

        <div className="flex flex-col gap-1">
          <label htmlFor="mute-scope-value" className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">
            Scope value
          </label>
          <Input
            id="mute-scope-value"
            value={scopeValue}
            onChange={(e) => setScopeValue(e.target.value)}
            placeholder="Required"
            readOnly={!!anchorAlert && scopeType !== 'this'}
            aria-invalid={!trimmedScopeValue}
          />
          {!trimmedScopeValue && (
            <p className="text-[11px] text-danger">Scope value is required</p>
          )}
        </div>

        <fieldset className="flex flex-col gap-2">
          <legend className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary mb-1">Duration</legend>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {(['1h', '24h', '7d', 'custom'] as DurationMode[]).map((mode) => {
              const active = durationMode === mode
              return (
                <label
                  key={mode}
                  className={cn(
                    'flex cursor-pointer items-center justify-center rounded-[var(--radius-sm)] border px-3 py-2 text-[12px] font-medium transition-colors',
                    active
                      ? 'border-accent bg-accent-dim text-accent'
                      : 'border-border bg-bg-elevated text-text-secondary hover:bg-bg-hover',
                  )}
                >
                  <Radio
                    name="mute-duration"
                    value={mode}
                    checked={active}
                    onChange={() => setDurationMode(mode)}
                    className="sr-only"
                  />
                  {mode === '1h' ? '1 hour' : mode === '24h' ? '24 hours' : mode === '7d' ? '7 days' : 'Custom'}
                </label>
              )
            })}
          </div>
          {durationMode === 'custom' && (
            <div className="mt-2 flex flex-col gap-1">
              <label htmlFor="mute-custom-expires" className="text-[11px] text-text-tertiary">
                Expires at (max {MAX_LOOKAHEAD_DAYS} days from now)
              </label>
              <Input
                id="mute-custom-expires"
                type="datetime-local"
                value={customExpiresAt}
                min={lookaheadBounds.minIso}
                max={lookaheadBounds.maxIso}
                onChange={(e) => setCustomExpiresAt(e.target.value)}
                aria-invalid={customInvalid}
              />
              {customInvalid && customExpiresAt && (
                <p className="text-[11px] text-danger">
                  Pick a future datetime within {MAX_LOOKAHEAD_DAYS} days
                </p>
              )}
            </div>
          )}
        </fieldset>

        <div className="flex flex-col gap-1">
          <label htmlFor="mute-reason" className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">
            Reason (optional)
          </label>
          <Textarea
            id="mute-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value.slice(0, MAX_REASON))}
            rows={3}
            placeholder="Why is this being muted? Helps audit trails."
          />
          <span className="self-end text-[10px] text-text-tertiary">{reason.length}/{MAX_REASON}</span>
        </div>

        <div className="flex flex-col gap-2 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-3">
          <label className="flex items-center gap-2 cursor-pointer">
            <Checkbox
              id="mute-save-as-rule"
              checked={saveAsRule}
              onChange={(e) => setSaveAsRule(e.target.checked)}
            />
            <span className="text-[13px] text-text-primary font-medium">Save as named rule</span>
          </label>
          <p className="text-[11px] text-text-tertiary">
            Persists this suppression in Settings &rarr; Alert Rules so the team can review or revoke it later.
          </p>
          {saveAsRule && (
            <div className="mt-1 flex flex-col gap-1">
              <label htmlFor="mute-rule-name" className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">
                Rule name
              </label>
              <Input
                id="mute-rule-name"
                value={ruleName}
                maxLength={MAX_RULE_NAME}
                onChange={(e) => {
                  setRuleName(e.target.value)
                  if (ruleNameError) setRuleNameError(null)
                }}
                placeholder="e.g. NOC weekend silence"
                aria-invalid={!!ruleNameError || !trimmedRuleName}
              />
              <div className="flex items-center justify-between">
                {ruleNameError ? (
                  <p className="text-[11px] text-danger">{ruleNameError}</p>
                ) : !trimmedRuleName ? (
                  <p className="text-[11px] text-danger">Rule name is required when saving</p>
                ) : <span />}
                <span className="text-[10px] text-text-tertiary">{ruleName.length}/{MAX_RULE_NAME}</span>
              </div>
            </div>
          )}
        </div>

        <SlidePanelFooter className="-mx-5 -mb-5">
          <Button type="button" variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="default"
            disabled={submitDisabled}
            className="gap-1.5"
          >
            {submitting ? <Spinner className="h-3.5 w-3.5" /> : <BellOff className="h-3.5 w-3.5" />}
            Mute
          </Button>
        </SlidePanelFooter>
      </form>
    </SlidePanel>
  )
}

export default MutePanel
