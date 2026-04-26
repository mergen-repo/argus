import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  CheckCircle2,
  Circle,
  ExternalLink,
  Play,
  Wifi,
  WifiOff,
  XCircle,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { SlidePanel } from '@/components/ui/slide-panel'
import { cn } from '@/lib/utils'
import { timeAgo } from '@/lib/format'
import type { PolicyRollout, RolloutStage } from '@/types/policy'

export interface RolloutExpandedSlidePanelProps {
  open: boolean
  onClose: () => void
  rollout: PolicyRollout
  wsStatus?: 'connected' | 'connecting' | 'disconnected'
  lastUpdateAt?: string
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

interface DrillDownLinkProps {
  to: string
  label: string
  description: string
}

function DrillDownLink({ to, label, description }: DrillDownLinkProps) {
  return (
    <Link
      to={to}
      className="group flex items-center justify-between gap-3 rounded-[var(--radius-sm)] border border-border bg-bg-surface p-3 transition-colors hover:border-accent/40 hover:bg-bg-hover"
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <ExternalLink className="h-3.5 w-3.5 text-accent" aria-hidden="true" />
          <span className="text-sm font-medium text-text-primary group-hover:text-accent">
            {label}
          </span>
        </div>
        <p className="mt-1 text-xs text-text-tertiary">{description}</p>
      </div>
    </Link>
  )
}

interface StageRowProps {
  stage: RolloutStage
  index: number
  isCurrent: boolean
}

function StageRow({ stage, index, isCurrent }: StageRowProps) {
  const status = stage.status
  const isCompleted = status === 'completed'
  const isFailed = status === 'failed'
  const isPending = !isCurrent && !isCompleted && !isFailed

  const Icon = isFailed
    ? XCircle
    : isCompleted
      ? CheckCircle2
      : isCurrent
        ? Play
        : Circle

  const iconCls = isFailed
    ? 'text-danger'
    : isCompleted
      ? 'text-success'
      : isCurrent
        ? 'text-accent'
        : 'text-text-tertiary'

  const containerCls = isFailed
    ? 'border-danger/30 bg-danger-dim/30'
    : isCurrent
      ? 'border-accent/40 bg-accent-dim'
      : isCompleted
        ? 'border-success/20 bg-success-dim/30'
        : 'border-border-subtle bg-bg-surface'

  const migrated = stage.migrated ?? 0
  const total = stage.sim_count ?? 0

  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-[var(--radius-sm)] border p-3',
        containerCls,
      )}
    >
      <Icon className={cn('h-4 w-4 shrink-0', iconCls)} aria-hidden="true" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-text-primary">
            Stage {index + 1} · {stage.pct}%
          </span>
          {isPending ? (
            <span className="text-[10px] uppercase tracking-wider text-text-tertiary">
              Pending
            </span>
          ) : (
            <span
              className={cn(
                'text-[10px] uppercase tracking-wider',
                isFailed ? 'text-danger' : isCurrent ? 'text-accent' : 'text-success',
              )}
            >
              {status.replace(/_/g, ' ')}
            </span>
          )}
        </div>
        <div className="mt-0.5 font-mono text-[11px] text-text-tertiary">
          {migrated.toLocaleString()} / {total > 0 ? total.toLocaleString() : '?'} SIMs
        </div>
      </div>
    </div>
  )
}

