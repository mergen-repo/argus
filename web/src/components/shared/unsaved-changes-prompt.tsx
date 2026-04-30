import * as React from 'react'
import { useBlocker } from 'react-router-dom'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface UnsavedChangesPromptProps {
  isDirty: boolean
}

export const UnsavedChangesPrompt = React.memo(function UnsavedChangesPrompt({
  isDirty,
}: UnsavedChangesPromptProps) {
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    isDirty && currentLocation.pathname !== nextLocation.pathname,
  )

  if (blocker.state !== 'blocked') return null

  return (
    <Dialog open onOpenChange={() => blocker.reset?.()}>
      <DialogContent className="sm:max-w-xs">
        <DialogHeader>
          <DialogTitle>Unsaved Changes</DialogTitle>
          <DialogDescription>
            You have unsaved changes. Are you sure you want to leave? Your changes will be lost.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => blocker.reset?.()}>Stay</Button>
          <Button variant="destructive" size="sm" onClick={() => blocker.proceed?.()}>Leave</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})
