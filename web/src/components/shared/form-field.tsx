import * as React from 'react'
import { cn } from '@/lib/utils'

interface FormFieldProps {
  label: string
  required?: boolean
  error?: string
  children: React.ReactNode
  className?: string
  description?: string
}

export const FormField = React.memo(function FormField({
  label,
  required,
  error,
  children,
  className,
  description,
}: FormFieldProps) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <label className="text-xs font-medium text-text-primary flex items-center gap-1">
        {label}
        {required && <span className="text-danger" aria-hidden>*</span>}
      </label>
      {children}
      {description && !error && (
        <p className="text-[10px] text-text-tertiary">{description}</p>
      )}
      {error && (
        <p className="text-[10px] text-danger" role="alert">{error}</p>
      )}
    </div>
  )
})
