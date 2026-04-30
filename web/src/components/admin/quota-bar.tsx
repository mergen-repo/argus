import { cn } from '@/lib/utils'
import { formatBytes } from '@/lib/format'

export interface QuotaBarProps {
  label: string
  current: number
  max: number
  unit?: 'count' | 'bytes' | 'rps'
  className?: string
}

function formatValue(value: number, unit: 'count' | 'bytes' | 'rps'): string {
  if (unit === 'bytes') return formatBytes(value)
  if (unit === 'rps') return `${value.toLocaleString()}/s`
  return value.toLocaleString()
}

// 4-tier color logic per FIX-246 spec AC-2:
//   <50  green   (success)
//   50–80 yellow (warning)
//   >80  red     (danger)
//   >=95 critical (danger + pulse)
// Thresholds use >= to match the BE handler usageQuotaStatus and the
// quota_breach_checker (Gate F-A6).
function getBarColor(pct: number): string {
  if (pct >= 80) return 'bg-danger'
  if (pct >= 50) return 'bg-warning'
  return 'bg-success'
}

function getLabelColor(pct: number): string {
  if (pct >= 80) return 'text-danger'
  if (pct >= 50) return 'text-warning'
  return 'text-success'
}

export function QuotaBar({ label, current, max, unit = 'count', className }: QuotaBarProps) {
  const pct = max > 0 ? Math.min((current / max) * 100, 100) : 0
  const isCritical = pct >= 95

  const formattedCurrent = formatValue(current, unit)
  const formattedMax = formatValue(max, unit)

  return (
    <div className={cn('space-y-1', className)}>
      <div className="flex items-center justify-between text-xs">
        <span className="text-text-secondary">{label}</span>
        <span className={cn('font-medium', getLabelColor(pct))}>
          {pct.toFixed(0)}%
        </span>
      </div>
      <div className="relative h-2 rounded-full bg-bg-muted overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-300', getBarColor(pct))}
          style={{ width: `${pct}%` }}
        />
        {isCritical && (
          <div
            className="absolute inset-0 rounded-full motion-safe:animate-pulse motion-reduce:animate-none bg-danger opacity-30"
          />
        )}
      </div>
      <div className="text-xs text-text-tertiary">
        {formattedCurrent} / {formattedMax}
      </div>
    </div>
  )
}
