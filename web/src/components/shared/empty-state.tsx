import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  icon?: React.ElementType
  title: string
  description?: string
  ctaLabel?: string | null
  ctaHref?: string
  onCta?: () => void
  className?: string
}

export const EmptyState = React.memo(function EmptyState({
  icon: Icon,
  title,
  description,
  ctaLabel,
  ctaHref,
  onCta,
  className,
}: EmptyStateProps) {
  const navigate = useNavigate()

  return (
    <div className={cn('flex flex-col items-center justify-center py-16 px-4 text-center', className)}>
      {Icon && (
        <span className="mb-4 text-text-tertiary">
          <Icon className="h-10 w-10" />
        </span>
      )}
      <p className="text-sm font-semibold text-text-primary">{title}</p>
      {description && (
        <p className="mt-1 text-xs text-text-secondary max-w-xs">{description}</p>
      )}
      {ctaLabel && (ctaHref ? (
        <Button size="sm" className="mt-4" onClick={() => navigate(ctaHref)}>{ctaLabel}</Button>
      ) : onCta ? (
        <Button size="sm" className="mt-4" onClick={onCta}>{ctaLabel}</Button>
      ) : null)}
    </div>
  )
})
