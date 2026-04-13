import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { useMutation, useQueryClient, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useEscalateAnomaly } from '@/hooks/use-ops'
import type { Anomaly } from '@/types/analytics'

interface AlertActionsProps {
  anomaly: Anomaly
  onClose?: () => void
}

function useAckAnomaly(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (note: string) => {
      await api.patch(`/analytics/anomalies/${id}`, { state: 'acknowledged', note })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alerts'] })
    },
  })
}

function useResolveAnomaly(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (note: string) => {
      await api.patch(`/analytics/anomalies/${id}`, { state: 'resolved', note })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alerts'] })
    },
  })
}

function useAdminUsers() {
  return useQuery({
    queryKey: ['users', 'admin'],
    queryFn: async () => {
      const res = await api.get('/users?role=super_admin&limit=20')
      return (res.data.data ?? []) as Array<{ id: string; email: string }>
    },
    staleTime: 60_000,
  })
}

interface AckDialogProps {
  anomaly: Anomaly
  open: boolean
  onClose: () => void
}

export function AckDialog({ anomaly, open, onClose }: AckDialogProps) {
  const [note, setNote] = useState('')
  const ack = useAckAnomaly(anomaly.id)

  const handleSubmit = async () => {
    await ack.mutateAsync(note)
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="bg-bg-surface border-border rounded-[10px] max-w-md">
        <DialogHeader>
          <DialogTitle className="text-[15px] text-text-primary">Acknowledge Alert</DialogTitle>
        </DialogHeader>
        <div className="py-3">
          <p className="text-[13px] text-text-secondary mb-3">
            Acknowledging: <span className="text-accent">{anomaly.type}</span>
          </p>
          <Textarea
            placeholder="Optional note (investigation context)"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            className="bg-bg-elevated border-border text-text-primary placeholder:text-text-tertiary resize-none"
            rows={3}
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} className="text-text-secondary">Cancel</Button>
          <Button
            onClick={handleSubmit}
            disabled={ack.isPending}
            className="bg-warning text-bg-primary hover:bg-warning/90"
          >
            {ack.isPending ? <Spinner className="h-4 w-4 mr-2" /> : null}
            Acknowledge
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface ResolveDialogProps {
  anomaly: Anomaly
  open: boolean
  onClose: () => void
}

export function ResolveDialog({ anomaly, open, onClose }: ResolveDialogProps) {
  const [note, setNote] = useState('')
  const resolve = useResolveAnomaly(anomaly.id)

  const handleSubmit = async () => {
    if (!note.trim()) return
    await resolve.mutateAsync(note)
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="bg-bg-surface border-border rounded-[10px] max-w-md">
        <DialogHeader>
          <DialogTitle className="text-[15px] text-text-primary">Resolve Alert</DialogTitle>
        </DialogHeader>
        <div className="py-3">
          <p className="text-[13px] text-text-secondary mb-3">
            Resolving: <span className="text-accent">{anomaly.type}</span>
          </p>
          <Textarea
            placeholder="Resolution summary (required)"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            className="bg-bg-elevated border-border text-text-primary placeholder:text-text-tertiary resize-none"
            rows={3}
          />
          {!note.trim() && <p className="text-[11px] text-danger mt-1">Resolution summary is required</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} className="text-text-secondary">Cancel</Button>
          <Button
            onClick={handleSubmit}
            disabled={resolve.isPending || !note.trim()}
            className="bg-success text-bg-primary hover:bg-success/90"
          >
            {resolve.isPending ? <Spinner className="h-4 w-4 mr-2" /> : null}
            Resolve
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface EscalateDialogProps {
  anomaly: Anomaly
  open: boolean
  onClose: () => void
}

export function EscalateDialog({ anomaly, open, onClose }: EscalateDialogProps) {
  const [note, setNote] = useState('')
  const [onCallUserId, setOnCallUserId] = useState<string | undefined>()
  const escalate = useEscalateAnomaly(anomaly.id)
  const { data: adminUsers } = useAdminUsers()

  const handleSubmit = async () => {
    await escalate.mutateAsync({ note, on_call_user_id: onCallUserId })
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="bg-bg-surface border-border rounded-[10px] max-w-md">
        <DialogHeader>
          <DialogTitle className="text-[15px] text-text-primary">Escalate Alert</DialogTitle>
        </DialogHeader>
        <div className="py-3 space-y-3">
          <p className="text-[13px] text-text-secondary">
            Escalating: <span className="text-danger">{anomaly.type}</span>
          </p>
          <Textarea
            placeholder="Escalation note (max 500 chars)"
            value={note}
            onChange={(e) => setNote(e.target.value.slice(0, 500))}
            className="bg-bg-elevated border-border text-text-primary placeholder:text-text-tertiary resize-none"
            rows={3}
          />
          <div>
            <label className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary mb-1 block">
              On-Call User (optional)
            </label>
            <Select
              value={onCallUserId ?? 'none'}
              onChange={(e) => setOnCallUserId(e.target.value === 'none' ? undefined : e.target.value)}
              options={[
                { value: 'none', label: 'No specific on-call' },
                ...(adminUsers ?? []).map((u) => ({ value: u.id, label: u.email })),
              ]}
              className="bg-bg-elevated border-border text-text-primary text-[13px] w-full"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} className="text-text-secondary">Cancel</Button>
          <Button
            onClick={handleSubmit}
            disabled={escalate.isPending}
            className="bg-danger text-bg-primary hover:bg-danger/90"
          >
            {escalate.isPending ? <Spinner className="h-4 w-4 mr-2" /> : null}
            Escalate
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AlertActionButtons({ anomaly }: AlertActionsProps) {
  const [ackOpen, setAckOpen] = useState(false)
  const [resolveOpen, setResolveOpen] = useState(false)
  const [escalateOpen, setEscalateOpen] = useState(false)

  const canAck = anomaly.state === 'open'
  const canResolve = anomaly.state === 'open' || anomaly.state === 'acknowledged'
  const canEscalate = anomaly.state === 'open' || anomaly.state === 'acknowledged'

  return (
    <>
      <div className="flex items-center gap-1">
        {canAck && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setAckOpen(true)}
            className="text-warning hover:bg-warning-dim text-[12px] h-7 px-2"
            aria-label="Acknowledge"
          >
            Ack
          </Button>
        )}
        {canResolve && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setResolveOpen(true)}
            className="text-success hover:bg-success-dim text-[12px] h-7 px-2"
            aria-label="Resolve"
          >
            Resolve
          </Button>
        )}
        {canEscalate && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setEscalateOpen(true)}
            className="text-danger hover:bg-danger-dim text-[12px] h-7 px-2"
            aria-label="Escalate"
          >
            Escalate
          </Button>
        )}
      </div>

      <AckDialog anomaly={anomaly} open={ackOpen} onClose={() => setAckOpen(false)} />
      <ResolveDialog anomaly={anomaly} open={resolveOpen} onClose={() => setResolveOpen(false)} />
      <EscalateDialog anomaly={anomaly} open={escalateOpen} onClose={() => setEscalateOpen(false)} />
    </>
  )
}
