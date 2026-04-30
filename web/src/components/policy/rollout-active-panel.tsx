import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  ArrowUpRight,
  CheckCircle2,
  ChevronRight,
  Circle,
  ExternalLink,
  Octagon,
  Play,
  RefreshCw,
  RotateCcw,
  XCircle,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { timeAgo } from '@/lib/format'
import type { PolicyRollout, RolloutStage } from '@/types/policy'

export interface RolloutCoaCounts {
  pending: number
  queued: number
  acked: number
  failed: number
  no_session: number
  skipped: number
}

export interface RolloutActivePanelProps {
  rollout: PolicyRollout
  onAdvance: () => void
  onRollback: () => void
  onAbort: () => void
  onRetryFailed?: () => void
  onOpenExpanded?: () => void
  coaCounts?: RolloutCoaCounts
}

type StateVariant = 'default' | 'success' | 'danger' | 'warning' | 'secondary'

function stateVariant(state: string): StateVariant {
  switch (state) {
    case 'completed':
      return 'success'
    case 'rolled_back':
      return 'danger'
    case 'aborted':
      return 'warning'
    case 'in_progress':
      return 'default'
    default:
      return 'secondary'
  }
}

function progressBarClass(state: string): string {
  switch (state) {
    case 'completed':
      return 'bg-success'
    case 'aborted':
      return 'bg-warning'
    case 'rolled_back':
      return 'bg-danger'
    case 'in_progress':
    default:
      return 'bg-gradient-to-r from-accent to-accent/70'
  }
}

function shortId(id: string): string {
  if (!id) return ''
  if (id.length <= 12) return id
  return `${id.slice(0, 8)}…${id.slice(-4)}`
}

