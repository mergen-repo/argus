import { useState } from 'react'
import { Plus, GitCompare, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tooltip } from '@/components/ui/tooltip'
import { useVersionDiff } from '@/hooks/use-policies'
import type { PolicyVersion, DiffLine } from '@/types/policy'

interface VersionsTabProps {
  versions: PolicyVersion[]
  currentVersionId?: string
  onSelectVersion: (version: PolicyVersion) => void
  onCreateVersion: () => void
  isCreating: boolean
}

function versionStateVariant(state: string): 'success' | 'warning' | 'default' | 'secondary' {
  switch (state) {
    case 'active': return 'success'
    case 'draft': return 'warning'
    case 'superseded': return 'secondary'
    case 'archived': return 'secondary'
    default: return 'default'
  }
}

function DiffViewer({ id1, id2 }: { id1: string; id2: string }) {
  const { data: diff, isLoading } = useVersionDiff(id1, id2)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8 text-text-tertiary">
        <Loader2 className="h-4 w-4 animate-spin mr-2" />
        Loading diff...
      </div>
    )
  }

  if (!diff) return null

  return (
    <div className="rounded-[var(--radius-sm)] border border-border bg-bg-primary overflow-hidden">
      <div className="px-3 py-1.5 bg-bg-elevated border-b border-border text-xs text-text-secondary">
        v{diff.version_1} vs v{diff.version_2}
      </div>
      <div className="overflow-auto max-h-64 font-mono text-xs">
        {diff.lines.map((line: DiffLine, i: number) => (
          <div
            key={i}
            className={`px-3 py-0.5 border-l-2 ${
              line.type === 'added'
                ? 'bg-success-dim/30 border-success text-success'
                : line.type === 'removed'
                ? 'bg-danger-dim/30 border-danger text-danger'
                : 'border-transparent text-text-secondary'
            }`}
          >
            <span className="select-none mr-3 text-text-tertiary w-6 inline-block text-right">
              {line.type === 'added' ? '+' : line.type === 'removed' ? '-' : ' '}
            </span>
            {line.content}
          </div>
        ))}
      </div>
    </div>
  )
}

interface VersionWithRollback extends PolicyVersion {
  rolled_back_at?: string
}

function buildTooltipContent(v: VersionWithRollback): string {
  const parts: string[] = []
  parts.push(`Created: ${new Date(v.created_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })}`)
  if (v.activated_at) {
    parts.push(`Activated: ${new Date(v.activated_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })}`)
  }
  if (v.rolled_back_at) {
    parts.push(`Rolled back: ${new Date(v.rolled_back_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })}`)
  }
  return parts.join(' · ')
}

