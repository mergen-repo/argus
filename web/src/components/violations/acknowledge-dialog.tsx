// FIX-244 DEV-530: compact Acknowledge confirm dialog (Option C — Dialog).
//
// Used by the row action menu, the SlidePanel footer, and the bulk bar.
// Note is optional; passes through to the Acknowledge endpoint as-is.

import * as React from 'react'
import { Dialog, DialogContent, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

interface AcknowledgeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 'single' shows a violation-specific title; 'bulk' shows a count. */
  mode?: 'single' | 'bulk'
  count?: number
  violationLabel?: string
  loading?: boolean
  onConfirm: (note: string) => Promise<void> | void
}

export function AcknowledgeDialog({
  open,
  onOpenChange,
  mode = 'single',
  count,
  violationLabel,
  loading,
  onConfirm,
}: AcknowledgeDialogProps) {
  const [note, setNote] = React.useState('')

  React.useEffect(() => {
    if (!open) setNote('')
  }, [open])

  const title =
    mode === 'bulk'
      ? `Acknowledge ${count ?? 0} violation${(count ?? 0) === 1 ? '' : 's'}`
      : violationLabel
      ? `Acknowledge "${violationLabel}"`
      : 'Acknowledge violation'

  const handleConfirm = async () => {
    await onConfirm(note.trim())
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>
          Acknowledging records that this violation has been seen by the team. It does not change the SIM state.
        </DialogDescription>
        <div className="mt-3">
          <label className="block text-[10px] uppercase tracking-wider text-text-tertiary mb-1">
            Note <span className="text-text-tertiary normal-case">(optional)</span>
          </label>
          <Textarea
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="What did you observe?"
            rows={3}
            disabled={loading}
          />
        </div>
        <DialogFooter className="mt-4">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleConfirm} disabled={loading}>
            {loading ? 'Acknowledging…' : 'Acknowledge'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