export function RolloutExpandedSlidePanel({
  open,
  onClose,
  rollout,
  wsStatus,
  lastUpdateAt,
}: RolloutExpandedSlidePanelProps) {
  const stages = useMemo(() => parseStages(rollout.stages), [rollout.stages])

  const isStaged = !(stages.length === 1 && stages[0]?.pct === 100)
  const strategyLabel = isStaged ? 'Staged Canary' : 'Direct Assign (100%)'
  const stateLabel = rollout.state.replace(/_/g, ' ').toUpperCase()
  const idChip = shortId(rollout.id)
  const progressPct = rollout.total_sims > 0
    ? Math.min(100, Math.round((rollout.migrated_sims / rollout.total_sims) * 100))
    : 0

  const failedStage = stages.find((s) => s.status === 'failed')

  const isActiveState = rollout.state === 'pending' || rollout.state === 'in_progress'
  const wsLabel =
    wsStatus === 'connected'
      ? 'WS connected'
      : wsStatus === 'connecting'
        ? 'WS connecting…'
        : 'WS disconnected · polling every 5s'
  const showPollingNote = wsStatus === 'disconnected' && isActiveState
  const lastUpdateRel = lastUpdateAt ? timeAgo(lastUpdateAt) : null

  const auditLink = `/audit?entity_id=${rollout.id}&action_prefix=policy_rollout`

  return (
    <SlidePanel
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
      title={`Rollout ${idChip}`}
      description="Detailed rollout information and drill-downs"
      width="lg"
    >
      <div className="space-y-5">
        <section
          aria-label="Rollout summary"
          className="space-y-3 rounded-[var(--radius-md)] border border-border bg-bg-surface p-4"
        >
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={stateVariant(rollout.state)}>{stateLabel}</Badge>
            <span
              className="font-mono text-xs text-text-secondary"
              title={rollout.id}
            >
              {idChip}
            </span>
            {rollout.started_at && (
              <span className="text-xs text-text-tertiary">
                started {timeAgo(rollout.started_at)}
              </span>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col">
              <span className="text-[10px] font-medium uppercase tracking-wider text-text-tertiary">
                Strategy
              </span>
              <span className="text-sm font-semibold text-text-primary">
                {strategyLabel}
              </span>
            </div>
            <div className="flex flex-col items-end">
              <span className="text-[10px] font-medium uppercase tracking-wider text-text-tertiary">
                Migrated
              </span>
              <span className="font-mono text-xs text-text-primary">
                {rollout.migrated_sims.toLocaleString()} /{' '}
                {rollout.total_sims.toLocaleString()}
              </span>
            </div>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between text-xs">
              <span className="text-text-secondary">Overall progress</span>
              <span className="font-mono text-xs font-semibold text-text-primary">
                {progressPct}%
              </span>
            </div>
            <div
              role="progressbar"
              aria-label="Rollout overall progress"
              aria-valuenow={progressPct}
              aria-valuemin={0}
              aria-valuemax={100}
              className="h-2 overflow-hidden rounded-[var(--radius-sm)] bg-bg-hover"
            >
              <div
                className={cn(
                  'h-full rounded-[var(--radius-sm)] transition-all duration-500',
                  rollout.state === 'completed'
                    ? 'bg-success'
                    : rollout.state === 'rolled_back'
                      ? 'bg-danger'
                      : rollout.state === 'aborted'
                        ? 'bg-warning'
                        : 'bg-gradient-to-r from-accent to-accent/70',
                )}
                style={{ width: `${progressPct}%` }}
              />
            </div>
          </div>
        </section>

        <section aria-label="Stages" className="space-y-2">
          <h3 className="text-xs font-medium uppercase tracking-wider text-text-secondary">
            Stages
          </h3>
          <div className="space-y-2">
            {stages.length === 0 ? (
              <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-surface p-3 text-xs text-text-tertiary">
                No stages defined.
              </div>
            ) : (
              stages.map((stage, i) => (
                <StageRow
                  key={`${stage.pct}-${i}`}
                  stage={stage}
                  index={i}
                  isCurrent={i === rollout.current_stage && isActiveState}
                />
              ))
            )}
          </div>
        </section>

        {failedStage && (
          <section
            role="alert"
            aria-label="Errors"
            className="flex items-start gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim/30 p-3"
          >
            <AlertTriangle
              className="mt-0.5 h-4 w-4 shrink-0 text-danger"
              aria-hidden="true"
            />
            <div className="min-w-0 flex-1 space-y-1">
              <p className="text-xs font-semibold text-danger">
                Stage {failedStage.pct}% failed
              </p>
              <p className="text-xs text-text-secondary">
                {(failedStage.migrated ?? 0).toLocaleString()} of{' '}
                {(failedStage.sim_count ?? 0).toLocaleString()} SIMs migrated before
                failure.
              </p>
            </div>
          </section>
        )}

        <section aria-label="Drill-downs" className="space-y-2">
          <h3 className="text-xs font-medium uppercase tracking-wider text-text-secondary">
            Drill-downs
          </h3>
          <div className="space-y-2">
            <DrillDownLink
              to={`/sims?rollout_id=${rollout.id}`}
              label="View Migrated SIMs"
              description="SIMs included in this rollout"
            />
            <DrillDownLink
              to={`/cdr?rollout_id=${rollout.id}`}
              label="CDR Explorer"
              description="Call detail records during rollout window"
            />
            <DrillDownLink
              to={`/sessions?rollout_id=${rollout.id}`}
              label="Sessions"
              description="Active and historical sessions for migrated SIMs"
            />
            <DrillDownLink
              to={auditLink}
              label="Audit Log"
              description="State transitions and operator actions"
            />
          </div>
        </section>

        <section
          aria-label="Connection status"
          className="flex items-center justify-between gap-2 rounded-[var(--radius-sm)] border border-border-subtle bg-bg-surface px-3 py-2 text-xs"
        >
          <div className="flex items-center gap-2">
            {wsStatus === 'connected' ? (
              <Wifi className="h-3.5 w-3.5 text-success" aria-hidden="true" />
            ) : (
              <WifiOff
                className={cn(
                  'h-3.5 w-3.5',
                  wsStatus === 'connecting' ? 'text-warning' : 'text-text-tertiary',
                )}
                aria-hidden="true"
              />
            )}
            <span
              className={cn(
                'font-medium',
                wsStatus === 'connected'
                  ? 'text-success'
                  : wsStatus === 'connecting'
                    ? 'text-warning'
                    : 'text-text-secondary',
              )}
            >
              {wsLabel}
            </span>
          </div>
          {lastUpdateRel && (
            <span className="text-text-tertiary">Last update {lastUpdateRel}</span>
          )}
          {!lastUpdateRel && showPollingNote && (
            <span className="text-text-tertiary">Awaiting next poll…</span>
          )}
        </section>
      </div>
    </SlidePanel>
  )
}
