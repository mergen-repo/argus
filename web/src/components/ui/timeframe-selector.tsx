import { cn } from '@/lib/utils'

export type TimeframeOption = {
  value: string
  label: string
}

const DEFAULT_OPTIONS: TimeframeOption[] = [
  { value: '15m', label: '15m' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

interface TimeframeSelectorProps {
  value: string
  onChange: (value: string) => void
  options?: TimeframeOption[]
  className?: string
}

export function TimeframeSelector({
  value,
  onChange,
  options = DEFAULT_OPTIONS,
  className,
}: TimeframeSelectorProps) {
  return (
    <div className={cn('inline-flex rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-0.5', className)}>
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onChange(opt.value)}
          className={cn(
            'px-2.5 py-1 text-xs font-medium rounded-[3px] transition-colors',
            value === opt.value
              ? 'bg-accent text-bg-primary shadow-sm'
              : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover',
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}
