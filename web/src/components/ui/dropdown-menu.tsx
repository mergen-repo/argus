import * as React from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'

interface DropdownMenuProps {
  children: React.ReactNode
}

interface DropdownContextValue {
  open: boolean
  setOpen: (open: boolean) => void
  triggerRef: React.RefObject<HTMLDivElement | null>
}

const DropdownContext = React.createContext<DropdownContextValue>({
  open: false,
  setOpen: () => {},
  triggerRef: { current: null },
})

function DropdownMenu({ children }: DropdownMenuProps) {
  const [open, setOpen] = React.useState(false)

  React.useEffect(() => {
    if (!open) return
    const handleClick = () => setOpen(false)
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('click', handleClick)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('click', handleClick)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [open])

  const ref = React.useRef<HTMLDivElement>(null)
  return (
    <DropdownContext.Provider value={{ open, setOpen, triggerRef: ref }}>
      <div ref={ref} className="relative inline-block">{children}</div>
    </DropdownContext.Provider>
  )
}

function DropdownMenuTrigger({ children, className, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  const { open, setOpen } = React.useContext(DropdownContext)
  return (
    <button
      className={className}
      onClick={(e) => {
        e.stopPropagation()
        setOpen(!open)
      }}
      {...props}
    >
      {children}
    </button>
  )
}

function DropdownMenuContent({ children, className, align = 'end' }: React.HTMLAttributes<HTMLDivElement> & { align?: 'start' | 'end' }) {
  const { open, triggerRef } = React.useContext(DropdownContext)
  const [pos, setPos] = React.useState<{ top: number; left: number; right: number } | null>(null)

  React.useEffect(() => {
    if (!open || !triggerRef.current) { setPos(null); return }
    const rect = triggerRef.current.getBoundingClientRect()
    setPos({
      top: rect.bottom + 4,
      left: rect.left,
      right: window.innerWidth - rect.right,
    })
  }, [open, triggerRef])

  if (!open || !pos) return null

  return createPortal(
    <div
      className={cn(
        'fixed z-[9999] min-w-[10rem] overflow-hidden rounded-[var(--radius-md)] border border-border bg-bg-elevated p-1 shadow-lg animate-in fade-in slide-in-from-top-1 duration-100',
        className,
      )}
      style={{
        top: pos.top,
        ...(align === 'end' ? { right: pos.right } : { left: pos.left }),
      }}
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>,
    document.body,
  )
}

function DropdownMenuItem({ className, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  const { setOpen } = React.useContext(DropdownContext)
  return (
    <button
      className={cn(
        'flex w-full items-center gap-2 rounded-[4px] px-2 py-1.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors cursor-pointer whitespace-nowrap text-left',
        className,
      )}
      onClick={(e) => {
        props.onClick?.(e)
        setOpen(false)
      }}
      {...props}
    />
  )
}

function DropdownMenuSeparator({ className }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('-mx-1 my-1 h-px bg-border', className)} />
}

function DropdownMenuLabel({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('px-2 py-1.5 text-xs font-medium text-text-tertiary', className)}
      {...props}
    />
  )
}

export { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuLabel }
