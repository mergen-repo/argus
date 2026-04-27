// FIX-239 DEV-541: Section 3 — Session Lifecycle.

import { RefreshCw } from 'lucide-react'
import { TimelineFlow } from '../components/timeline-flow'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'session-lifecycle',
  number: 3,
  title: 'Session Lifecycle',
  subtitle: 'RADIUS accounting state machine from device attach to IP release.',
  group: 'operations',
  icon: RefreshCw,
  searchTerms: ['session', 'accounting', 'acct-start', 'acct-stop', 'interim-update', 'cdr', 'lifecycle'],
  lastUpdated: '2026-04-27',
}

const STOP_REASONS: { name: string; class: 'normal' | 'idle' | 'admin' | 'error'; explanation: string }[] = [
  { name: 'User-Request', class: 'normal', explanation: 'Device gracefully detached (modem power-off, manual disconnect).' },
  { name: 'Lost-Carrier', class: 'normal', explanation: 'Radio link lost — typical on M2M devices entering low coverage.' },
  { name: 'Idle-Timeout', class: 'idle', explanation: 'No traffic for the configured idle window. Default 30 min on Argus.' },
  { name: 'Session-Timeout', class: 'idle', explanation: 'Maximum session duration reached. Per-SIM policy override possible.' },
  { name: 'Admin-Reset', class: 'admin', explanation: 'Operator triggered Disconnect-Request via Argus API or UI.' },
  { name: 'NAS-Reboot', class: 'error', explanation: 'NAS rebooted; sessions auto-cleared.' },
  { name: 'Port-Error', class: 'error', explanation: 'Hardware error on the NAS port; investigate operator side.' },
]

const STOP_TONE: Record<typeof STOP_REASONS[number]['class'], string> = {
  normal: 'border-success/30 bg-success-dim text-success',
  idle: 'border-accent/30 bg-accent-dim text-accent',
  admin: 'border-warning/30 bg-warning-dim text-warning',
  error: 'border-danger/30 bg-danger-dim text-danger',
}

export function Component() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Timeline</h3>
        <TimelineFlow
          markers={[
            { pct: 0, label: 'Attach', desc: 'Access-Request' },
            { pct: 8, label: 'Accept', desc: 'Access-Accept · IP' },
            { pct: 14, label: 'Acct-Start', tone: 'success' },
            { pct: 35, label: 'Interim', desc: 't+5m' },
            { pct: 55, label: 'Interim', desc: 't+10m' },
            { pct: 75, label: 'Interim', desc: 't+15m' },
            { pct: 92, label: 'Acct-Stop', tone: 'warning' },
            { pct: 100, label: 'IP release', tone: 'danger' },
          ]}
        />
        <p className="text-[11px] text-text-tertiary mt-2 leading-relaxed">
          Interim-Update cadence is operator-driven (5 min default). The IP is held by the SIM until Acct-Stop is received;
          a session held silently more than the operator interim-gap is treated as orphaned (see §8 Troubleshooting playbooks).
        </p>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Acct-Terminate-Cause reference</h3>
        <ul className="space-y-1.5">
          {STOP_REASONS.map((r) => (
            <li key={r.name} className="flex items-start gap-2 text-xs text-text-secondary">
              <span className={`shrink-0 inline-flex items-center rounded-[var(--radius-sm)] border px-1.5 py-0.5 text-[10px] font-mono ${STOP_TONE[r.class]}`}>
                {r.name}
              </span>
              <span>{r.explanation}</span>
            </li>
          ))}
        </ul>
      </div>

      <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated p-3 text-xs text-text-secondary">
        <p>
          <strong className="text-text-primary">CDR persistence:</strong> Acct-Stop triggers a single
          <code className="font-mono text-[11px] mx-1 px-1 rounded bg-bg-primary">INSERT INTO cdr</code>
          call against TimescaleDB. Interim-Updates write hot rows to Redis only; flushed to TimescaleDB on Stop or every
          15 minutes by the digest worker.
        </p>
      </div>
    </div>
  )
}
