// FIX-236 DEV-551: generic sticky bulk action bar shared across list pages.
//
// Two modes:
//   - 'selected': count = number of explicitly checked rows
//   - 'matching-filter': count = server-resolved total of rows matching the
//     active list filter (preview-count endpoint). The button labels surface
//     this distinction so the user is never confused about scope.
//
// The violation-specific bar (web/src/components/violations/bulk-action-bar.tsx)
// pre-dates this generic shell and is kept in place to avoid regression risk.
// Future refactor → D-162.

import { Button } from '@/components/ui/button'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { LucideIcon } from 'lucide-react'

export type BulkBarMode = 'selected' | 'matching-filter'

export interface BulkAction {
  id: string
  label: string
  icon?: LucideIcon
  /** 'destructive' renders a red button; 'default' a neutral one. */
  tone?: 'default' | 'destructive'
  onClick: () => void
  /** Disabled state (per-action; bar's `loading` disables all). */
  disabled?: boolean
}

interface BulkActionBarProps {
  mode: BulkBarMode
  /** For mode='selected', the explicit row count. For mode='matching-filter', the server-resolved total. */
  count: number
  /** When mode='matching-filter' AND server reports the total exceeded the cap, use this label suffix. */
  capped?: boolean
  loading?: boolean
  actions: BulkAction[]
  onClear?: () => void
  /** Aria label for the toolbar region. */
  label?: string
}

const TONE_CLASS: Record<NonNullable<BulkAction['tone']>, string> = {
  default: '',
  destructive: 'text-danger hover:text-danger',
}

export function BulkActionBar({ mode, count, capped, loading, actions, onClear, label }: BulkActionBarProps) {
  if (count === 0) return null

  const scopeText =
    mode === 'matching-filter'
      ? capped
        ? `${count.toLocaleString()}+ matching filter`
        : `${count.toLocaleString()} matching filter`
      : `${count.toLocaleString()} selected`

  return (
    <div
      role="region"
      aria-label={label ?? 'Bulk actions'}
      className="fixed bottom-4 left-1/2 -translate-x-1/2 z-40 flex items-center gap-2 rounded-[var(--radius-lg)] border border-border-default bg-bg-elevated px-4 py-2 shadow-card max-w-[calc(100vw-2rem)] flex-wrap print:hidden"
    >
      <span className="text-xs text-text-secondary">
        <span className="font-mono font-semibold text-text-primary">{scopeText.split(' ')[0]}</span>{' '}
        {scopeText.split(' ').slice(1).join(' ')}
      </span>
      <div className="h-4 w-px bg-border-subtle" aria-hidden="true" />
      {actions.map((a) => {
        const Icon = a.icon
        return (
          <Button
            key={a.id}
            size="sm"
            variant="outline"
            className={cn('gap-1.5 text-xs', TONE_CLASS[a.tone ?? 'default'])}
            disabled={loading || a.disabled}
            onClick={a.onClick}
          >
            {Icon && <Icon className="h-3.5 w-3.5" />}
            {a.label}
          </Button>
        )
      })}
      {onClear && (
        <Button size="sm" variant="ghost" className="gap-1.5 text-xs text-text-tertiary" disabled={loading} onClick={onClear}>
          <X className="h-3.5 w-3.5" />
          Clear
        </Button>
      )}
    </div>
  )
}
