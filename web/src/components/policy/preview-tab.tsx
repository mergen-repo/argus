import { Loader2, AlertCircle, Users, Radio, Globe } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import type { DryRunResult, SampleSIM } from '@/types/policy'
import { RAT_DISPLAY } from '@/lib/constants'
import { formatDuration } from '@/lib/format'

interface PreviewTabProps {
  result: DryRunResult | null | undefined
  isLoading: boolean
  error?: unknown
}

function formatBps(bps: number): string {
  if (bps >= 1_000_000_000) return `${(bps / 1_000_000_000).toFixed(1)} Gbps`
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} Kbps`
  return `${bps} bps`
}

function BreakdownBar({ label, data }: { label: string; data: Record<string, number> }) {
  const entries = Object.entries(data).sort((a, b) => b[1] - a[1])
  const total = entries.reduce((sum, [, v]) => sum + v, 0)
  if (total === 0) return null

  const colors = ['bg-accent', 'bg-purple', 'bg-success', 'bg-warning', 'bg-danger', 'bg-info']

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-text-secondary">{label}</span>
        <span className="text-xs text-text-tertiary">{total.toLocaleString()} total</span>
      </div>
      <div className="flex h-2 rounded-full overflow-hidden bg-bg-hover">
        {entries.map(([key, val], i) => (
          <div
            key={key}
            className={`${colors[i % colors.length]} transition-all`}
            style={{ width: `${(val / total) * 100}%` }}
            title={`${key}: ${val.toLocaleString()}`}
          />
        ))}
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {entries.map(([key, val], i) => (
          <div key={key} className="flex items-center gap-1.5 text-xs text-text-secondary">
            <span className={`h-2 w-2 rounded-full ${colors[i % colors.length]}`} />
            <span>{label === 'By RAT' ? (RAT_DISPLAY[key] || key) : key}</span>
            <span className="font-mono text-text-tertiary">{val.toLocaleString()}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function SampleSimRow({ sim }: { sim: SampleSIM }) {
  return (
    <div className="border border-border-subtle rounded-[var(--radius-sm)] p-3 bg-bg-surface">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-xs text-accent">{sim.iccid}</span>
          {sim.ip_address && (
            <span className="font-mono text-[10px] text-text-tertiary">{sim.ip_address}</span>
          )}
        </div>
        <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary">
          {RAT_DISPLAY[sim.rat_type] || sim.rat_type}
        </span>
      </div>
      <div className="flex gap-3 text-xs text-text-secondary mb-2">
        <span>{sim.operator}</span>
        <span className="text-text-tertiary">/</span>
        <span>{sim.apn}</span>
      </div>
      {sim.before && sim.after && (
        <div className="grid grid-cols-2 gap-2 text-xs">
          <div className="text-text-tertiary font-medium">Before</div>
          <div className="text-text-tertiary font-medium">After</div>
          {sim.after.bandwidth_down !== sim.before.bandwidth_down && (
            <>
              <div className="font-mono text-danger">{formatBps(sim.before.bandwidth_down ?? 0)} down</div>
              <div className="font-mono text-success">{formatBps(sim.after.bandwidth_down ?? 0)} down</div>
            </>
          )}
          {sim.after.bandwidth_up !== sim.before.bandwidth_up && (
            <>
              <div className="font-mono text-danger">{formatBps(sim.before.bandwidth_up ?? 0)} up</div>
              <div className="font-mono text-success">{formatBps(sim.after.bandwidth_up ?? 0)} up</div>
            </>
          )}
          {sim.after.session_timeout !== sim.before.session_timeout && (
            <>
              <div className="font-mono text-danger">{formatDuration(sim.before.session_timeout ?? 0)}</div>
              <div className="font-mono text-success">{formatDuration(sim.after.session_timeout ?? 0)}</div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

export function PreviewTab({ result, isLoading, error }: PreviewTabProps) {
  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-text-tertiary">
        <Loader2 className="h-6 w-6 animate-spin mb-3" />
        <span className="text-sm">Running dry-run simulation...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <AlertCircle className="h-6 w-6 text-danger mb-3" />
        <span className="text-sm text-danger">Dry-run failed</span>
        <span className="text-xs text-text-tertiary mt-1">Check DSL syntax and try again</span>
      </div>
    )
  }

  if (!result) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-text-tertiary">
        <Users className="h-8 w-8 mb-3 opacity-50" />
        <span className="text-sm">Save draft to see dry-run preview</span>
        <span className="text-xs mt-1">Press Ctrl+Enter to run</span>
      </div>
    )
  }

  return (
    <div className="p-4 space-y-5 overflow-y-auto h-full">
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-2 px-3 py-2 rounded-[var(--radius-sm)] bg-accent-dim border border-accent/20">
          <Users className="h-4 w-4 text-accent" />
          <span className="text-2xl font-bold font-mono text-accent">
            {result.total_affected.toLocaleString()}
          </span>
        </div>
        <div>
          <div className="text-sm font-medium text-text-primary">Affected SIMs</div>
          <div className="text-xs text-text-tertiary">
            Evaluated {new Date(result.evaluated_at).toLocaleString()}
          </div>
        </div>
      </div>

      <div className="space-y-4">
        <BreakdownBar label="By Operator" data={result.by_operator} />
        <BreakdownBar label="By APN" data={result.by_apn} />
        <BreakdownBar label="By RAT" data={result.by_rat} />
      </div>

      {result.behavioral_changes && result.behavioral_changes.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-xs font-medium text-text-secondary uppercase tracking-wider">Behavioral Changes</h4>
          {result.behavioral_changes.map((change, i) => (
            <div key={i} className="flex items-start gap-2 p-2 rounded-[var(--radius-sm)] bg-bg-surface border border-border-subtle">
              <Badge variant="secondary" className="text-[10px] shrink-0">{change.type}</Badge>
              <div className="min-w-0">
                <div className="text-xs text-text-primary">{change.description}</div>
                <div className="text-xs text-text-tertiary mt-0.5">
                  {change.affected_count.toLocaleString()} SIMs
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {result.sample_sims && result.sample_sims.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-xs font-medium text-text-secondary uppercase tracking-wider">
            Sample SIMs ({result.sample_sims.length})
          </h4>
          <div className="space-y-2">
            {result.sample_sims.map((sim) => (
              <SampleSimRow key={sim.sim_id} sim={sim} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
