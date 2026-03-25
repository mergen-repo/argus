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
  React.useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onOpenChange(false)
    }
    if (open) {
      document.addEventListener('keydown', handleEscape)
      document.body.style.overflow = 'hidden'
    }
    return () => {
      document.removeEventListener('keydown', handleEscape)
      document.body.style.overflow = ''
    }
  }, [open, onOpenChange])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50">
      <div
        className="fixed inset-0 bg-black/50 backdrop-blur-[2px] animate-fade-in"
        onClick={() => onOpenChange(false)}
      />
      <div
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
            {title && <h2 className="text-[15px] font-semibold text-text-primary">{title}</h2>}
            {description && <p className="text-xs text-text-secondary mt-0.5">{description}</p>}
          </div>
          <button
            onClick={() => onOpenChange(false)}
            className="rounded-md p-1.5 text-text-tertiary hover:text-text-primary hover:bg-bg-hover transition-colors"
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
