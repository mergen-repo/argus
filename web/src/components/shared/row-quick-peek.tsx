import * as React from 'react'
import { cn } from '@/lib/utils'

export interface QuickPeekField {
  label: string
  value: React.ReactNode
}

interface RowQuickPeekProps {
  children: React.ReactNode
  title: string
  fields: QuickPeekField[]
  className?: string
}

export function RowQuickPeek({ children, title, fields, className }: RowQuickPeekProps) {
  const [visible, setVisible] = React.useState(false)
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const wrapperRef = React.useRef<HTMLDivElement>(null)

  const handleMouseEnter = React.useCallback(() => {
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => setVisible(true), 500)
  }, [])

  const handleMouseLeave = React.useCallback(() => {
    clearTimeout(timerRef.current)
    setVisible(false)
  }, [])

  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setVisible(false)
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      clearTimeout(timerRef.current)
    }
  }, [])

  return (
    <div
      ref={wrapperRef}
      className={cn('relative', className)}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
    >
      {children}
      {visible && (
        <div className="absolute left-0 top-full z-40 mt-1 w-64 rounded-[var(--radius-md)] border border-border bg-bg-surface shadow-lg animate-in fade-in slide-in-from-top-1 duration-150">
          <div className="px-3 py-2 border-b border-border">
            <span className="text-xs font-semibold text-text-primary truncate block">{title}</span>
          </div>
          <div className="px-3 py-2 space-y-1.5">
            {fields.map((field, idx) => (
              <div key={idx} className="flex items-start justify-between gap-2">
                <span className="text-[11px] text-text-tertiary shrink-0">{field.label}</span>
                <span className="text-[11px] text-text-secondary text-right">{field.value}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
