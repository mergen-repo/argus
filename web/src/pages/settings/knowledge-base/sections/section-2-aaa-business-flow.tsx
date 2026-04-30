// FIX-239 DEV-541: Section 2 — AAA Business Flow.
//
// Integrates existing "SIM Authentication Flow" + "Security Mechanisms" +
// "CoA/DM Session Control" into one operational view.

import { GitBranch } from 'lucide-react'
import { SequenceDiagram } from '../components/sequence-diagram'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'aaa-business-flow',
  number: 2,
  title: 'AAA Business Flow',
  subtitle: 'End-to-end authentication, accounting, and mid-session control.',
  group: 'operations',
  icon: GitBranch,
  searchTerms: ['radius', 'access-request', 'access-accept', 'access-reject', 'eap-sim', 'eap-aka', 'coa', 'dm', 'disconnect'],
  lastUpdated: '2026-04-27',
}

const REJECT_REASONS: { code: string; meaning: string; cause: string }[] = [
  { code: 'sim_not_found', meaning: 'ICCID/IMSI not in DB', cause: 'SIM not provisioned or wrong operator routing.' },
  { code: 'sim_inactive', meaning: 'state ≠ active', cause: 'Suspended, terminated, or pending activation.' },
  { code: 'apn_not_allowed', meaning: 'requested APN not in allow-list', cause: 'Wrong device profile or operator misconfig.' },
  { code: 'concurrent_limit', meaning: 'max sessions exceeded', cause: 'Existing session not stopped — check accounting flow.' },
  { code: 'policy_block', meaning: 'DSL rule rejected', cause: 'Quota exceeded, time window outside allowed, geo blocked.' },
  { code: 'auth_failed', meaning: 'EAP-SIM/AKA challenge failed', cause: 'Wrong shared secret, clock skew, or SIM cloning.' },
]

export function Component() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-3">RADIUS auth round-trip</h3>
        <SequenceDiagram
          actors={['Device', 'NAS / P-GW', 'Argus AAA', 'PostgreSQL']}
          messages={[
            { from: 0, to: 1, label: 'Attach' },
            { from: 1, to: 2, label: 'Access-Request' },
            { from: 2, to: 3, label: 'SIM lookup', kind: 'async' },
            { from: 3, to: 2, label: 'SIM row', kind: 'reply' },
            { from: 2, to: 1, label: 'Access-Accept (IP, QoS)', kind: 'reply' },
            { from: 1, to: 2, label: 'Acct-Start' },
            { from: 1, to: 2, label: 'Acct-Interim (every 5min)' },
            { from: 1, to: 2, label: 'Acct-Stop' },
          ]}
        />
        <p className="text-[11px] text-text-tertiary mt-2">
          Sub-millisecond happy-path: Redis cache hit on hot SIMs short-circuits the PG lookup; DB is consulted only on cache miss.
        </p>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Access-Reject reasons cheatsheet</h3>
        <div className="overflow-x-auto rounded-[var(--radius-sm)] border border-border-subtle">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Reply-Message code</TableHead>
                <TableHead>Meaning</TableHead>
                <TableHead>Likely cause</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {REJECT_REASONS.map((r) => (
                <TableRow key={r.code}>
                  <TableCell><span className="font-mono text-[11px] text-text-primary">{r.code}</span></TableCell>
                  <TableCell><span className="text-xs text-text-secondary">{r.meaning}</span></TableCell>
                  <TableCell><span className="text-xs text-text-tertiary">{r.cause}</span></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">CoA / DM in practice</h3>
        <ul className="space-y-2 text-xs text-text-secondary">
          <li>
            <strong className="text-text-primary">Argus → NAS :3799</strong> sends a CoA-Request when a policy rule is rolled
            out to a live SIM (mid-session bandwidth change, time-window exit, anomaly throttle). NAS must respond within 5
            seconds with CoA-ACK or CoA-NAK.
          </li>
          <li>
            <strong className="text-text-primary">Disconnect-Request</strong> (DM) is used for hard terminations:
            SIM suspended via API, anomaly detected, or admin bulk reset. The IP is returned to the pool only after the NAS
            responds Disconnect-ACK.
          </li>
          <li>
            <strong className="text-text-primary">Failures</strong> raise the <code className="font-mono text-[10px]">coa_failed</code>{' '}
            audit event with NAK error-cause; ops should grep audit logs and the NAS device for clock/secret mismatch.
          </li>
        </ul>
      </div>
    </div>
  )
}
