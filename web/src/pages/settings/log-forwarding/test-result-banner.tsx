// STORY-098 Task 6 — Inline result banner for the Test Connection action.
// Renders a subtle inline banner inside the slide-panel form footer area
// so the operator can see test outcomes without losing the panel context.

import { CheckCircle2, AlertOctagon, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

export type TestResultState =
  | { state: 'idle' }
  | { state: 'pending' }
  | { state: 'ok' }
  | { state: 'error'; message: string }

interface TestResultBannerProps {
  result: TestResultState
  className?: string
}

export function TestResultBanner({ result, className }: TestResultBannerProps) {
  if (result.state === 'idle') return null

  if (result.state === 'pending') {
    return (
      <div
        className={cn(
          'flex items-center gap-2 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2 text-xs text-text-secondary',
          className,
        )}
        role="status"
        aria-live="polite"
      >
        <Loader2 className="h-3.5 w-3.5 animate-spin text-text-tertiary" />
        Testing connection…
      </div>
    )
  }

  if (result.state === 'ok') {
    return (
      <div
        className={cn(
          'flex items-center gap-2 rounded-[var(--radius-sm)] border border-success/30 bg-success-dim px-3 py-2 text-xs text-success',
          className,
        )}
        role="status"
        aria-live="polite"
      >
        <CheckCircle2 className="h-3.5 w-3.5 flex-shrink-0" />
        Connection successful — destination accepted the test packet.
      </div>
    )
  }

  return (
    <div
      className={cn(
        'flex items-start gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-xs text-danger',
        className,
      )}
      role="alert"
      aria-live="assertive"
    >
      <AlertOctagon className="h-3.5 w-3.5 flex-shrink-0 mt-0.5" />
      <div className="space-y-0.5">
        <p className="font-medium">Connection failed</p>
        <p className="font-mono text-[11px] break-all opacity-90">{result.message}</p>
      </div>
    </div>
  )
}
