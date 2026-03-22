import { useState, useEffect } from 'react'
import { Play, RotateCcw, ChevronRight, Loader2, AlertCircle, CheckCircle2, XCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { useRollout, useStartRollout, useAdvanceRollout, useRollbackRollout } from '@/hooks/use-policies'
import { wsClient } from '@/lib/ws'
import type { PolicyRollout, PolicyVersion, RolloutStage } from '@/types/policy'

interface RolloutTabProps {
  policyId: string
  currentVersion?: PolicyVersion
  rolloutId?: string
}

const DEFAULT_STAGES = [1, 10, 100]

function RolloutProgress({ rollout }: { rollout: PolicyRollout }) {
  const stages: RolloutStage[] = typeof rollout.stages === 'string'
    ? JSON.parse(rollout.stages)
    : rollout.stages

  const progressPct = rollout.total_sims > 0
    ? Math.round((rollout.migrated_sims / rollout.total_sims) * 100)
    : 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Badge
            variant={
              rollout.state === 'completed' ? 'success'
                : rollout.state === 'rolled_back' ? 'danger'
                : rollout.state === 'in_progress' ? 'default'
                : 'secondary'
            }
          >
            {rollout.state.replace('_', ' ').toUpperCase()}
          </Badge>
        </div>
        <span className="text-xs font-mono text-text-tertiary">
          {rollout.migrated_sims.toLocaleString()} / {rollout.total_sims.toLocaleString()}
        </span>
      </div>

      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="text-text-secondary">Progress</span>
          <span className="font-mono text-text-primary font-semibold">{progressPct}%</span>
        </div>
        <div className="h-3 rounded-full bg-bg-hover overflow-hidden">
          <div
            className="h-full rounded-full bg-gradient-to-r from-accent to-accent/70 transition-all duration-500"
            style={{ width: `${progressPct}%` }}
          />
        </div>
      </div>

      <div className="space-y-2">
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">Stages</span>
        <div className="flex items-center gap-1">
          {stages.map((stage, i) => {
            const isActive = i === rollout.current_stage
            const isCompleted = stage.status === 'completed'
            const isPending = stage.status === 'pending'

            return (
              <div key={i} className="flex items-center gap-1 flex-1">
                <div
                  className={`flex-1 rounded-[var(--radius-sm)] p-2 border text-center ${
                    isActive
                      ? 'border-accent/30 bg-accent-dim'
                      : isCompleted
                      ? 'border-success/20 bg-success-dim/30'
                      : 'border-border-subtle bg-bg-surface'
                  }`}
                >
                  <div className="flex items-center justify-center gap-1">
                    {isCompleted && <CheckCircle2 className="h-3 w-3 text-success" />}
                    {isActive && <Play className="h-3 w-3 text-accent" />}
                    <span className={`text-sm font-semibold ${
                      isActive ? 'text-accent' : isCompleted ? 'text-success' : 'text-text-tertiary'
                    }`}>
                      {stage.pct}%
                    </span>
                  </div>
                  {stage.migrated != null && (
                    <div className="text-[10px] font-mono text-text-tertiary mt-0.5">
                      {stage.migrated}/{stage.sim_count ?? '?'}
                    </div>
                  )}
                </div>
                {i < stages.length - 1 && (
                  <ChevronRight className="h-3 w-3 text-text-tertiary shrink-0" />
                )}
              </div>
            )
          })}
        </div>
      </div>

      {rollout.started_at && (
        <div className="text-xs text-text-tertiary">
          Started: {new Date(rollout.started_at).toLocaleString()}
        </div>
      )}
    </div>
  )
}

