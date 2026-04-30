import * as React from 'react'
import { cn } from '@/lib/utils'

export interface RadioProps extends React.InputHTMLAttributes<HTMLInputElement> {}

export const Radio = React.forwardRef<HTMLInputElement, RadioProps>(
  ({ className, type: _ignored, ...props }, ref) => (
    <input
      ref={ref}
      type="radio"
      className={cn('cursor-pointer accent-accent', className)}
      {...props}
    />
  ),
)
Radio.displayName = 'Radio'
