// FIX-244 DEV-532: sticky bulk action bar for the violations page.
//
// Mirrors the FIX-201 SIM list pattern (count chip + action buttons + Clear).
// Selection scope is the visible page only — filter-based bulk select is
// deferred to FIX-236 (recorded as D-151). The header tooltip in the page
// surfaces this constraint to the user.

import { Button } from '@/components/ui/button'
import { CheckCircle2, X, XCircle } from 'lucide-react'

interface BulkActionBarProps {
  count: number
  loading?: boolean
  onAcknowledge: () => void
  onDismiss: () => void
  onClear: () => void
}

export function BulkActionBar({ count, loading, onAcknowledge, onDismiss, onClear }: BulkActionBarProps) {
  if (count === 0) return null

  return (
    <div
      role="region"
      aria-label="Bulk violation actions"
      className="fixed bottom-4 left-1/2 -translate-x-1/2 z-40 flex items-center gap-2 rounded-[var(--radius-lg)] border border-border-default bg-bg-elevated px-4 py-2 shadow-card"
    >
      <span className="text-xs text-text-secondary">
        <span className="font-mono font-semibold text-text-primary">{count}</span> selected
      </span>
      <div className="h-4 w-px bg-border-subtle" />
      <Button size="sm" variant="outline" className="gap-1.5 text-xs" onClick={onAcknowledge} disabled={loading}>
        <CheckCircle2 className="h-3.5 w-3.5 text-success" />
        Acknowledge
      </Button>
      <Button size="sm" variant="outline" className="gap-1.5 text-xs" onClick={onDismiss} disabled={loading}>
        <XCircle className="h-3.5 w-3.5 text-text-tertiary" />
        Dismiss
      </Button>
      <Button size="sm" variant="ghost" className="gap-1.5 text-xs text-text-tertiary" onClick={onClear} disabled={loading}>
        <X className="h-3.5 w-3.5" />
        Clear
      </Button>
    </div>
  )
}
