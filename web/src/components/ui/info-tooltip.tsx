import * as React from 'react'
import { Info } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { GLOSSARY_TOOLTIPS } from '@/lib/glossary-tooltips'

interface InfoTooltipProps {
  term: string
  children: React.ReactNode
  side?: 'top' | 'bottom' | 'left' | 'right'
  className?: string
}

const positionClasses: Record<string, string> = {
  top: 'bottom-full left-1/2 -translate-x-1/2 mb-2',
  bottom: 'top-full left-1/2 -translate-x-1/2 mt-2',
  left: 'right-full top-1/2 -translate-y-1/2 mr-2',
  right: 'left-full top-1/2 -translate-y-1/2 ml-2',
}

export const InfoTooltip = React.memo(function InfoTooltip({
  term,
  children,
  side = 'top',
  className,
}: InfoTooltipProps) {
  const [open, setOpen] = React.useState(false)
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null)
  const content = GLOSSARY_TOOLTIPS[term]

  React.useEffect(() => {
    if (!open) return
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open])

  React.useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [])

  if (!content) {
    // FIX-250: Vite-native env (process.env unavailable in browser bundle)
    if (import.meta.env.DEV) {
      console.warn(`[InfoTooltip] Unknown term: "${term}". Add it to glossary-tooltips.ts.`)
    }
    return <span className={className}>{children}</span>
  }

  const handleMouseEnter = () => {
    timerRef.current = setTimeout(() => setOpen(true), 500)
  }

  const handleMouseLeave = () => {
    if (timerRef.current) clearTimeout(timerRef.current)
    setOpen(false)
  }

  const handleClick = () => {
    if (timerRef.current) clearTimeout(timerRef.current)
    setOpen((prev) => !prev)
  }

  return (
    <span className={cn('inline-flex items-center gap-[2px]', className)}>
      {children}
      <span className="relative inline-flex">
        <Button
          variant="ghost"
          size="icon"
          aria-label={`What is ${term}?`}
          aria-expanded={open}
          className="h-4 w-4 p-0 rounded hover:bg-transparent"
          onMouseEnter={handleMouseEnter}
          onMouseLeave={handleMouseLeave}
          onClick={handleClick}
          onFocus={handleMouseEnter}
          onBlur={handleMouseLeave}
        >
          <Info className="h-3 w-3 text-text-tertiary hover:text-text-secondary transition-colors" />
        </Button>
        {open && (
          <div
            role="tooltip"
            className={cn(
              'absolute z-50 max-w-[220px] whitespace-normal rounded-[var(--radius-sm)] bg-bg-elevated border border-border px-2 py-1.5 text-xs text-text-primary shadow-lg',
              'animate-in fade-in-0 zoom-in-95',
              positionClasses[side],
            )}
          >
            {content}
          </div>
        )}
      </span>
    </span>
  )
})
