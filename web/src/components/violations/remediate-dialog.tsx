// FIX-244 DEV-530: Remediate confirm dialog (Option C — Dialog).
//
// One component covers the three remediate actions (suspend_sim / escalate /
// dismiss) plus a bulk-dismiss variant. Reason is required (≥3 chars) for
// the destructive paths (suspend_sim, dismiss); optional for escalate.
// Mirrors the server-side validation in handler.Remediate / handler.BulkDismiss.

import * as React from 'react'
import { AlertTriangle } from 'lucide-react'
import { Dialog, DialogContent, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

export type RemediateAction = 'suspend_sim' | 'escalate' | 'dismiss'

interface RemediateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  action: RemediateAction
  /** 'single' uses sim/violation context; 'bulk' shows count + scoped copy. */
  mode?: 'single' | 'bulk'
  count?: number
  iccid?: string
  loading?: boolean
  onConfirm: (reason: string) => Promise<void> | void
}

const ACTION_COPY: Record<RemediateAction, { title: (ctx: { iccid?: string; count?: number; bulk: boolean }) => string; description: string; confirmLabel: string; destructive: boolean; reasonRequired: boolean }> = {
  suspend_sim: {
    title: ({ iccid, count, bulk }) =>
      bulk ? `Suspend SIMs on ${count ?? 0} violations?` : iccid ? `Suspend SIM ${iccid}?` : 'Suspend SIM?',
    description:
      'This disconnects all active sessions for the SIM and revokes RADIUS Access-Accept until the SIM is reactivated.',
    confirmLabel: 'Suspend SIM',
    destructive: true,
    reasonRequired: true,
  },
  escalate: {
    title: ({ count, bulk }) =>
      bulk ? `Escalate ${count ?? 0} violations?` : 'Escalate violation?',
    description: 'Notifies the on-call channel. The violation stays open until acknowledged.',
    confirmLabel: 'Escalate',
    destructive: false,
    reasonRequired: false,
  },
  dismiss: {
    title: ({ count, bulk }) =>
      bulk ? `Dismiss ${count ?? 0} violations?` : 'Dismiss violation?',
    description: 'Marks the violation as a false positive. Requires a short reason for the audit log.',
    confirmLabel: 'Dismiss',
    destructive: false,
    reasonRequired: true,
  },
}

export function RemediateDialog({
  open,
  onOpenChange,
  action,
  mode = 'single',
  count,
  iccid,
  loading,
  onConfirm,
}: RemediateDialogProps) {
  const copy = ACTION_COPY[action]
  const [reason, setReason] = React.useState('')

  React.useEffect(() => {
    if (!open) setReason('')
  }, [open])

  const trimmed = reason.trim()
  const reasonValid = !copy.reasonRequired || trimmed.length >= 3

  const handleConfirm = async () => {
    if (!reasonValid) return
    await onConfirm(trimmed)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{copy.title({ iccid, count, bulk: mode === 'bulk' })}</DialogTitle>
        <DialogDescription>{copy.description}</DialogDescription>

        {copy.destructive && (
          <div className="mt-3 flex items-start gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim p-3">
            <AlertTriangle className="h-4 w-4 text-danger shrink-0 mt-0.5" />
            <p className="text-[11px] text-danger">
              Destructive action — affects production traffic immediately.
            </p>
          </div>
        )}

        <div className="mt-3">
          <label className="block text-[10px] uppercase tracking-wider text-text-tertiary mb-1">
            Reason{copy.reasonRequired ? ' (required, ≥3 chars)' : ' (optional)'}
          </label>
          <Textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={copy.reasonRequired ? 'Why are you taking this action?' : 'Optional context for the audit log.'}
            rows={3}
            disabled={loading}
          />
        </div>

        <DialogFooter className="mt-4">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant={copy.destructive ? 'destructive' : 'default'}
            onClick={handleConfirm}
            disabled={loading || !reasonValid}
          >
            {loading ? 'Working…' : copy.confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
