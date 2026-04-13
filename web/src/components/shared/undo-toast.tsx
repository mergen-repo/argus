import * as React from 'react'
import { useUndo } from '@/hooks/use-undo'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface UndoToastProps {
  actionId: string
  message: string
  invalidateKeys?: string[][]
  className?: string
}

export const UndoToast = React.memo(function UndoToast({
  actionId,
  message,
  invalidateKeys,
  className,
}: UndoToastProps) {
  const { register } = useUndo(invalidateKeys)

  React.useEffect(() => {
    register(actionId, message)
  }, [actionId, message, register])

  return null
})
