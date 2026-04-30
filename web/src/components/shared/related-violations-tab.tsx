import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { Shield, MoreHorizontal, CheckCircle2, Ban, ExternalLink, ArrowUpRight, AlertTriangle } from 'lucide-react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SeverityBadge } from '@/components/shared/severity-badge'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ListResponse, ApiResponse } from '@/types/sim'
import { timeAgo } from '@/lib/format'
import { EntityLink } from './entity-link'
import { toast } from 'sonner'

// FIX-244 DEV-524: PolicyViolation is now defined in @/types/violation
// (single source of truth shared with pages/violations/index.tsx). The
// re-export below keeps existing `from '@/components/shared'` consumers
// working without changes — only the definition site moved.
import type { PolicyViolation } from '@/types/violation'
export type { PolicyViolation } from '@/types/violation'

interface RelatedViolationsTabProps {
  entityId: string
  scope: 'sim' | 'policy'
}

function useRelatedViolations(entityId: string, scope: 'sim' | 'policy') {
  return useQuery({
    queryKey: ['policy-violations', 'related', scope, entityId],
    queryFn: async () => {
      const params = new URLSearchParams({ limit: '50' })
      if (scope === 'sim') params.set('sim_id', entityId)
      else params.set('policy_id', entityId)
      const res = await api.get<ListResponse<PolicyViolation>>(`/policy-violations?${params.toString()}`)
      return res.data.data ?? []
    },
    staleTime: 30_000,
    enabled: !!entityId,
  })
}

function useDismissViolation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, note }: { id: string; note: string }) => {
      const res = await api.post<ApiResponse<PolicyViolation>>(
        `/policy-violations/${id}/acknowledge`,
        { note },
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policy-violations'] })
      toast.success('Violation dismissed')
    },
    onError: () => toast.error('Failed to dismiss violation'),
  })
}

function useRemediateViolation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, action, reason }: { id: string; action: string; reason: string }) => {
      const res = await api.post(`/policy-violations/${id}/remediate`, { action, reason })
      return res.data
    },
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: ['policy-violations'] })
      queryClient.invalidateQueries({ queryKey: ['sims'] })
      const msg = vars.action === 'suspend_sim' ? 'SIM suspended, violation acknowledged' : 'Action completed'
      toast.success(msg)
    },
    onError: () => toast.error('Remediation failed'),
  })
}

interface ConfirmDialogState {
  violationId: string
  action: 'suspend_sim' | 'escalate'
  label: string
}

