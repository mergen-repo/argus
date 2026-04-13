import * as React from 'react'
import { cn } from '@/lib/utils'

export interface CheckboxProps extends React.InputHTMLAttributes<HTMLInputElement> {}

export const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
  ({ className, type: _ignored, ...props }, ref) => (
    <input
      ref={ref}
      type="checkbox"
      className={cn('cursor-pointer accent-accent', className)}
      {...props}
    />
  ),
)
Checkbox.displayName = 'Checkbox'
