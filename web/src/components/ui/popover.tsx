import * as React from 'react'
import { cn } from '@/lib/utils'

// Minimal controlled popover primitive. shadcn's Radix-based Popover is
// not wired into this project (see sheet.tsx + command.tsx) — this
// hand-rolled disclosure mirrors the Sheet pattern: controlled open
// state, outside-click + Escape close handling, token-driven styling.

interface PopoverContextValue {
  open: boolean
  setOpen: (open: boolean) => void
  triggerRef: React.RefObject<HTMLButtonElement | null>
  contentRef: React.RefObject<HTMLDivElement | null>
}

const PopoverContext = React.createContext<PopoverContextValue | null>(null)

function usePopoverContext() {
  const ctx = React.useContext(PopoverContext)
  if (!ctx) throw new Error('Popover subcomponents must be used inside <Popover>')
  return ctx
}

export interface PopoverProps {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  defaultOpen?: boolean
  children: React.ReactNode
}

export function Popover({ open: controlledOpen, onOpenChange, defaultOpen = false, children }: PopoverProps) {
  const [uncontrolledOpen, setUncontrolledOpen] = React.useState(defaultOpen)
  const isControlled = controlledOpen !== undefined
  const open = isControlled ? controlledOpen : uncontrolledOpen
  const setOpen = React.useCallback(
    (next: boolean) => {
      if (!isControlled) setUncontrolledOpen(next)
      onOpenChange?.(next)
    },
    [isControlled, onOpenChange],
  )
  const triggerRef = React.useRef<HTMLButtonElement | null>(null)
  const contentRef = React.useRef<HTMLDivElement | null>(null)

  return (
    <PopoverContext.Provider value={{ open, setOpen, triggerRef, contentRef }}>{children}</PopoverContext.Provider>
  )
}

export interface PopoverTriggerProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  asChild?: boolean
}

export const PopoverTrigger = React.forwardRef<HTMLButtonElement, PopoverTriggerProps>(
  ({ onClick, ...props }, ref) => {
    const { open, setOpen, triggerRef } = usePopoverContext()
    const mergedRef = React.useCallback(
      (node: HTMLButtonElement | null) => {
        triggerRef.current = node
        if (typeof ref === 'function') ref(node)
        else if (ref) (ref as React.MutableRefObject<HTMLButtonElement | null>).current = node
      },
      [ref, triggerRef],
    )
    return (
      <button
        ref={mergedRef}
        type="button"
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={(e) => {
          onClick?.(e)
          if (!e.defaultPrevented) setOpen(!open)
        }}
        {...props}
      />
    )
  },
)
PopoverTrigger.displayName = 'PopoverTrigger'

export interface PopoverContentProps extends React.HTMLAttributes<HTMLDivElement> {
  align?: 'start' | 'end' | 'center'
  sideOffset?: number
}

export const PopoverContent = React.forwardRef<HTMLDivElement, PopoverContentProps>(
  ({ className, align = 'start', sideOffset = 6, style, children, ...props }, ref) => {
    const { open, setOpen, triggerRef, contentRef } = usePopoverContext()
    const mergedRef = React.useCallback(
      (node: HTMLDivElement | null) => {
        contentRef.current = node
        if (typeof ref === 'function') ref(node)
        else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = node
      },
      [ref, contentRef],
    )

    React.useEffect(() => {
      if (!open) return
      const handleKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') setOpen(false)
      }
      const handleMouse = (e: MouseEvent) => {
        const t = e.target as Node
        if (contentRef.current?.contains(t)) return
        if (triggerRef.current?.contains(t)) return
        setOpen(false)
      }
      document.addEventListener('keydown', handleKey)
      document.addEventListener('mousedown', handleMouse)
      return () => {
        document.removeEventListener('keydown', handleKey)
        document.removeEventListener('mousedown', handleMouse)
      }
    }, [open, setOpen, contentRef, triggerRef])

    if (!open) return null

    const alignClass =
      align === 'end' ? 'right-0' : align === 'center' ? 'left-1/2 -translate-x-1/2' : 'left-0'

    return (
      <div className="relative inline-block">
        <div
          ref={mergedRef}
          role="dialog"
          className={cn(
            'absolute z-50 min-w-[240px] rounded-[var(--radius-sm)] border border-border bg-bg-elevated shadow-2xl',
            'animate-in fade-in-0 zoom-in-95',
            alignClass,
            className,
          )}
          style={{ marginTop: sideOffset, ...style }}
          {...props}
        >
          {children}
        </div>
      </div>
    )
  },
)
PopoverContent.displayName = 'PopoverContent'
