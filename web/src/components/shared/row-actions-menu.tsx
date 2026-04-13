import * as React from 'react'
import { MoreVertical } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export interface RowAction {
  label: string
  icon?: React.ElementType
  onClick: () => void
  variant?: 'default' | 'destructive'
  disabled?: boolean
  separator?: boolean
}

interface RowActionsMenuProps {
  actions: RowAction[]
  className?: string
}

export const RowActionsMenu = React.memo(function RowActionsMenu({
  actions,
  className,
}: RowActionsMenuProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn('p-1 rounded-[var(--radius-sm)] text-text-tertiary hover:text-text-primary hover:bg-bg-hover transition-colors', className)}
        onClick={(e) => e.stopPropagation()}
        aria-label="Row actions"
      >
        <MoreVertical className="h-4 w-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        {actions.map((action, idx) => (
          <React.Fragment key={idx}>
            {action.separator && idx > 0 && <DropdownMenuSeparator />}
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation()
                action.onClick()
              }}
              disabled={action.disabled}
              className={cn(
                action.variant === 'destructive' && 'text-status-error focus:text-status-error',
              )}
            >
              {action.icon && <action.icon className="mr-2 h-3.5 w-3.5" />}
              {action.label}
            </DropdownMenuItem>
          </React.Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
})
