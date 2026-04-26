import { useState, useEffect } from 'react'
import {
  Play,
  ChevronRight,
  Loader2,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Octagon,
  ArrowUpRight,
} from 'lucide-react'
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
import { RolloutActivePanel } from '@/components/policy/rollout-active-panel'
import { RolloutExpandedSlidePanel } from '@/components/policy/rollout-expanded-slide-panel'
import {
  useRollout,
  useStartRollout,
  useAdvanceRollout,
  useRollbackRollout,
  useAbortRollout,
} from '@/hooks/use-policies'
import { wsClient, type WSStatus } from '@/lib/ws'
import type { PolicyRollout, PolicyVersion } from '@/types/policy'

interface RolloutTabProps {
  policyId: string
  currentVersion?: PolicyVersion
  rolloutId?: string
}

const STAGED_STAGES = [1, 10, 100]
const DIRECT_STAGES = [100]

type ConfirmAction = 'start' | 'advance' | 'rollback' | 'abort' | null

const ACTIVE_STATES = ['pending', 'in_progress'] as const
const TERMINAL_STATES = ['completed', 'rolled_back', 'aborted'] as const

function isActiveState(state: string): boolean {
  return (ACTIVE_STATES as readonly string[]).includes(state)
}

function isTerminalState(state: string): boolean {
  return (TERMINAL_STATES as readonly string[]).includes(state)
}

function terminalVariant(state: string): 'success' | 'danger' | 'warning' | 'secondary' {
  switch (state) {
    case 'completed':
      return 'success'
    case 'rolled_back':
      return 'danger'
    case 'aborted':
      return 'warning'
    default:
      return 'secondary'
  }
}

function terminalSummaryText(rollout: PolicyRollout): string {
  const migrated = rollout.migrated_sims.toLocaleString()
  const total = rollout.total_sims.toLocaleString()
  switch (rollout.state) {
    case 'completed':
      return `Rollout completed — ${migrated} of ${total} SIMs migrated.`
    case 'rolled_back':
      return `Rollout rolled back — ${migrated} of ${total} SIMs reverted to previous version.`
    case 'aborted':
      return `Rollout aborted — ${migrated} of ${total} SIMs remain on the new policy. CoA was not fired.`
    default:
      return `Rollout ${rollout.state.replace(/_/g, ' ')}.`
  }
}

interface TerminalSummaryBannerProps {
  rollout: PolicyRollout
  onViewSummary: () => void
}

function TerminalSummaryBanner({ rollout, onViewSummary }: TerminalSummaryBannerProps) {
  const variant = terminalVariant(rollout.state)
  const Icon =
    rollout.state === 'completed'
      ? CheckCircle2
      : rollout.state === 'rolled_back'
        ? XCircle
        : Octagon
  const tone =
    variant === 'success'
      ? 'border-success/30 bg-success-dim/30'
      : variant === 'danger'
        ? 'border-danger/30 bg-danger-dim/30'
        : 'border-warning/30 bg-warning-dim/40'
  const iconTone =
    variant === 'success'
      ? 'text-success'
      : variant === 'danger'
        ? 'text-danger'
        : 'text-warning'

  // FIX-232 Gate F-A6 — surface the terminal-state timestamp for compliance
  // with SCR mockup. Pick the field matching the rollout state.
  const terminalTimestamp =
    rollout.state === 'completed'
      ? rollout.completed_at
      : rollout.state === 'rolled_back'
        ? rollout.rolled_back_at
        : rollout.state === 'aborted'
          ? rollout.aborted_at
          : undefined
  const formattedTs = terminalTimestamp
    ? new Date(terminalTimestamp).toLocaleString()
    : null

  return (
    <div
      role="status"
      className={`flex items-start gap-3 rounded-[var(--radius-sm)] border p-3 ${tone}`}
    >
      <Icon className={`h-4 w-4 shrink-0 mt-0.5 ${iconTone}`} aria-hidden="true" />
      <div className="flex-1 min-w-0 space-y-1">
        <div className="flex items-center gap-2">
          <Badge variant={variant}>{rollout.state.replace(/_/g, ' ').toUpperCase()}</Badge>
          <span className="font-mono text-[11px] text-text-tertiary" title={rollout.id}>
            {rollout.id.slice(0, 8)}…{rollout.id.slice(-4)}
          </span>
          {formattedTs && terminalTimestamp && (
            <time
              dateTime={terminalTimestamp}
              className="text-[11px] text-text-tertiary"
              title={terminalTimestamp}
            >
              {formattedTs}
            </time>
          )}
        </div>
        <p className="text-xs text-text-secondary">{terminalSummaryText(rollout)}</p>
      </div>
      <Button variant="ghost" size="xs" onClick={onViewSummary} aria-label="View rollout summary">
        View summary
        <ArrowUpRight className="h-3 w-3" aria-hidden="true" />
      </Button>
    </div>
  )
}

