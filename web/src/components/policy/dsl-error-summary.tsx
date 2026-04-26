import type { Diagnostic } from '@codemirror/lint'
import { CheckCircle2, AlertCircle, AlertTriangle } from 'lucide-react'

interface DSLErrorSummaryProps {
  diagnostics: Diagnostic[]
  onJumpTo?: (pos: number) => void
  className?: string
}

export function DSLErrorSummary({ diagnostics, onJumpTo, className }: DSLErrorSummaryProps) {
  const errors = diagnostics.filter((d) => d.severity === 'error')
  const warnings = diagnostics.filter((d) => d.severity === 'warning')

  if (diagnostics.length === 0) {
    return (
      <div
        className={`flex items-center gap-2 px-3 py-1.5 text-xs text-text-tertiary border-t border-border bg-bg-elevated ${className ?? ''}`}
      >
        <CheckCircle2 className="h-3.5 w-3.5 text-success" />
        <span>No issues — DSL is valid</span>
      </div>
    )
  }

  return (
    <div
      className={`flex items-center gap-4 px-3 py-1.5 text-xs border-t border-border bg-bg-elevated ${className ?? ''}`}
    >
      {errors.length > 0 && (
        <button
          type="button"
          onClick={() => onJumpTo?.(errors[0].from)}
          className="flex items-center gap-1.5 text-danger hover:underline focus:outline-none focus:ring-1 focus:ring-danger rounded transition-colors"
          aria-label={`${errors.length} error${errors.length > 1 ? 's' : ''} — click to jump`}
        >
          <AlertCircle className="h-3.5 w-3.5" />
          <span className="font-medium">
            {errors.length} error{errors.length > 1 ? 's' : ''}
          </span>
        </button>
      )}
      {warnings.length > 0 && (
        <button
          type="button"
          onClick={() => onJumpTo?.(warnings[0].from)}
          className="flex items-center gap-1.5 text-warning hover:underline focus:outline-none focus:ring-1 focus:ring-warning rounded transition-colors"
          aria-label={`${warnings.length} warning${warnings.length > 1 ? 's' : ''} — click to jump`}
        >
          <AlertTriangle className="h-3.5 w-3.5" />
          <span className="font-medium">
            {warnings.length} warning{warnings.length > 1 ? 's' : ''}
          </span>
        </button>
      )}
      {errors.length > 0 && (
        <span className="text-text-tertiary truncate max-w-md" title={errors[0].message}>
          {errors[0].message}
        </span>
      )}
    </div>
  )
}