export function RolloutTab({ policyId, currentVersion, rolloutId: initialRolloutId }: RolloutTabProps) {
  const [rolloutId, setRolloutId] = useState(initialRolloutId)
  const [confirmAction, setConfirmAction] = useState<'start' | 'advance' | 'rollback' | null>(null)

  const { data: rollout, refetch: refetchRollout } = useRollout(rolloutId)
  const startMutation = useStartRollout(policyId)
  const advanceMutation = useAdvanceRollout()
  const rollbackMutation = useRollbackRollout()

  useEffect(() => {
    const unsub = wsClient.on('policy.rollout_progress', (data: unknown) => {
      const typed = data as { rollout_id?: string }
      if (typed.rollout_id === rolloutId) {
        refetchRollout()
      }
    })
    return unsub
  }, [rolloutId, refetchRollout])

  const handleStart = async () => {
    if (!currentVersion) return
    try {
      const result = await startMutation.mutateAsync({
        versionId: currentVersion.id,
        stages: DEFAULT_STAGES,
      })
      setRolloutId(result.id)
      setConfirmAction(null)
    } catch {
      // handled by api interceptor
    }
  }

  const handleAdvance = async () => {
    if (!rolloutId) return
    try {
      await advanceMutation.mutateAsync(rolloutId)
      setConfirmAction(null)
      refetchRollout()
    } catch {
      // handled by api interceptor
    }
  }

  const handleRollback = async () => {
    if (!rolloutId) return
    try {
      await rollbackMutation.mutateAsync(rolloutId)
      setConfirmAction(null)
      refetchRollout()
    } catch {
      // handled by api interceptor
    }
  }

  if (!currentVersion) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-text-tertiary">
        <AlertCircle className="h-6 w-6 mb-3 opacity-50" />
        <span className="text-sm">Select a draft version to start rollout</span>
      </div>
    )
  }

  const canStartRollout = currentVersion.state === 'draft' && !rollout
  const canAdvance = rollout?.state === 'in_progress'
  const canRollback = rollout?.state === 'in_progress'

  return (
    <div className="p-4 space-y-4 overflow-y-auto h-full">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-medium text-text-secondary uppercase tracking-wider">Rollout</h4>
      </div>

      {rollout ? (
        <>
          <RolloutProgress rollout={rollout} />
          <div className="flex gap-2">
            {canAdvance && (
              <Button size="sm" className="gap-1.5 text-xs flex-1" onClick={() => setConfirmAction('advance')}>
                <ChevronRight className="h-3 w-3" />
                Advance Stage
              </Button>
            )}
            {canRollback && (
              <Button
                size="sm"
                variant="outline"
                className="gap-1.5 text-xs border-danger/30 text-danger hover:bg-danger-dim flex-1"
                onClick={() => setConfirmAction('rollback')}
              >
                <RotateCcw className="h-3 w-3" />
                Rollback
              </Button>
            )}
          </div>
        </>
      ) : (
        <div className="space-y-4">
          <div className="rounded-[var(--radius-sm)] border border-border p-4 bg-bg-surface">
            <h5 className="text-sm font-medium text-text-primary mb-2">Staged Rollout</h5>
            <p className="text-xs text-text-secondary mb-3">
              Roll out this policy version gradually: 1% then 10% then 100% of affected SIMs.
            </p>
            <div className="flex items-center gap-2">
              {DEFAULT_STAGES.map((pct, i) => (
                <div key={pct} className="flex items-center gap-1">
                  <div className="px-3 py-1 rounded-full border border-border bg-bg-elevated text-xs font-semibold text-text-primary">
                    {pct}%
                  </div>
                  {i < DEFAULT_STAGES.length - 1 && (
                    <ChevronRight className="h-3 w-3 text-text-tertiary" />
                  )}
                </div>
              ))}
            </div>
          </div>

          {canStartRollout && (
            <Button className="w-full gap-2" onClick={() => setConfirmAction('start')}>
              <Play className="h-4 w-4" />
              Start Rollout
            </Button>
          )}

          {currentVersion.state !== 'draft' && (
            <div className="flex items-center gap-2 p-3 rounded-[var(--radius-sm)] bg-warning-dim border border-warning/20">
              <AlertCircle className="h-4 w-4 text-warning shrink-0" />
              <span className="text-xs text-warning">
                Only draft versions can be rolled out. Create a new version first.
              </span>
            </div>
          )}
        </div>
      )}

      <Dialog open={!!confirmAction} onOpenChange={() => setConfirmAction(null)}>
        <DialogContent onClose={() => setConfirmAction(null)}>
          <DialogHeader>
            <DialogTitle>
              {confirmAction === 'start' && 'Start Rollout'}
              {confirmAction === 'advance' && 'Advance to Next Stage'}
              {confirmAction === 'rollback' && 'Rollback'}
            </DialogTitle>
            <DialogDescription>
              {confirmAction === 'start' && `Start staged rollout for v${currentVersion.version}? This will begin migrating SIMs at 1%.`}
              {confirmAction === 'advance' && 'Advance to the next rollout stage? More SIMs will be migrated.'}
              {confirmAction === 'rollback' && 'Rollback this rollout? All migrated SIMs will revert to the previous policy version.'}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>Cancel</Button>
            <Button
              variant={confirmAction === 'rollback' ? 'destructive' : 'default'}
              onClick={
                confirmAction === 'start' ? handleStart
                : confirmAction === 'advance' ? handleAdvance
                : handleRollback
              }
              disabled={startMutation.isPending || advanceMutation.isPending || rollbackMutation.isPending}
              className="gap-2"
            >
              {(startMutation.isPending || advanceMutation.isPending || rollbackMutation.isPending) && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              {confirmAction === 'start' && 'Start'}
              {confirmAction === 'advance' && 'Advance'}
              {confirmAction === 'rollback' && 'Rollback'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