function VersionTimeline({ versions }: { versions: PolicyVersion[] }) {
  if (versions.length === 0) return null

  const chronological = [...versions].sort((a, b) => a.version - b.version)

  return (
    <div className="mb-4">
      <h3 className="text-sm font-semibold text-text-primary mb-3">Version timeline</h3>
      <div className="flex items-center gap-2 flex-wrap">
        {chronological.map((v, idx) => {
          const isActive = v.state === 'active'
          const isRollingOut = v.state === 'rolling_out'
          const vWithRollback = v as VersionWithRollback

          const stateLabel = v.state.charAt(0).toUpperCase() + v.state.slice(1).replace('_', ' ')
          const dateLabel = v.activated_at
            ? new Date(v.activated_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })
            : new Date(v.created_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })

          const ariaSummary = [`Version ${v.version}`, stateLabel, dateLabel]
            .filter(Boolean).join(', ')

          return (
            <div key={v.id} className="flex items-center gap-2">
              <Tooltip content={buildTooltipContent(vWithRollback)} side="top">
                <div
                  tabIndex={0}
                  role="group"
                  aria-label={ariaSummary}
                  className="flex flex-col items-center gap-1 rounded-[var(--radius-sm)] focus:outline-none focus-visible:ring-1 focus-visible:ring-accent"
                >
                  <Badge
                    variant={versionStateVariant(v.state)}
                    className={[
                      'cursor-default select-none',
                      isActive ? 'ring-1 ring-success' : '',
                      isRollingOut ? 'animate-pulse' : '',
                    ].filter(Boolean).join(' ')}
                  >
                    v{v.version}
                  </Badge>
                  <div className="flex flex-col items-center gap-0.5">
                    <span className="text-[10px] font-medium text-text-secondary leading-none">{stateLabel}</span>
                    <span className="text-[10px] text-text-tertiary leading-none">{dateLabel}</span>
                  </div>
                </div>
              </Tooltip>
              {idx < chronological.length - 1 && (
                <div className="h-px bg-border flex-1 min-w-[16px]" aria-hidden="true" />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function VersionsTab({ versions, currentVersionId, onSelectVersion, onCreateVersion, isCreating }: VersionsTabProps) {
  const [diffPair, setDiffPair] = useState<[string, string] | null>(null)

  const sorted = [...versions].sort((a, b) => b.version - a.version)

  const handleDiff = (v1: PolicyVersion, v2: PolicyVersion) => {
    if (diffPair && diffPair[0] === v1.id && diffPair[1] === v2.id) {
      setDiffPair(null)
    } else {
      setDiffPair([v1.id, v2.id])
    }
  }

  return (
    <div className="p-4 space-y-4 overflow-y-auto h-full">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          Versions ({versions.length})
        </h4>
        <Button size="sm" className="gap-1.5 text-xs" onClick={onCreateVersion} disabled={isCreating}>
          {isCreating ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
          New Version
        </Button>
      </div>

      <VersionTimeline versions={versions} />

      <div className="space-y-2">
        {sorted.map((version, idx) => {
          const isSelected = currentVersionId === version.id
          return (
            <div key={version.id}>
              <button
                onClick={() => onSelectVersion(version)}
                className={`w-full text-left p-3 rounded-[var(--radius-sm)] border transition-colors ${
                  isSelected
                    ? 'border-accent/30 bg-accent-dim'
                    : 'border-border-subtle bg-bg-surface hover:border-border hover:bg-bg-hover'
                }`}
              >
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-text-primary">v{version.version}</span>
                    <Badge variant={versionStateVariant(version.state)} className="text-[10px]">
                      {version.state.toUpperCase()}
                    </Badge>
                  </div>
                  {version.affected_sim_count != null && (
                    <span className="text-xs font-mono text-text-tertiary">
                      {version.affected_sim_count.toLocaleString()} SIMs
                    </span>
                  )}
                </div>
                <div className="text-xs text-text-tertiary">
                  {new Date(version.created_at).toLocaleString()}
                  {version.activated_at && (
                    <span className="ml-2 text-success">
                      activated {new Date(version.activated_at).toLocaleDateString()}
                    </span>
                  )}
                </div>
              </button>

              {idx < sorted.length - 1 && (
                <div className="flex justify-center my-1">
                  <button
                    onClick={() => handleDiff(version, sorted[idx + 1])}
                    className={`flex items-center gap-1 px-2 py-0.5 text-[10px] rounded-full border transition-colors ${
                      diffPair && diffPair[0] === version.id && diffPair[1] === sorted[idx + 1].id
                        ? 'border-accent/30 bg-accent-dim text-accent'
                        : 'border-border text-text-tertiary hover:text-accent hover:border-accent/30'
                    }`}
                  >
                    <GitCompare className="h-3 w-3" />
                    diff
                  </button>
                </div>
              )}

              {diffPair && diffPair[0] === version.id && idx < sorted.length - 1 && (
                <div className="my-2">
                  <DiffViewer id1={diffPair[0]} id2={diffPair[1]} />
                </div>
              )}
            </div>
          )
        })}
      </div>

      {versions.length === 0 && (
        <div className="flex flex-col items-center justify-center py-12 text-text-tertiary">
          <span className="text-sm">No versions yet</span>
        </div>
      )}
    </div>
  )
}
