// FIX-244 DEV-525: violation lifecycle chip.
//
// Maps the four-plus-one ViolationStatus values to the project's semantic
// token classes. Mirrors the visual contract documented in FIX-244 plan
// "Design Token Map > Color tokens" — never use Tailwind numbered palette
// (PAT-018 hot zone).

import { cn } from '@/lib/utils'
import type { ViolationStatus } from '@/types/violation'

interface StatusBadgeProps {
  status: ViolationStatus
  className?: string
}

const STATUS_TOKENS: Record<ViolationStatus, { bg: string; text: string; border: string; label: string }> = {
  open: {
    bg: 'bg-danger-dim',
    text: 'text-danger',
    border: 'border-danger/30',
    label: 'Open',
  },
  acknowledged: {
    bg: 'bg-warning-dim',
    text: 'text-warning',
    border: 'border-warning/30',
    label: 'Acknowledged',
  },
  remediated: {
    bg: 'bg-success-dim',
    text: 'text-success',
    border: 'border-success/30',
    label: 'Remediated',
  },
  dismissed: {
    bg: 'bg-bg-hover',
    text: 'text-text-tertiary',
    border: 'border-border-subtle',
    label: 'Dismissed',
  },
  escalated: {
    bg: 'bg-warning-dim',
    text: 'text-warning',
    border: 'border-warning/30',
    label: 'Escalated',
  },
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const tok = STATUS_TOKENS[status]
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-[var(--radius-sm)] border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide',
        tok.bg,
        tok.text,
        tok.border,
        className,
      )}
    >
      {tok.label}
    </span>
  )
}
