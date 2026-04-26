import type { Diagnostic } from '@codemirror/lint'
import { CheckCircle2, AlertCircle, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'

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
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onJumpTo?.(errors[0].from)}
          className="h-auto px-1 py-0 gap-1.5 text-danger hover:bg-transparent hover:underline focus-visible:ring-danger"
          aria-label={`${errors.length} error${errors.length > 1 ? 's' : ''} — click to jump`}
        >
          <AlertCircle className="h-3.5 w-3.5" />
          <span className="font-medium">
            {errors.length} error{errors.length > 1 ? 's' : ''}
          </span>
        </Button>
      )}
      {warnings.length > 0 && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onJumpTo?.(warnings[0].from)}
          className="h-auto px-1 py-0 gap-1.5 text-warning hover:bg-transparent hover:underline focus-visible:ring-warning"
          aria-label={`${warnings.length} warning${warnings.length > 1 ? 's' : ''} — click to jump`}
        >
          <AlertTriangle className="h-3.5 w-3.5" />
          <span className="font-medium">
            {warnings.length} warning{warnings.length > 1 ? 's' : ''}
          </span>
        </Button>
      )}
      {errors.length > 0 && (
        <span className="text-text-tertiary truncate max-w-md" title={errors[0].message}>
          {errors[0].message}
        </span>
      )}
    </div>
  )
}
