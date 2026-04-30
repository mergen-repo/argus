// FIX-239 DEV-536: horizontal timeline with marker dots and labels.

import { cn } from '@/lib/utils'

export interface TimelineMarker {
  /** Position 0..100 along the line. */
  pct: number
  label: string
  desc?: string
  tone?: 'default' | 'warning' | 'danger' | 'success'
}

interface TimelineFlowProps {
  markers: TimelineMarker[]
  className?: string
}

const TONE_DOT: Record<NonNullable<TimelineMarker['tone']>, string> = {
  default: 'bg-accent',
  warning: 'bg-warning',
  danger: 'bg-danger',
  success: 'bg-success',
}

export function TimelineFlow({ markers, className }: TimelineFlowProps) {
  return (
    <div className={cn('relative pt-6 pb-12', className)}>
      <div className="absolute left-0 right-0 top-8 h-0.5 bg-border-default" aria-hidden="true" />
      <div className="relative h-2">
        {markers.map((m, i) => (
          <div
            key={`${i}-${m.label}`}
            className="absolute -translate-x-1/2 flex flex-col items-center gap-1"
            style={{ left: `${Math.max(0, Math.min(100, m.pct))}%` }}
          >
            <span
              className={cn('h-3 w-3 rounded-full ring-2 ring-bg-surface', TONE_DOT[m.tone ?? 'default'])}
              aria-hidden="true"
            />
            <span className="absolute top-5 whitespace-nowrap text-[10px] font-medium text-text-primary text-center">
              {m.label}
            </span>
            {m.desc && (
              <span className="absolute top-10 whitespace-nowrap text-[9px] text-text-tertiary">{m.desc}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
