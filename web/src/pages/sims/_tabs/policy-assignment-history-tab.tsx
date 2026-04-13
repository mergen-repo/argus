import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EntityLink } from '@/components/shared'
import { timeAgo } from '@/lib/format'
import type { ListResponse } from '@/types/sim'

interface PolicyAssignmentHistoryTabProps {
  simId: string
  currentPolicyVersionId?: string | null
}

interface StateHistoryEntry {
  id: string
  sim_id: string
  from_state: string | null
  to_state: string
  reason: string | null
  triggered_by: string
  user_id: string | null
  created_at: string
}

function useSimStateHistory(simId: string) {
  return useQuery({
    queryKey: ['sims', simId, 'history'],
    queryFn: async () => {
      const res = await api.get<ListResponse<StateHistoryEntry>>(`/sims/${simId}/history?limit=50`)
      return res.data.data
    },
    enabled: !!simId,
    staleTime: 30_000,
  })
}

function stateVariant(state: string): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state) {
    case 'active': return 'success'
    case 'suspended': return 'warning'
    case 'terminated': return 'danger'
    case 'ordered': case 'provisioned': return 'secondary'
    default: return 'default'
  }
}

export function PolicyAssignmentHistoryTab({ simId, currentPolicyVersionId }: PolicyAssignmentHistoryTabProps) {
  const { data: history, isLoading } = useSimStateHistory(simId)

  return (
    <div className="mt-4 space-y-4">
      {currentPolicyVersionId && (
        <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface p-3">
          <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary font-medium mb-1.5">Current Policy</p>
          <EntityLink entityType="policy_version" entityId={currentPolicyVersionId} />
        </div>
      )}

      <div>
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
          State History
        </p>
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-8 w-full" />)}
          </div>
        ) : !history?.length ? (
          <p className="text-xs text-text-tertiary text-center py-6">No state history yet.</p>
        ) : (
          <div className="space-y-1">
            {history.map((entry) => (
              <div key={entry.id} className="flex items-center gap-3 px-3 py-2 rounded-[var(--radius-sm)] hover:bg-bg-hover transition-colors text-xs">
                <span className="text-text-tertiary font-mono text-[11px] w-20 shrink-0" title={new Date(entry.created_at).toISOString()}>
                  {timeAgo(entry.created_at)}
                </span>
                <div className="flex items-center gap-1.5">
                  {entry.from_state && (
                    <>
                      <Badge variant={stateVariant(entry.from_state)} className="text-[10px]">{entry.from_state}</Badge>
                      <span className="text-text-tertiary">→</span>
                    </>
                  )}
                  <Badge variant={stateVariant(entry.to_state)} className="text-[10px]">{entry.to_state}</Badge>
                </div>
                <span className="text-text-tertiary">{entry.triggered_by}</span>
                {entry.reason && <span className="text-text-tertiary truncate ml-auto">{entry.reason}</span>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
