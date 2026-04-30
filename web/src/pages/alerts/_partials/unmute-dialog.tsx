import { useEffect, useState } from 'react'
import axios from 'axios'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Spinner } from '@/components/ui/spinner'
import { api } from '@/lib/api'

const MAX_REASON = 500

interface UnmuteResponse {
  status: string
  data: {
    id: string
    restored_count?: number
  }
}

export interface UnmuteDialogProps {
  open: boolean
  onClose: () => void
  suppressionId: string | null
  ruleName?: string
  onSuccess?: () => void
}

export function UnmuteDialog({ open, onClose, suppressionId, ruleName, onSuccess }: UnmuteDialogProps) {
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) {
      setReason('')
      setSubmitting(false)
    }
  }, [open])

  const handleSubmit = async () => {
    if (!suppressionId || submitting) return
    setSubmitting(true)
    try {
      const res = await api.delete<UnmuteResponse>(`/alerts/suppressions/${suppressionId}`, {
        data: reason.trim() ? { reason: reason.trim().slice(0, MAX_REASON) } : {},
      })
      const restored = res.data.data?.restored_count ?? 0
      toast.success(
        restored > 0
          ? `Suppression removed — ${restored} alert${restored === 1 ? '' : 's'} restored`
          : 'Suppression removed',
      )
      onSuccess?.()
      onClose()
    } catch (err: unknown) {
      if (axios.isAxiosError(err)) {
        const message = err.response?.data?.error?.message
        toast.error(message ?? 'Failed to remove suppression')
      } else {
        toast.error('Failed to remove suppression')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Remove suppression rule?</DialogTitle>
          <DialogDescription>
            Alerts currently suppressed by this rule may return to open state. This action is auditable.
          </DialogDescription>
        </DialogHeader>

        {ruleName && (
          <p className="text-[12px] text-text-secondary">
            Rule:&nbsp;
            <span className="font-medium text-text-primary">{ruleName}</span>
          </p>
        )}

        <div className="flex flex-col gap-1 py-2">
          <label htmlFor="unmute-reason" className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">
            Reason (optional)
          </label>
          <Textarea
            id="unmute-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value.slice(0, MAX_REASON))}
            rows={3}
            placeholder="Why is this being unmuted?"
          />
          <span className="self-end text-[10px] text-text-tertiary">{reason.length}/{MAX_REASON}</span>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={handleSubmit}
            disabled={submitting || !suppressionId}
            className="gap-1.5"
          >
            {submitting ? <Spinner className="h-3.5 w-3.5" /> : null}
            Unmute
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default UnmuteDialog