export function RelatedViolationsTab({ entityId, scope }: RelatedViolationsTabProps) {
  const navigate = useNavigate()
  const { data: violations = [], isLoading, isError } = useRelatedViolations(entityId, scope)
  const dismiss = useDismissViolation()
  const remediate = useRemediateViolation()

  const [confirmDialog, setConfirmDialog] = React.useState<ConfirmDialogState | null>(null)
  const [reason, setReason] = React.useState('')

  function handleConfirm() {
    if (!confirmDialog) return
    remediate.mutate(
      { id: confirmDialog.violationId, action: confirmDialog.action, reason },
      {
        onSuccess: () => {
          setConfirmDialog(null)
          setReason('')
        },
      },
    )
  }

  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <p className="text-[13px] text-danger mb-1">Failed to load violations</p>
        <p className="text-[11px] text-text-tertiary">Please try refreshing the page</p>
      </div>
    )
  }

  if (violations.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <Shield className="h-10 w-10 text-success mx-auto mb-3 opacity-40" />
        <p className="text-[13px] text-text-secondary mb-1">No policy violations</p>
        <p className="text-[11px] text-text-tertiary">This {scope} has no recorded violations</p>
      </div>
    )
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border-subtle hover:bg-transparent">
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Type</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Severity</TableHead>
            {scope === 'policy' && (
              <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">SIM</TableHead>
            )}
            {scope === 'sim' && (
              <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Policy</TableHead>
            )}
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Time</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Status</TableHead>
            <TableHead className="w-8 py-2" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {violations.map((v) => (
            <TableRow
              key={v.id}
              className="hover:bg-bg-hover transition-colors duration-150 cursor-pointer"
              onClick={() => navigate(`/violations/${v.id}`)}
            >
              <TableCell className="py-2.5">
                <span className="text-[12px] text-text-primary font-medium">
                  {v.violation_type.replace(/_/g, ' ')}
                </span>
                <span className="ml-1.5 text-[11px] text-text-tertiary">rule #{v.rule_index}</span>
              </TableCell>
              <TableCell className="py-2.5">
                <SeverityBadge severity={v.severity} />
              </TableCell>
              {scope === 'policy' && (
                <TableCell className="py-2.5" onClick={(e) => e.stopPropagation()}>
                  <EntityLink entityType="sim" entityId={v.sim_id} label={v.sim_iccid ?? undefined} truncate />
                </TableCell>
              )}
              {scope === 'sim' && (
                <TableCell className="py-2.5" onClick={(e) => e.stopPropagation()}>
                  <EntityLink entityType="policy" entityId={v.policy_id} label={v.policy_name ?? undefined} truncate />
                </TableCell>
              )}
              <TableCell className="py-2.5">
                <span className="text-[11px] text-text-tertiary" title={new Date(v.created_at).toISOString()}>
                  {timeAgo(v.created_at)}
                </span>
              </TableCell>
              <TableCell className="py-2.5">
                {v.acknowledged_at ? (
                  <Badge variant="secondary" className="text-[10px]">Dismissed</Badge>
                ) : (
                  <Badge variant="warning" className="text-[10px]">Open</Badge>
                )}
              </TableCell>
              <TableCell className="py-2.5" onClick={(e) => e.stopPropagation()}>
                <DropdownMenu>
                  <DropdownMenuTrigger className="inline-flex items-center justify-center h-7 w-7 rounded-[var(--radius-sm)] text-text-tertiary hover:bg-bg-hover hover:text-text-primary transition-colors duration-200" aria-label="Actions">
                    <MoreHorizontal className="h-3.5 w-3.5" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuItem
                      onClick={() => navigate(`/violations/${v.id}`)}
                      className="text-[12px]"
                    >
                      <ExternalLink className="h-3.5 w-3.5 mr-2" />
                      View Details
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    {!v.acknowledged_at && (
                      <>
                        <DropdownMenuItem
                          onClick={() => setConfirmDialog({ violationId: v.id, action: 'suspend_sim', label: 'Suspend SIM' })}
                          className="text-[12px] text-warning focus:text-warning"
                        >
                          <Ban className="h-3.5 w-3.5 mr-2" />
                          Suspend SIM
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => setConfirmDialog({ violationId: v.id, action: 'escalate', label: 'Escalate' })}
                          className="text-[12px] text-danger focus:text-danger"
                        >
                          <ArrowUpRight className="h-3.5 w-3.5 mr-2" />
                          Escalate
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => navigate(`/policies/${v.policy_id}?rule=${v.rule_index}`)}
                          className="text-[12px]"
                        >
                          <AlertTriangle className="h-3.5 w-3.5 mr-2" />
                          Review Policy
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          onClick={() => dismiss.mutate({ id: v.id, note: 'Dismissed from related violations tab' })}
                          className="text-[12px] text-text-secondary"
                        >
                          <CheckCircle2 className="h-3.5 w-3.5 mr-2" />
                          Dismiss
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Dialog open={!!confirmDialog} onOpenChange={(open) => { if (!open) { setConfirmDialog(null); setReason('') } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px] font-semibold">
              Confirm {confirmDialog?.label}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <p className="text-[13px] text-text-secondary">
              {confirmDialog?.action === 'suspend_sim'
                ? 'This will immediately suspend the SIM and acknowledge the violation. This action can be reversed by resuming the SIM.'
                : 'This will escalate the violation and create a high-severity notification for on-call recipients.'}
            </p>
            <div>
              <label className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium block mb-1.5">
                Reason
              </label>
              <Textarea
                rows={3}
                placeholder="Describe the reason for this action..."
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                className="resize-none"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setConfirmDialog(null); setReason('') }}>
              Cancel
            </Button>
            <Button
              variant={confirmDialog?.action === 'suspend_sim' ? 'destructive' : 'default'}
              onClick={handleConfirm}
              disabled={remediate.isPending}
            >
              {remediate.isPending ? 'Processing...' : confirmDialog?.label}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