interface SelectionCardsProps {
  mode: 'direct' | 'staged'
  setMode: (m: 'direct' | 'staged') => void
  currentVersion: PolicyVersion
  canStartRollout: boolean
  onStart: () => void
}

function SelectionCards({
  mode,
  setMode,
  currentVersion,
  canStartRollout,
  onStart,
}: SelectionCardsProps) {
  return (
    <div className="space-y-4">
      <div role="radiogroup" aria-label="Rollout mode" className="grid grid-cols-2 gap-3">
        <div
          role="radio"
          aria-checked={mode === 'direct'}
          tabIndex={0}
          onClick={() => setMode('direct')}
          onKeyDown={(e) => {
            if (e.key === ' ' || e.key === 'Enter') {
              e.preventDefault()
              setMode('direct')
            }
          }}
          className={`cursor-pointer text-left rounded-[var(--radius-md)] border p-4 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ${
            mode === 'direct'
              ? 'border-accent bg-accent/5'
              : 'border-border bg-bg-surface hover:border-text-tertiary'
          }`}
        >
          <div className="flex items-center justify-between mb-2">
            <h5 className="text-sm font-medium text-text-primary">Direct Assign</h5>
            <div
              aria-hidden="true"
              className={`h-3.5 w-3.5 rounded-full border flex items-center justify-center ${
                mode === 'direct' ? 'border-accent bg-accent' : 'border-border'
              }`}
            >
              {mode === 'direct' && <div className="h-1.5 w-1.5 rounded-full bg-bg-primary" />}
            </div>
          </div>
          <p className="text-xs text-text-secondary mb-3">
            Apply immediately to all matching SIMs at once (100%).
          </p>
          <div className="inline-block px-3 py-1 rounded-full border border-border bg-bg-elevated text-xs font-semibold text-text-primary">
            100%
          </div>
        </div>

        <div
          role="radio"
          aria-checked={mode === 'staged'}
          tabIndex={0}
          onClick={() => setMode('staged')}
          onKeyDown={(e) => {
            if (e.key === ' ' || e.key === 'Enter') {
              e.preventDefault()
              setMode('staged')
            }
          }}
          className={`cursor-pointer text-left rounded-[var(--radius-md)] border p-4 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ${
            mode === 'staged'
              ? 'border-accent bg-accent/5'
              : 'border-border bg-bg-surface hover:border-text-tertiary'
          }`}
        >
          <div className="flex items-center justify-between mb-2">
            <h5 className="text-sm font-medium text-text-primary">Staged Rollout</h5>
            <div
              aria-hidden="true"
              className={`h-3.5 w-3.5 rounded-full border flex items-center justify-center ${
                mode === 'staged' ? 'border-accent bg-accent' : 'border-border'
              }`}
            >
              {mode === 'staged' && <div className="h-1.5 w-1.5 rounded-full bg-bg-primary" />}
            </div>
          </div>
          <p className="text-xs text-text-secondary mb-3">
            Canary rollout: 1% → 10% → 100% with manual advancement.
          </p>
          <div className="flex items-center gap-1">
            {STAGED_STAGES.map((pct, i) => (
              <div key={pct} className="flex items-center gap-1">
                <div className="px-2 py-0.5 rounded-full border border-border bg-bg-elevated text-[10px] font-semibold text-text-primary">
                  {pct}%
                </div>
                {i < STAGED_STAGES.length - 1 && (
                  <ChevronRight className="h-2.5 w-2.5 text-text-tertiary" />
                )}
              </div>
            ))}
          </div>
        </div>
      </div>

      {canStartRollout && (
        <Button className="w-full gap-2" onClick={onStart}>
          <Play className="h-4 w-4" />
          {mode === 'direct' ? 'Assign to All Matching SIMs' : 'Start Staged Rollout'}
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
  )
}

export function RolloutTab({ policyId, currentVersion, rolloutId: initialRolloutId }: RolloutTabProps) {
  const [rolloutId, setRolloutId] = useState(initialRolloutId)
  const [confirmAction, setConfirmAction] = useState<ConfirmAction>(null)
  const [mode, setMode] = useState<'direct' | 'staged'>('direct')
  const [expandedOpen, setExpandedOpen] = useState(false)
  const [wsStatus, setWsStatus] = useState<WSStatus>(() => wsClient.getStatus())
  const [lastUpdateAt, setLastUpdateAt] = useState<string | undefined>(undefined)

  const { data: rollout, refetch: refetchRollout } = useRollout(rolloutId)
  const startMutation = useStartRollout(policyId)
  const advanceMutation = useAdvanceRollout()
  const rollbackMutation = useRollbackRollout()
  const abortMutation = useAbortRollout(rolloutId)

  useEffect(() => {
    const unsub = wsClient.on('policy.rollout_progress', (data: unknown) => {
      const typed = data as { rollout_id?: string }
      if (typed.rollout_id === rolloutId) {
        setLastUpdateAt(new Date().toISOString())
        refetchRollout()
      }
    })
    return unsub
  }, [rolloutId, refetchRollout])

  useEffect(() => {
    const unsub = wsClient.onStatus(setWsStatus)
    return unsub
  }, [])

  // Polling fallback when WS is disconnected and rollout is active.
  useEffect(() => {
    if (wsStatus !== 'disconnected') return
    if (!rolloutId) return
    if (!rollout || !isActiveState(rollout.state)) return

    const id = window.setInterval(() => {
      refetchRollout()
      setLastUpdateAt(new Date().toISOString())
    }, 5000)
    return () => window.clearInterval(id)
  }, [wsStatus, rolloutId, rollout, refetchRollout])

  const handleStart = async () => {
    if (!currentVersion) return
    try {
      const result = await startMutation.mutateAsync({
        versionId: currentVersion.id,
        stages: mode === 'direct' ? DIRECT_STAGES : STAGED_STAGES,
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

  const handleAbort = async () => {
    if (!rolloutId) return
    try {
      await abortMutation.mutateAsync({})
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

  const dialogTitle =
    confirmAction === 'start'
      ? 'Start Rollout'
      : confirmAction === 'advance'
        ? 'Advance to Next Stage'
        : confirmAction === 'rollback'
          ? 'Rollback'
          : confirmAction === 'abort'
            ? 'Abort Rollout'
            : ''

  const dialogDescription =
    confirmAction === 'start'
      ? mode === 'direct'
        ? `Apply v${currentVersion.version} to ALL matching SIMs immediately? This cannot be undone incrementally.`
        : `Start staged rollout for v${currentVersion.version}? This will begin migrating SIMs at 1%.`
      : confirmAction === 'advance'
        ? 'Advance to next stage. Current stage is complete. Continue?'
        : confirmAction === 'rollback'
          ? 'Rollback rollout? This will revert all migrated SIMs to the previous policy version and fire CoA. This action is destructive.'
          : confirmAction === 'abort'
            ? 'Abort rollout? Already-migrated SIMs WILL stay on the new policy. CoA will NOT fire. Use this when the rollout was started by mistake but the new policy is correct.'
            : ''

  const confirmHandler =
    confirmAction === 'start'
      ? handleStart
      : confirmAction === 'advance'
        ? handleAdvance
        : confirmAction === 'rollback'
          ? handleRollback
          : confirmAction === 'abort'
            ? handleAbort
            : () => {}

  const confirmLabel =
    confirmAction === 'start'
      ? 'Start'
      : confirmAction === 'advance'
        ? 'Advance'
        : confirmAction === 'rollback'
          ? 'Confirm Rollback'
          : confirmAction === 'abort'
            ? 'Confirm Abort'
            : ''

  const confirmVariant: 'default' | 'destructive' = confirmAction === 'rollback' ? 'destructive' : 'default'
  const confirmExtraClass =
    confirmAction === 'abort'
      ? 'border border-warning/30 bg-warning-dim text-warning hover:bg-warning-dim/70'
      : ''

  const isSubmitting =
    startMutation.isPending ||
    advanceMutation.isPending ||
    rollbackMutation.isPending ||
    abortMutation.isPending

  const showActivePanel = !!rollout && isActiveState(rollout.state)
  const showTerminalBanner = !!rollout && isTerminalState(rollout.state)

  return (
    <div className="p-4 space-y-4 overflow-y-auto h-full">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-medium text-text-secondary uppercase tracking-wider">Rollout</h4>
        {(showActivePanel || showTerminalBanner) && rollout && (
          <span
            className={`text-[10px] font-medium uppercase tracking-wider ${
              wsStatus === 'connected'
                ? 'text-success'
                : wsStatus === 'connecting'
                  ? 'text-warning'
                  : 'text-text-tertiary'
            }`}
            aria-live="polite"
          >
            {wsStatus === 'connected'
              ? 'WS connected'
              : wsStatus === 'connecting'
                ? 'WS connecting…'
                : 'WS disconnected · polling 5s'}
          </span>
        )}
      </div>

      {showActivePanel && rollout ? (
        <RolloutActivePanel
          rollout={rollout}
          onAdvance={() => setConfirmAction('advance')}
          onRollback={() => setConfirmAction('rollback')}
          onAbort={() => setConfirmAction('abort')}
          onOpenExpanded={() => setExpandedOpen(true)}
        />
      ) : showTerminalBanner && rollout ? (
        <>
          <TerminalSummaryBanner
            rollout={rollout}
            onViewSummary={() => setExpandedOpen(true)}
          />
          <SelectionCards
            mode={mode}
            setMode={setMode}
            currentVersion={currentVersion}
            canStartRollout={canStartRollout}
            onStart={() => setConfirmAction('start')}
          />
        </>
      ) : (
        <SelectionCards
          mode={mode}
          setMode={setMode}
          currentVersion={currentVersion}
          canStartRollout={canStartRollout}
          onStart={() => setConfirmAction('start')}
        />
      )}

      {rollout && (
        <RolloutExpandedSlidePanel
          open={expandedOpen}
          onClose={() => setExpandedOpen(false)}
          rollout={rollout}
          wsStatus={wsStatus}
          lastUpdateAt={lastUpdateAt}
        />
      )}

      <Dialog open={!!confirmAction} onOpenChange={() => setConfirmAction(null)}>
        <DialogContent onClose={() => setConfirmAction(null)}>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription>{dialogDescription}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>
              Cancel
            </Button>
            <Button
              variant={confirmVariant}
              onClick={confirmHandler}
              disabled={isSubmitting}
              className={`gap-2 ${confirmExtraClass}`}
            >
              {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {confirmLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
