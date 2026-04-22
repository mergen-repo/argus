import * as React from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

interface SlidePanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  children: React.ReactNode
  title?: string
  description?: string
  width?: 'sm' | 'md' | 'lg' | 'xl'
  side?: 'right' | 'left'
}

const widthMap = {
  sm: 'max-w-md',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
}

function SlidePanel({ open, onOpenChange, children, title, description, width = 'lg', side = 'right' }: SlidePanelProps) {
  const panelRef = React.useRef<HTMLDivElement | null>(null)
  const closeBtnRef = React.useRef<HTMLButtonElement | null>(null)
  const titleId = React.useId()
  const descId = React.useId()

  React.useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null
    // Auto-focus the close button on open for a predictable tab start.
    const focusTimer = window.setTimeout(() => {
      closeBtnRef.current?.focus()
    }, 0)

    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onOpenChange(false)
        return
      }
      if (e.key !== 'Tab' || !panelRef.current) return
      // Simple focus trap: cycle focus between first/last tabbable elements.
      const focusables = panelRef.current.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      )
      if (focusables.length === 0) return
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', handleKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.body.style.overflow = ''
      window.clearTimeout(focusTimer)
      previouslyFocused?.focus?.()
    }
  }, [open, onOpenChange])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50">
      <div
        className="fixed inset-0 bg-black/50 backdrop-blur-[2px] animate-fade-in"
        onClick={() => onOpenChange(false)}
        aria-hidden="true"
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        aria-describedby={description ? descId : undefined}
        className={cn(
          'fixed top-0 h-full flex flex-col border bg-bg-surface shadow-2xl',
          'transition-transform duration-300 ease-out',
          widthMap[width],
          side === 'right'
            ? 'right-0 border-l border-border animate-slide-right-in'
            : 'left-0 border-r border-border',
          'w-full',
        )}
      >
        <div className="flex h-14 items-center justify-between border-b border-border px-5 shrink-0">
          <div className="flex flex-col">
            {title && (
              <h2 id={titleId} className="text-[15px] font-semibold text-text-primary">
                {title}
              </h2>
            )}
            {description && (
              <p id={descId} className="text-xs text-text-secondary mt-0.5">
                {description}
              </p>
            )}
          </div>
          <button
            ref={closeBtnRef}
            type="button"
            onClick={() => onOpenChange(false)}
            className="rounded-md p-1.5 text-text-tertiary hover:text-text-primary hover:bg-bg-hover transition-colors"
            aria-label="Close panel"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-5">
          {children}
        </div>
      </div>
    </div>
  )
}

const SlidePanelFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('flex items-center justify-end gap-3 border-t border-border px-5 py-3 mt-auto shrink-0 bg-bg-surface', className)} {...props} />
)
SlidePanelFooter.displayName = 'SlidePanelFooter'

export { SlidePanel, SlidePanelFooter }
