// FIX-239 DEV-542: Section 4 — Policy Workflow.

import { Workflow } from 'lucide-react'
import { StepperFlow } from '../components/stepper-flow'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'policy-workflow',
  number: 4,
  title: 'Policy Workflow',
  subtitle: 'DSL or Form authoring → Preview → Dry Run → Canary 1% → Advance → Full rollout.',
  group: 'operations',
  icon: Workflow,
  searchTerms: ['policy', 'dsl', 'rollout', 'canary', 'dry run', 'preview', 'rule', 'staged'],
  lastUpdated: '2026-04-27',
}

export function Component() {
  return (
    <div className="space-y-6">
      <StepperFlow
        layout="horizontal"
        steps={[
          { label: 'Author', desc: 'DSL or Form' },
          { label: 'Preview', desc: 'AST + reachability' },
          { label: 'Dry Run', desc: 'Last 24h replay' },
          { label: 'Canary 1%', desc: 'Sticky cohort' },
          { label: 'Advance', desc: '10% → 50% → 100%' },
          { label: 'Active', desc: 'rolling_out → active' },
        ]}
      />

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Lifecycle states (post-FIX-231)</h3>
        <ul className="text-xs text-text-secondary space-y-1.5">
          <li><code className="font-mono text-[11px] mr-2 px-1 rounded bg-bg-primary text-accent">draft</code>writeable; not enforced.</li>
          <li><code className="font-mono text-[11px] mr-2 px-1 rounded bg-bg-primary text-accent">rolling_out</code>active for the staged cohort only; previous version still serves the rest.</li>
          <li><code className="font-mono text-[11px] mr-2 px-1 rounded bg-bg-primary text-success">active</code>fully promoted; serves all SIMs in scope. Exactly one active version per policy at any time.</li>
          <li><code className="font-mono text-[11px] mr-2 px-1 rounded bg-bg-primary text-text-tertiary">superseded</code>retained for audit; no enforcement.</li>
        </ul>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Cohort selection</h3>
        <p className="text-xs text-text-secondary leading-relaxed">
          Cohort filter is itself a DSL predicate (<code className="font-mono text-[11px]">tags includes "fleet-x"</code>,{' '}
          <code className="font-mono text-[11px]">operator.code = "tts"</code>, etc.). The rollout engine evaluates the
          predicate against every SIM in the policy's scope at advance-time, then sticky-hashes (consistent hashing on
          ICCID) to ensure the same SIM stays in the same cohort across percentage advances.
        </p>
      </div>

      <div className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim p-3 text-[11px] text-warning">
        <strong>Rule:</strong> rollback always allowed mid-rollout. Promoting <code className="font-mono">active</code> →{' '}
        <code className="font-mono">superseded</code> is a one-way hatch; older versions are not auto-resurrected. Use the
        Re-publish action to re-introduce a superseded version as a new draft.
      </div>
    </div>
  )
}
