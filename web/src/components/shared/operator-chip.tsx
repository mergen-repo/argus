import { AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { operatorChipColor, type OperatorCode } from '@/lib/operator-chip'

interface OperatorChipProps {
  name?: string | null
  code?: OperatorCode
  rawId?: string | null
  clickable?: boolean
  onClick?: () => void
  className?: string
}

export function OperatorChip({ name, code, rawId, clickable, onClick, className }: OperatorChipProps) {
  const isOrphan = !name
  const colors = operatorChipColor(code)

  const base = cn(
    'inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] px-2 py-0.5',
    isOrphan ? 'bg-bg-elevated' : colors.bg,
    clickable && 'hover:ring-1 hover:ring-accent/40 focus:outline-none focus:ring-2 focus:ring-accent cursor-pointer transition-colors',
    className,
  )

  const title = isOrphan && rawId ? `Orphan operator reference: ${rawId}` : undefined

  const content = isOrphan ? (
    <>
      <AlertCircle className="h-3 w-3 text-warning" aria-hidden="true" />
      <span className="text-[13px] italic text-text-secondary">(Unknown)</span>
    </>
  ) : (
    <>
      <span className={cn('h-1.5 w-1.5 rounded-full flex-none', colors.dot)} aria-hidden="true" />
      <span className="text-[13px] font-medium text-text-primary">{name}</span>
      {code && (
        <span className="text-[11px] font-mono text-text-tertiary">({code})</span>
      )}
    </>
  )

  if (clickable && onClick) {
    return (
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={onClick}
        title={title}
        className={cn('h-auto', base)}
      >
        {content}
      </Button>
    )
  }

  return (
    <span title={title} className={base}>
      {content}
    </span>
  )
}
