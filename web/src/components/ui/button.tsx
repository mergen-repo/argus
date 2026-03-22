import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap text-sm font-medium transition-all duration-200 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0 cursor-pointer',
  {
    variants: {
      variant: {
        default:
          'bg-accent text-bg-primary shadow-sm hover:bg-accent/90 hover:shadow-[0_0_20px_rgba(0,212,255,0.3)]',
        destructive:
          'bg-danger text-white shadow-sm hover:bg-danger/90',
        outline:
          'border border-border bg-transparent text-text-primary hover:bg-bg-hover hover:border-text-tertiary',
        secondary:
          'bg-bg-elevated text-text-primary border border-border hover:bg-bg-hover',
        ghost:
          'text-text-secondary hover:bg-bg-hover hover:text-text-primary',
        link:
          'text-accent underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2 rounded-[var(--radius-sm)]',
        sm: 'h-8 px-3 text-xs rounded-[var(--radius-sm)]',
        lg: 'h-10 px-6 rounded-[var(--radius-sm)]',
        icon: 'h-9 w-9 rounded-[var(--radius-sm)]',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, ...props }, ref) => {
    return (
      <button
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'

export { Button, buttonVariants }
