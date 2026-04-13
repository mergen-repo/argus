import * as React from 'react'
import { cn } from '@/lib/utils'

export interface FileInputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> {}

export const FileInput = React.forwardRef<HTMLInputElement, FileInputProps>(
  ({ className, ...props }, ref) => (
    <input ref={ref} type="file" className={cn(className)} {...props} />
  ),
)
FileInput.displayName = 'FileInput'
