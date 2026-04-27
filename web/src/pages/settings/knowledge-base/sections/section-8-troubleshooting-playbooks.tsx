// FIX-239 DEV-544: Section 8 — Troubleshooting Playbooks.

import { Stethoscope } from 'lucide-react'
import { DecisionTree, type TreeNode } from '../components/decision-tree'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'troubleshooting-playbooks',
  number: 8,
  title: 'Troubleshooting Playbooks',
  subtitle: 'Decision trees for the three most common P1 incidents.',
  group: 'troubleshooting',
  icon: Stethoscope,
  searchTerms: ['troubleshoot', 'playbook', 'incident', 'auth fail', 'policy not applying', 'session stuck idle'],
  lastUpdated: '2026-04-27',
}

const ALL_AUTH_FAIL: TreeNode = {
  kind: 'question',
  question: 'All SIMs from one operator are returning Access-Reject — where to look?',
  branches: [
    {
      label: 'Affects only one operator',
      child: {
        kind: 'question',
        question: 'Is the operator NAS reachable?',
        branches: [
          {
            label: 'No — UDP 1812 timeouts',
            child: {
              kind: 'action',
              title: 'Check operator firewall / NAS allowlist',
              steps: [
                'Operator detail → Health: confirm latest Access-Request timestamp.',
                'Coordinate with operator NOC to verify Argus IP is in their allowlist.',
                'Check Argus side firewall — recent change may have dropped 1812/UDP from operator subnet.',
              ],
              logPattern: `grep "udp_listen_drop" /var/log/argus/aaa.log | tail`,
            },
          },
          {
            label: 'Yes — requests arrive but reject',
            child: {
              kind: 'action',
              title: 'Verify shared secret + clock skew',
              steps: [
                'Operator detail → Secrets: confirm the secret matches what the operator NAS holds.',
                'Run `ntpq -p` on Argus host; verify clock skew under 1 second.',
                'EAP-AKA token windows are tight; even 2-second skew breaks auth silently.',
              ],
              query: `SELECT to_timestamp(ts/1000) AS at, error_message FROM audit_logs
WHERE action = 'auth.reject' AND error_code = 'auth_failed'
ORDER BY ts DESC LIMIT 50;`,
            },
          },
        ],
      },
    },
    {
      label: 'Affects all operators',
      child: {
        kind: 'action',
        title: 'Likely platform issue — check internal dependencies',
        steps: [
          'Postgres reachable? `psql -h pg-host -c "SELECT 1"` from the Argus host.',
          'Redis reachable? `redis-cli -h redis-host PING`.',
          'NATS up? `curl http://nats-host:8222/varz` returns ok.',
          'If any infra is down, escalate — every Access-Request stalls at the first dependency.',
        ],
      },
    },
  ],
}

const POLICY_NOT_APPLYING: TreeNode = {
  kind: 'question',
  question: 'A new policy is not applying to SIMs in scope — where to look?',
  branches: [
    {
      label: 'Policy state is not active or rolling_out',
      child: {
        kind: 'action',
        title: 'Promote the version',
        steps: [
          'Policies → version detail → check state. draft never enforces.',
          'Run Dry Run → if green, advance to canary 1%.',
          'For an existing rollout: check if the rollout is paused; advance or rollback as appropriate.',
        ],
        query: `SELECT id, version, state FROM policy_versions
WHERE policy_id = $POLICY_ID
ORDER BY version DESC;`,
      },
    },
    {
      label: 'Policy is rolling_out but SIM not in cohort',
      child: {
        kind: 'action',
        title: 'Verify cohort filter and sticky hash',
        steps: [
          'Rollout detail → cohort filter → re-evaluate against the SIM (Cohort match preview).',
          'Confirm the SIM\'s current hash bucket falls within the active percentage window.',
          'If filter is correct but bucket excluded: this is expected — wait for next advance.',
        ],
      },
    },
    {
      label: 'Policy active for everyone, but specific SIM still old behavior',
      child: {
        kind: 'action',
        title: 'CoA was not delivered or rejected',
        steps: [
          'Sessions page → find the SIM\'s active session → check last CoA timestamp.',
          'Audit log: filter coa.request / coa.ack / coa.nak for this SIM.',
          'If CoA-NAK with a NAS-side error: open ticket with operator. Until the next CoA, the session uses the version from session start.',
        ],
        logPattern: `grep "coa.nak" /var/log/argus/coa.log | grep $ICCID`,
      },
    },
  ],
}

const STUCK_IDLE: TreeNode = {
  kind: 'question',
  question: 'Session has been "active" for > 1 hour with no traffic — is it stuck?',
  branches: [
    {
      label: 'Last interim-update was within 10 min',
      child: {
        kind: 'action',
        title: 'Healthy idle session',
        steps: [
          'Sessions are kept open until idle-timeout fires; default 30 min in Argus, but operator can override.',
          'No action needed unless idle-timeout in policy is wrong for this fleet.',
        ],
      },
    },
    {
      label: 'No interim-update for > 30 min',
      child: {
        kind: 'question',
        question: 'Was the NAS rebooted or is connectivity lost?',
        branches: [
          {
            label: 'NAS heartbeat unhealthy',
            child: {
              kind: 'action',
              title: 'Orphaned session — clean up',
              steps: [
                'Sessions page → select stuck session → Force-Stop. Argus writes a synthetic Acct-Stop with terminate-cause = NAS-Reboot.',
                'IP pool releases the lease; SIM can reattach freely.',
                'Investigate NAS uptime with operator NOC.',
              ],
              query: `SELECT id, started_at, last_interim_at FROM sessions
WHERE state = 'active' AND last_interim_at < NOW() - INTERVAL '30 minutes';`,
            },
          },
          {
            label: 'NAS healthy but no interim',
            child: {
              kind: 'action',
              title: 'Likely operator misconfig — interim disabled',
              steps: [
                'Operator detail → Protocols → confirm Acct-Interim-Interval is set on the NAS profile.',
                'If interim is intentionally disabled, raise the idle-timeout threshold to avoid false positives.',
              ],
            },
          },
        ],
      },
    },
  ],
}

export function Component() {
  return (
    <div className="space-y-5">
      <Playbook title="All SIMs auth failing">
        <DecisionTree root={ALL_AUTH_FAIL} />
      </Playbook>
      <Playbook title="Policy not applying">
        <DecisionTree root={POLICY_NOT_APPLYING} />
      </Playbook>
      <Playbook title="Sessions stuck idle > 1h">
        <DecisionTree root={STUCK_IDLE} />
      </Playbook>
    </div>
  )
}

function Playbook({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-[var(--radius-md)] border border-warning/30 overflow-hidden">
      <div className="bg-warning-dim border-b border-warning/30 px-3 py-2">
        <h3 className="text-[12px] font-semibold text-warning">{title}</h3>
      </div>
      <div className="p-3">{children}</div>
    </div>
  )
}
