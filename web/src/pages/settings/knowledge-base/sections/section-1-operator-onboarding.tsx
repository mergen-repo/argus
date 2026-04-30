// FIX-239 DEV-541: Section 1 — Operator Onboarding Flow.

import { Plug } from 'lucide-react'
import { StepperFlow } from '../components/stepper-flow'
import { OnboardingChecklist } from '../components/onboarding-checklist'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'operator-onboarding',
  number: 1,
  title: 'Operator Onboarding Flow',
  subtitle: 'Five-stage path from creating an Operator record to live RADIUS traffic.',
  group: 'onboarding',
  icon: Plug,
  searchTerms: ['onboarding', 'new operator', 'operator add', 'integration', 'shared secret'],
  lastUpdated: '2026-04-27',
}

export function Component() {
  return (
    <div className="space-y-6">
      <StepperFlow
        layout="horizontal"
        steps={[
          { label: 'Create Operator', desc: 'Operators page → Add', status: 'done' },
          { label: 'Protocols Config', desc: 'RADIUS/Diameter/SBA', status: 'done' },
          { label: 'Firewall Whitelist', desc: 'NAS IP + Argus IP', status: 'current' },
          { label: 'Test Auth', desc: 'Live Tester / radclient', status: 'pending' },
          { label: 'Go Live', desc: 'Flip operator state', status: 'pending' },
        ]}
      />

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-3">Onboarding checklist</h3>
        <OnboardingChecklist
          items={[
            { id: 'op-record', title: 'Create Operator record', desc: 'Operators → Add → enter name, code (3-letter), region.' },
            { id: 'shared-secret', title: 'Generate shared secret', desc: 'Operator detail → Secrets tab → Rotate. Copy the value once; it is not shown again.' },
            { id: 'nas-ip', title: 'Register NAS IP allow-list', desc: 'Operator detail → Networking → add the operator NAS IP(s) that will originate Access-Request traffic.' },
            { id: 'protocols', title: 'Enable required protocols', desc: 'RADIUS Auth (1812/UDP), RADIUS Accounting (1813/UDP), and CoA outbound (3799/UDP) at minimum.' },
            { id: 'apns', title: 'Bind APNs', desc: 'Map operator-side APN names to internal APN records via Operator → APNs tab.' },
            { id: 'fw-inbound', title: 'Firewall inbound: open 1812/1813 from NAS IPs', desc: 'Restrict to specific NAS IPs only; never expose UDP 1812 to 0.0.0.0/0.' },
            { id: 'fw-outbound', title: 'Firewall outbound: allow 3799/UDP to NAS IP', desc: 'Required for CoA / Disconnect traffic to land at the operator NAS.' },
            { id: 'time-sync', title: 'Verify NTP / clock skew < 1s', desc: 'EAP-AKA token windows are tight; clock skew breaks auth silently.' },
            { id: 'test-radclient', title: 'radclient smoke test from NAS', desc: 'echo "User-Name=test" | radclient -x argus-host:1812 auth $SECRET → expect Access-Reject (state INACTIVE).' },
            { id: 'test-portal', title: 'Live Tester from Operator detail', desc: 'Operator detail → Test Connection → Run RADIUS — expect a 200 with a deterministic Access-Accept on a fixture SIM.' },
            { id: 'monitor', title: 'Confirm metrics are flowing', desc: 'Dashboard → Operator drilldown → see auth / accounting volumes ticking up.' },
            { id: 'flip-live', title: 'Flip operator state to live', desc: 'Operators list → toggle state → live. Audit entry written.' },
          ]}
        />
      </div>

      <div className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim p-3 text-[11px] text-warning">
        <strong>Tip:</strong> never share secrets via Slack/email. Use the operator detail "Send via secure link" flow — the link expires in 10 minutes and is single-use.
      </div>
    </div>
  )
}
