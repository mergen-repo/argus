// FIX-239 DEV-536: numbered stepper for onboarding / lifecycle / workflow shapes.
//
// Two layout variants: 'horizontal' (used in compact lifecycle timelines) and
// 'vertical' (used in onboarding stepper). Status-aware colours map to the
// project's semantic tokens (PAT-018 sentinel — no Tailwind numbered palette).

import { cn } from '@/lib/utils'
import { Check, ArrowRight } from 'lucide-react'

export type StepStatus = 'done' | 'current' | 'pending'

export interface StepDef {
  label: string
  desc?: string
  status?: StepStatus
}

interface StepperFlowProps {
  steps: StepDef[]
  layout?: 'horizontal' | 'vertical'
  className?: string
}

const STATUS_TOKENS: Record<StepStatus, { circle: string; line: string; label: string }> = {
  done: {
    circle: 'bg-success-dim border-success/40 text-success',
    line: 'bg-success/40',
    label: 'text-text-primary',
  },
  current: {
    circle: 'bg-accent-dim border-accent/40 text-accent',
    line: 'bg-border-default',
    label: 'text-text-primary',
  },
  pending: {
    circle: 'bg-bg-elevated border-border-default text-text-tertiary',
    line: 'bg-border-subtle',
    label: 'text-text-secondary',
  },
}

export function StepperFlow({ steps, layout = 'vertical', className }: StepperFlowProps) {
  if (layout === 'horizontal') {
    return (
      <div className={cn('flex flex-wrap items-start gap-0', className)}>
        {steps.map((s, i) => {
          const status = s.status ?? 'pending'
          const tok = STATUS_TOKENS[status]
          return (
            <div key={`${i}-${s.label}`} className="flex items-start">
              <div className="flex flex-col items-center max-w-[120px]">
                <div
                  className={cn(
                    'flex h-7 w-7 items-center justify-center rounded-full border text-xs font-semibold',
                    tok.circle,
                  )}
                >
                  {status === 'done' ? <Check className="h-3.5 w-3.5" /> : i + 1}
                </div>
                <div className="mt-2 text-center">
                  <div className={cn('text-xs font-medium leading-tight', tok.label)}>{s.label}</div>
                  {s.desc && <div className="text-[10px] text-text-tertiary mt-0.5 leading-tight">{s.desc}</div>}
                </div>
              </div>
              {i < steps.length - 1 && (
                <ArrowRight className="h-4 w-4 text-border mt-1.5 mx-1 shrink-0" aria-hidden="true" />
              )}
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <ol className={cn('space-y-3', className)}>
      {steps.map((s, i) => {
        const status = s.status ?? 'pending'
        const tok = STATUS_TOKENS[status]
        return (
          <li key={`${i}-${s.label}`} className="flex items-start gap-3">
            <div
              className={cn(
                'flex h-7 w-7 items-center justify-center rounded-full border text-xs font-semibold flex-shrink-0',
                tok.circle,
              )}
            >
              {status === 'done' ? <Check className="h-3.5 w-3.5" /> : i + 1}
            </div>
            <div className="flex-1 min-w-0">
              <div className={cn('text-xs font-medium leading-snug', tok.label)}>{s.label}</div>
              {s.desc && <p className="text-xs text-text-tertiary mt-0.5 leading-relaxed">{s.desc}</p>}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
