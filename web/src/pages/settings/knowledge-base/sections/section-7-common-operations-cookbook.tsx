// FIX-239 DEV-543: Section 7 — Common Operations Cookbook.

import { ChefHat, Ban, Plus, Activity, ShieldOff, KeyRound, Search, AlertTriangle } from 'lucide-react'
import { OperationCard } from '../components/operation-card'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'common-operations-cookbook',
  number: 7,
  title: 'Common Operations Cookbook',
  subtitle: 'Step-by-step recipes for the most frequent ops tasks.',
  group: 'operations',
  icon: ChefHat,
  searchTerms: ['cookbook', 'how to', 'suspend fleet', 'add apn', 'reduce bandwidth', 'block lost sim', 'rotate api key', 'session drop', 'failover'],
  lastUpdated: '2026-04-27',
}

export function Component() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
      <OperationCard
        icon={Ban}
        title="Suspend SIM fleet (1000 SIMs)"
        description="Bulk-suspend a tagged cohort, e.g. when a customer cancels a contract."
        steps={[
          'SIM List → filter by tag/customer or paste ICCIDs into the bulk import box.',
          'Select all matching → Bulk Actions → Suspend.',
          'Confirm dialog: enter reason + tick "Send Disconnect to active sessions" to drop traffic immediately.',
          'Background job tracks per-id outcome; Failed list available in Jobs page if any IDs were already terminated.',
        ]}
        warnings={['Disconnect emits CoA Disconnect-Request to the NAS — may briefly spike NAS CPU on large fleets.']}
      />

      <OperationCard
        icon={Plus}
        title="Add APN + assign fleet"
        description="Provision a new APN and bind it to a SIM cohort."
        steps={[
          'APNs page → Add → enter name, type (private_managed for Argus IPAM), and IP pool reference.',
          'Operator detail → APNs tab → bind the new APN to the operator with a route policy.',
          'SIM List → filter cohort → Bulk Actions → Set APN.',
          'Verify: pick one SIM, check details show new APN + IP pool; trigger reauth to apply at the next session.',
        ]}
      />

      <OperationCard
        icon={Activity}
        title="Reduce bandwidth via policy rollout"
        description="Lower the rate limit for a cohort without touching individual SIM records."
        steps={[
          'Policies → New version → DSL: WHEN tag includes "fleet-x" THEN throttle rate=64kbps.',
          'Preview → Dry Run on last 24h → expect zero rejects (only throttle action).',
          'Canary 1% → wait 30 min → check session count, error rate, support tickets stable.',
          'Advance 10% → 50% → 100%. Each step emits CoA Filter-Id updates to active sessions.',
        ]}
        warnings={['Throttle takes effect on the NEXT CoA — already-active sessions continue at the old rate until the next rate refresh.']}
      />

      <OperationCard
        icon={ShieldOff}
        title="Block lost SIM"
        description="Mark a SIM as stolen / lost and immediately drop active sessions."
        tone="danger"
        steps={[
          'SIM Detail → Status → set state to stolen_lost → confirm.',
          'Active Sessions panel auto-issues a Disconnect-Request; the NAS terminates the session.',
          'A Reject is now returned for any subsequent Access-Request for this SIM.',
          'Audit log: sim.state_changed (active → stolen_lost), session.terminated, coa.disconnect.',
        ]}
        warnings={['Once flagged, only an admin with sim_manager role can reactivate.']}
      />

      <OperationCard
        icon={KeyRound}
        title="Rotate API key"
        description="Issue a new key for an integration partner without breaking current traffic."
        steps={[
          'Settings → API Keys → select the key → Rotate.',
          'Argus generates a new secret and shows it once; the old key remains valid for the configured grace window (default 24h).',
          'Distribute the new secret to the integration partner via your secure channel.',
          'After grace expires, revoke the old key. Verify partner still authenticates with the new key.',
        ]}
      />

      <OperationCard
        icon={Search}
        title="Investigate session drop spike"
        description="Sessions dropping faster than usual in the last hour."
        steps={[
          'Dashboard → Session graph → filter operator + last hour → confirm magnitude.',
          'Sessions page → filter Acct-Terminate-Cause = Lost-Carrier or NAS-Reboot to see if it is RF or operator-side.',
          'Operator detail → Health → check NAS heartbeat / interim-update gap metric.',
          'If gap > 300s, NAS may have rebooted — coordinate with operator NOC.',
          'Open ticket if persistent; export the affected sessions CSV for post-mortem.',
        ]}
      />

      <OperationCard
        icon={AlertTriangle}
        title="Handle operator outage (failover)"
        description="Primary operator NAS is unreachable; route traffic via the backup operator."
        tone="warning"
        steps={[
          'Operators page → confirm primary operator state shows degraded or unreachable.',
          'Move SIMs whose policy is "any" to the secondary operator: SIM List → filter operator=primary → Bulk Actions → Set Operator.',
          'For SIMs locked to a single operator, no automatic failover — surface to the customer with the planned ETA.',
          'Once primary recovers, re-balance via the same bulk action; audit log captures both moves.',
        ]}
        warnings={['Auth latency increases until DNS / Diameter routes converge — keep an eye on Dashboard latency panel.']}
      />
    </div>
  )
}
