// FIX-239 DEV-540: how-to card for the Common Operations Cookbook.

import { cn } from '@/lib/utils'
import type { LucideIcon } from 'lucide-react'

interface OperationCardProps {
  icon: LucideIcon
  title: string
  description: string
  steps: string[]
  warnings?: string[]
  tone?: 'default' | 'warning' | 'danger'
}

const TONE_HEADER: Record<NonNullable<OperationCardProps['tone']>, string> = {
  default: 'border-border-default bg-bg-elevated text-text-primary',
  warning: 'border-warning/30 bg-warning-dim text-warning',
  danger: 'border-danger/30 bg-danger-dim text-danger',
}

export function OperationCard({ icon: Icon, title, description, steps, warnings, tone = 'default' }: OperationCardProps) {
  return (
    <div className="rounded-[var(--radius-md)] border border-border-default bg-bg-surface overflow-hidden">
      <div className={cn('flex items-center gap-2 border-b border-border-subtle px-3 py-2', TONE_HEADER[tone])}>
        <Icon className="h-4 w-4" />
        <span className="text-xs font-semibold">{title}</span>
      </div>
      <div className="p-3 space-y-3">
        <p className="text-xs text-text-secondary leading-relaxed">{description}</p>
        <ol className="list-decimal pl-4 space-y-1 text-xs text-text-secondary">
          {steps.map((s, i) => (
            <li key={i}>{s}</li>
          ))}
        </ol>
        {warnings && warnings.length > 0 && (
          <ul className="space-y-1 border-t border-border-subtle pt-3">
            {warnings.map((w, i) => (
              <li key={i} className="flex items-start gap-2 text-[11px] text-warning">
                <span aria-hidden="true">⚠</span>
                <span>{w}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
