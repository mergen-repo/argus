// FIX-239 DEV-542: Section 5 — IP Allocation + APN Types.

import { Network } from 'lucide-react'
import { SequenceDiagram } from '../components/sequence-diagram'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'ip-allocation-apn-types',
  number: 5,
  title: 'IP Allocation + APN Types',
  subtitle: 'How addresses leave the pool, how APN ownership decides who controls IPAM.',
  group: 'operations',
  icon: Network,
  searchTerms: ['ip pool', 'ipam', 'allocation', 'apn', 'private managed', 'operator managed', 'customer managed', 'sticky ip'],
  lastUpdated: '2026-04-27',
}

const APN_TYPES = [
  {
    type: 'private_managed',
    label: 'Private Managed',
    color: 'text-accent',
    bg: 'bg-accent-dim border-accent/20',
    desc: 'Argus fully controls the APN. IP allocation, QoS policies, and routing are managed internally. Suitable for closed enterprise deployments where Argus is the single source of truth.',
    useCase: 'Enterprise IoT fleets, closed M2M networks',
  },
  {
    type: 'operator_managed',
    label: 'Operator Managed',
    color: 'text-warning',
    bg: 'bg-warning-dim border-warning/20',
    desc: 'The mobile operator owns the APN configuration. Argus acts as a RADIUS AAA backend, handling authentication and policy only. IP assignment is delegated to the operator core.',
    useCase: 'MVNO deployments, operator wholesale agreements',
  },
  {
    type: 'customer_managed',
    label: 'Customer Managed',
    color: 'text-success',
    bg: 'bg-success-dim border-success/20',
    desc: 'The end customer owns and manages the APN. Argus provides authentication hooks and audit logging. Traffic routing and IP assignment happen outside the Argus control plane.',
    useCase: 'Large enterprise with dedicated APN infrastructure',
  },
]

export function Component() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-3">Lease lifecycle (private_managed APN)</h3>
        <SequenceDiagram
          actors={['SIM', 'Argus AAA', 'IP Pool Store', 'NAS']}
          messages={[
            { from: 0, to: 3, label: 'Attach' },
            { from: 3, to: 1, label: 'Access-Request' },
            { from: 1, to: 2, label: 'Acquire lease', kind: 'async' },
            { from: 2, to: 1, label: 'IP', kind: 'reply' },
            { from: 1, to: 3, label: 'Access-Accept (IP)', kind: 'reply' },
            { from: 3, to: 1, label: 'Acct-Stop' },
            { from: 1, to: 2, label: 'Release lease' },
          ]}
        />
        <p className="text-[11px] text-text-tertiary mt-2">
          Sticky-IP option: on lease acquisition, the SIM ↔ IP mapping is cached for 24h; subsequent attaches re-issue the
          same address if it's still free. Useful for whitelisted IoT devices.
        </p>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-3">APN ownership models</h3>
        <div className="space-y-2">
          {APN_TYPES.map((apn) => (
            <div key={apn.type} className={`rounded-[var(--radius-sm)] border p-3 ${apn.bg}`}>
              <div className="flex items-center gap-2 mb-1.5">
                <span className={`text-xs font-mono font-semibold ${apn.color}`}>{apn.type}</span>
                <span className="text-xs text-text-secondary">·</span>
                <span className="text-xs font-medium text-text-primary">{apn.label}</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed mb-1">{apn.desc}</p>
              <p className="text-[10px] text-text-tertiary">
                <span className="uppercase tracking-wider mr-1">Use case:</span>{apn.useCase}
              </p>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
