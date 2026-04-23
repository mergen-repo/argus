import * as React from 'react'
import { cn } from '@/lib/utils'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export type TimeframePreset = '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom'

export interface TimeframeValue {
  value: TimeframePreset
  from?: string
  to?: string
}

export type TimeframeOption = {
  value: TimeframePreset | string
  label: string
}

const CANONICAL_OPTIONS: TimeframeOption[] = [
  { value: '1h', label: '1h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

interface TimeframeSelectorProps {
  value: TimeframeValue | TimeframePreset | string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  onChange: (v: any) => void
  options?: TimeframeOption[]
  disabledPresets?: TimeframePreset[]
  allowCustom?: boolean
  className?: string
  'aria-label'?: string
}

function normalizeValue(v: TimeframeValue | TimeframePreset | string): TimeframeValue {
  if (typeof v === 'string') return { value: v as TimeframePreset }
  return v
}

function formatCustomLabel(from?: string, to?: string): string {
  if (!from || !to) return 'Custom'
  const fmt = (s: string) => {
    const d = new Date(s)
    return `${d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}`
  }
  const raw = `Custom · ${fmt(from)} → ${fmt(to)}`
  return raw.length > 22 ? raw.slice(0, 21) + '…' : raw
}

// Convert a UTC ISO string (e.g. "2026-04-22T13:00:00.000Z") into the
// `YYYY-MM-DDTHH:mm` string format expected by `<input type="datetime-local">`,
// interpreted in the user's local timezone. Returns empty string on invalid input.
function toLocalDatetimeLocal(utcISO?: string): string {
  if (!utcISO) return ''
  const d = new Date(utcISO)
  if (isNaN(d.getTime())) return ''
  const pad = (n: number) => n.toString().padStart(2, '0')
  const yyyy = d.getFullYear()
  const mm = pad(d.getMonth() + 1)
  const dd = pad(d.getDate())
  const hh = pad(d.getHours())
  const mi = pad(d.getMinutes())
  return `${yyyy}-${mm}-${dd}T${hh}:${mi}`
}

// Convert a `<input type="datetime-local">` string (local-time, no tz) into
// a UTC ISO string for persistence / URL. Returns empty string on invalid input.
function fromLocalDatetimeLocal(localStr: string): string {
  if (!localStr) return ''
  const d = new Date(localStr)
  if (isNaN(d.getTime())) return ''
  return d.toISOString()
}

interface CustomPopoverBodyProps {
  open: boolean
  onClose: () => void
  onApply: (from: string, to: string) => void
  initialFrom?: string
  initialTo?: string
}

function CustomPopoverBody({ onClose, onApply, initialFrom, initialTo }: CustomPopoverBodyProps) {
  const [from, setFrom] = React.useState(initialFrom ?? '')
  const [to, setTo] = React.useState(initialTo ?? '')
  const [error, setError] = React.useState('')

  const handleApply = () => {
    if (!from || !to) { setError('Both dates are required.'); return }
    if (new Date(to) <= new Date(from)) { setError('"To" must be after "From".'); return }
    onApply(from, to)
  }

  return (
    <div className="p-3 space-y-3 min-w-[260px]">
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-text-secondary">From</label>
        <Input
          type="datetime-local"
          value={from}
          onChange={(e) => { setFrom(e.target.value); setError('') }}
          className="text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-text-secondary">To</label>
        <Input
          type="datetime-local"
          value={to}
          onChange={(e) => { setTo(e.target.value); setError('') }}
          className="text-xs"
        />
      </div>
      {error && <p className="text-xs text-danger">{error}</p>}
      <div className="flex justify-end gap-2 pt-1">
        <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
        <Button size="sm" onClick={handleApply}>Apply</Button>
      </div>
    </div>
  )
}

export function TimeframeSelector({
  value,
  onChange,
  options,
  disabledPresets = [],
  allowCustom = true,
  className,
  'aria-label': ariaLabel = 'Timeframe',
}: TimeframeSelectorProps) {
  const normalized = normalizeValue(value)
  const [popoverOpen, setPopoverOpen] = React.useState(false)

  const isLegacy = typeof value === 'string'

  const baseOptions = options ?? CANONICAL_OPTIONS
  const allOptions: TimeframeOption[] = allowCustom
    ? [...baseOptions, { value: 'custom', label: normalized.value === 'custom' ? formatCustomLabel(normalized.from, normalized.to) : 'Custom' }]
    : baseOptions

  const selectableIndices = allOptions
    .map((o, i) => ({ o, i }))
    .filter(({ o }) => !disabledPresets.includes(o.value as TimeframePreset))
    .map(({ i }) => i)

  const currentIndex = allOptions.findIndex((o) => o.value === normalized.value)

  const emit = (next: TimeframeValue) => {
    if (isLegacy) {
      onChange(next.value)
    } else {
      onChange(next)
    }
  }

  const handleSelect = (opt: TimeframeOption) => {
    if (disabledPresets.includes(opt.value as TimeframePreset)) return
    if (opt.value === 'custom') {
      setPopoverOpen(true)
      return
    }
    emit({ value: opt.value as TimeframePreset })
  }

  const handleCustomApply = (from: string, to: string) => {
    setPopoverOpen(false)
    emit({
      value: 'custom',
      from: fromLocalDatetimeLocal(from),
      to: fromLocalDatetimeLocal(to),
    })
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const pos = selectableIndices.indexOf(currentIndex)
    let nextPos: number
    if (e.key === 'ArrowRight') nextPos = (pos + 1) % selectableIndices.length
    else if (e.key === 'ArrowLeft') nextPos = (pos - 1 + selectableIndices.length) % selectableIndices.length
    else if (e.key === 'Home') nextPos = 0
    else nextPos = selectableIndices.length - 1
    const nextIdx = selectableIndices[nextPos]
    const nextOpt = allOptions[nextIdx]
    if (nextOpt) handleSelect(nextOpt)
  }

  return (
    <div
      role="group"
      aria-label={ariaLabel}
      onKeyDown={handleKeyDown}
      className={cn('inline-flex rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-0.5', className)}
    >
      {allOptions.map((opt, idx) => {
        const isActive = normalized.value === opt.value
        const isDisabled = disabledPresets.includes(opt.value as TimeframePreset)
        const isCustomActive = opt.value === 'custom' && isActive

        const pillClassName = cn(
          'h-auto px-2.5 py-1 text-xs font-medium rounded-[3px] transition-colors focus-visible:outline-accent focus-visible:outline-2 gap-0 inline-flex items-center justify-center whitespace-nowrap',
          isActive
            ? 'bg-accent text-bg-primary shadow-sm hover:bg-accent/90 hover:text-bg-primary hover:shadow-sm'
            : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover',
          isDisabled && 'opacity-40 cursor-not-allowed',
        )

        if (opt.value === 'custom') {
          // Use PopoverTrigger directly so the Popover anchors to the real
          // trigger element (fixes outside-click + aria-expanded wiring).
          return (
            <Popover key="custom" open={popoverOpen} onOpenChange={setPopoverOpen}>
              <PopoverTrigger
                tabIndex={isActive ? 0 : currentIndex === -1 && idx === 0 ? 0 : -1}
                aria-pressed={isActive}
                aria-disabled={isDisabled ? 'true' : undefined}
                title={isDisabled ? 'Not available for your role' : undefined}
                disabled={isDisabled}
                className={pillClassName}
              >
                {isCustomActive ? formatCustomLabel(normalized.from, normalized.to) : opt.label}
              </PopoverTrigger>
              <PopoverContent align="end">
                <CustomPopoverBody
                  open={popoverOpen}
                  onClose={() => setPopoverOpen(false)}
                  onApply={handleCustomApply}
                  initialFrom={toLocalDatetimeLocal(normalized.from)}
                  initialTo={toLocalDatetimeLocal(normalized.to)}
                />
              </PopoverContent>
            </Popover>
          )
        }

        return (
          <Button
            key={opt.value}
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => handleSelect(opt)}
            tabIndex={isActive ? 0 : currentIndex === -1 && idx === 0 ? 0 : -1}
            aria-pressed={isActive}
            aria-disabled={isDisabled ? 'true' : undefined}
            title={isDisabled ? 'Not available for your role' : undefined}
            disabled={isDisabled}
            className={pillClassName}
          >
            {opt.label}
          </Button>
        )
      })}
    </div>
  )
}