function parseStages(raw: PolicyRollout['stages']): RolloutStage[] {
  if (Array.isArray(raw)) return raw
  if (typeof raw === 'string') {
    try {
      const parsed = JSON.parse(raw) as RolloutStage[]
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }
  return []
}

function formatEta(stage: RolloutStage, stageStartedAt: string | undefined): string {
  if (!stageStartedAt) return '—'
  const migrated = stage.migrated ?? 0
  const total = stage.sim_count ?? 0
  // FIX-232 Gate F-U1 — when migrated has caught up to total but the stage is
  // still 'in_progress' (post-batch reconciliation), surface "Finalizing…"
  // instead of an em-dash so the operator knows the stage is wrapping up.
  if (total > 0 && migrated >= total && stage.status === 'in_progress') {
    return 'Finalizing…'
  }
  if (migrated <= 0 || total <= 0 || migrated >= total) return '—'
  const elapsedMs = Date.now() - new Date(stageStartedAt).getTime()
  if (!Number.isFinite(elapsedMs) || elapsedMs <= 0) return '—'
  const ratePerMs = migrated / elapsedMs
  if (!Number.isFinite(ratePerMs) || ratePerMs <= 0) return '—'
  const remaining = total - migrated
  const etaMs = remaining / ratePerMs
  const etaMin = Math.round(etaMs / 60_000)
  if (etaMin <= 0) return '<1m'
  if (etaMin < 60) return `~${etaMin}m`
  const hours = Math.floor(etaMin / 60)
  const mins = etaMin % 60
  return mins > 0 ? `~${hours}h ${mins}m` : `~${hours}h`
}

function StageCard({ stage, isActive }: { stage: RolloutStage; isActive: boolean }) {
  const status = stage.status
  const isCompleted = status === 'completed'
  const isFailed = status === 'failed'
  const isPending = !isActive && !isCompleted && !isFailed

  const containerCls = isFailed
    ? 'border-danger/30 bg-danger-dim/30'
    : isActive
      ? 'border-accent bg-accent-dim'
      : isCompleted
        ? 'border-success/20 bg-success-dim/30'
        : 'border-border-subtle bg-bg-surface'

  const pctTone = isFailed
    ? 'text-danger'
    : isActive
      ? 'text-accent'
      : isCompleted
        ? 'text-success'
        : 'text-text-tertiary'

  const migrated = stage.migrated ?? 0
  const total = stage.sim_count ?? 0

  return (
    <div
      className={cn(
        'flex-1 min-w-0 rounded-[var(--radius-sm)] border p-3 text-center transition-colors',
        containerCls,
      )}
    >
      <div className="flex items-center justify-center gap-1.5">
        {isCompleted && <CheckCircle2 className="h-3.5 w-3.5 text-success" aria-hidden="true" />}
        {isActive && <Play className="h-3.5 w-3.5 text-accent" aria-hidden="true" />}
        {isFailed && <XCircle className="h-3.5 w-3.5 text-danger" aria-hidden="true" />}
        {isPending && <Circle className="h-3.5 w-3.5 text-text-tertiary" aria-hidden="true" />}
        <span className={cn('text-sm font-semibold', pctTone)}>{stage.pct}%</span>
      </div>
      <div className="mt-1 font-mono text-[10px] text-text-tertiary">
        {migrated.toLocaleString()} / {total > 0 ? total.toLocaleString() : '?'} SIMs
      </div>
    </div>
  )
}

export function RolloutActivePanel({
  rollout,
  onAdvance,
  onRollback,
  onAbort,
  onRetryFailed,
  onOpenExpanded,
  coaCounts,
}: RolloutActivePanelProps) {
  const stages = useMemo(() => parseStages(rollout.stages), [rollout.stages])
  const currentStage = stages[rollout.current_stage]
  const failedStage = stages.find((s) => s.status === 'failed')

  const isStaged = !(stages.length === 1 && stages[0]?.pct === 100)
  const strategyLabel = isStaged ? 'Staged Canary' : 'Direct Assign (100%)'

  const progressPct = rollout.total_sims > 0
    ? Math.min(100, Math.round((rollout.migrated_sims / rollout.total_sims) * 100))
    : 0

  const isActiveState = rollout.state === 'pending' || rollout.state === 'in_progress'
  const canAdvance =
    isStaged &&
    rollout.state === 'in_progress' &&
    currentStage?.status === 'completed' &&
    rollout.current_stage < stages.length - 1

  const stateLabel = rollout.state.replace(/_/g, ' ').toUpperCase()
  const idChip = shortId(rollout.id)
  const startedRel = rollout.started_at ? timeAgo(rollout.started_at) : null
  const eta = rollout.state === 'in_progress' && currentStage
    ? formatEta(currentStage, rollout.started_at)
    : '—'

  return (
    <Card
      role="region"
      aria-label="Active rollout panel"
      className="p-4 space-y-4"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          <Badge variant={stateVariant(rollout.state)}>{stateLabel}</Badge>
          <span
            className="font-mono text-xs text-text-secondary"
            title={rollout.id}
          >
            {idChip}
          </span>
          {startedRel && (
            <span className="text-xs text-text-tertiary">started {startedRel}</span>
          )}
        </div>
        {onOpenExpanded && (
          <Button
            variant="ghost"
            size="xs"
            onClick={onOpenExpanded}
            aria-label="Open expanded rollout view"
          >
            Open expanded view
            <ArrowUpRight className="h-3 w-3" aria-hidden="true" />
          </Button>
        )}
      </div>

      <div className="flex items-center justify-between gap-3">
        <div className="flex flex-col">
          <span className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">
            Strategy
          </span>
          <span className="text-sm font-semibold text-text-primary">{strategyLabel}</span>
        </div>
        <div className="flex flex-col items-end">
          <span className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">
            Migrated
          </span>
          <span className="font-mono text-xs text-text-primary">
            {rollout.migrated_sims.toLocaleString()} / {rollout.total_sims.toLocaleString()}
          </span>
        </div>
      </div>

      <div className="space-y-2">
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          Stages
        </span>
        <div className="flex items-center gap-2">
          {stages.map((stage, i) => (
            <div key={`${stage.pct}-${i}`} className="flex items-center gap-2 flex-1 min-w-0">
              <StageCard stage={stage} isActive={i === rollout.current_stage && isActiveState} />
              {i < stages.length - 1 && (
                <ChevronRight className="h-3 w-3 text-text-tertiary shrink-0" aria-hidden="true" />
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="text-text-secondary">Overall progress</span>
          <span className="font-mono text-xs font-semibold text-text-primary">{progressPct}%</span>
        </div>
        <div
          role="progressbar"
          aria-label="Rollout overall progress"
          aria-valuenow={progressPct}
          aria-valuemin={0}
          aria-valuemax={100}
          className="h-2.5 rounded-[var(--radius-sm)] bg-bg-hover overflow-hidden"
        >
          <div
            className={cn('h-full rounded-[var(--radius-sm)] transition-all duration-500', progressBarClass(rollout.state))}
            style={{ width: `${progressPct}%` }}
          />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col p-3 rounded-[var(--radius-sm)] border border-border-subtle bg-bg-surface">
          <span className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">
            CoA Status
          </span>
          {coaCounts ? (
            <ul className="mt-1 space-y-0.5" aria-label="CoA status breakdown">
              {(
                [
                  { key: 'pending',    label: 'pending',    count: coaCounts.pending,    cls: 'text-accent',        always: false },
                  { key: 'queued',     label: 'queued',     count: coaCounts.queued,     cls: 'text-accent',        always: false },
                  { key: 'acked',      label: 'acked',      count: coaCounts.acked,      cls: 'text-success',       always: true  },
                  { key: 'failed',     label: 'failed',     count: coaCounts.failed,     cls: 'text-danger',        always: true  },
                  { key: 'no_session', label: 'no session', count: coaCounts.no_session, cls: 'text-text-tertiary', always: false },
                  { key: 'skipped',    label: 'skipped',    count: coaCounts.skipped,    cls: 'text-text-tertiary', always: false },
                ] as const
              )
                .filter((seg) => seg.always || seg.count > 0)
                .map((seg) => (
                  <li key={seg.key} className="flex items-center justify-between gap-2">
                    <span className="font-mono text-[10px] text-text-secondary">{seg.label}</span>
                    <span className={cn('font-mono text-[10px] font-semibold', seg.cls)}>
                      {seg.count.toLocaleString()}
                    </span>
                  </li>
                ))}
            </ul>
          ) : (
            <span className="font-mono text-xs text-text-tertiary">—</span>
          )}
        </div>
        <div className="flex flex-col p-3 rounded-[var(--radius-sm)] border border-border-subtle bg-bg-surface">
          <span className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">
            ETA (current stage)
          </span>
          <span className="font-mono text-xs text-text-primary">{eta}</span>
        </div>
      </div>

      {failedStage && (
        <div
          role="alert"
          className="flex items-start gap-2 p-3 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim/30"
        >
          <AlertTriangle className="h-4 w-4 text-danger shrink-0 mt-0.5" aria-hidden="true" />
          <div className="flex-1 min-w-0 space-y-1">
            <p className="text-xs font-semibold text-danger">
              Stage {failedStage.pct}% failed
            </p>
            <p className="text-xs text-text-secondary">
              {(failedStage.migrated ?? 0).toLocaleString()} of{' '}
              {(failedStage.sim_count ?? 0).toLocaleString()} SIMs migrated before failure.
            </p>
          </div>
          {onRetryFailed && (
            <Button
              variant="outline"
              size="xs"
              onClick={onRetryFailed}
              className="border-danger/30 text-danger hover:bg-danger-dim"
              aria-label={`Retry failed stage ${failedStage.pct}%`}
            >
              <RefreshCw className="h-3 w-3" aria-hidden="true" />
              Retry failed
            </Button>
          )}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 pt-1">
        {canAdvance && (
          <Button
            size="sm"
            onClick={onAdvance}
            aria-label={`Advance rollout ${idChip} to next stage`}
          >
            <ChevronRight className="h-3 w-3" aria-hidden="true" />
            Advance Stage
          </Button>
        )}
        {isActiveState && (
          <Button
            size="sm"
            variant="outline"
            onClick={onRollback}
            className="border-danger/30 text-danger hover:bg-danger-dim"
            aria-label={`Rollback rollout ${idChip} — reverts migrated SIMs to previous version`}
          >
            <RotateCcw className="h-3 w-3" aria-hidden="true" />
            Rollback
          </Button>
        )}
        {isActiveState && (
          <Button
            size="sm"
            variant="outline"
            onClick={onAbort}
            className="border-warning/30 text-warning hover:bg-warning-dim"
            aria-label={`Abort rollout ${idChip} — stops migration but does not revert assignments`}
          >
            <Octagon className="h-3 w-3" aria-hidden="true" />
            Abort
          </Button>
        )}
        <Link
          to={currentStage?.pct ? `/sims?rollout_id=${rollout.id}&rollout_stage_pct=${currentStage.pct}` : `/sims?rollout_id=${rollout.id}`}
          className={cn(
            buttonVariants({ variant: 'outline', size: 'sm' }),
            'no-underline',
          )}
          aria-label={`View SIM cohort for rollout ${idChip}`}
        >
          <ExternalLink className="h-3 w-3" aria-hidden="true" />
          View cohort
        </Link>
      </div>

      <span className="sr-only" aria-live="polite">
        {rollout.state === 'in_progress'
          ? `Rollout in progress: ${progressPct} percent migrated.`
          : `Rollout ${stateLabel.toLowerCase()}.`}
      </span>
    </Card>
  )
}
