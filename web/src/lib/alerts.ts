// Canonical FE enum for alerts.source and alerts.state (FIX-209).

export const ALERT_SOURCE_VALUES = ['sim', 'operator', 'infra', 'policy', 'system'] as const;
export type AlertSource = typeof ALERT_SOURCE_VALUES[number];

export const ALERT_SOURCE_OPTIONS: ReadonlyArray<{ value: '' | AlertSource; label: string }> = [
  { value: '',         label: 'All Sources' },
  { value: 'sim',      label: 'SIM' },
  { value: 'operator', label: 'Operator' },
  { value: 'infra',    label: 'Infra' },
  { value: 'policy',   label: 'Policy' },
  { value: 'system',   label: 'System' },
];

export function isAlertSource(s: string): s is AlertSource {
  return (ALERT_SOURCE_VALUES as readonly string[]).includes(s);
}

export const ALERT_STATE_VALUES = ['open', 'acknowledged', 'resolved', 'suppressed'] as const;
export type AlertState = typeof ALERT_STATE_VALUES[number];

export const ALERT_STATE_OPTIONS: ReadonlyArray<{ value: '' | AlertState; label: string }> = [
  { value: '',             label: 'All' },
  { value: 'open',         label: 'Active' },
  { value: 'acknowledged', label: 'Acknowledged' },
  { value: 'resolved',     label: 'Resolved' },
  { value: 'suppressed',   label: 'Suppressed' },
];

function humanizeWindow(ms: number): string {
  if (ms < 1000) return '<1s';
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  const d = Math.floor(h / 24);
  if (s < 60) return `${s}s`;
  if (m < 60) return `${m}m`;
  if (h < 24) return `${h}h`;
  return `${d}d`;
}

export function formatOccurrence(count: number, firstSeenAt: string, lastSeenAt: string): string {
  if (count <= 1) return '';
  const first = new Date(firstSeenAt).getTime();
  const last = new Date(lastSeenAt).getTime();
  const windowMs = Math.max(last - first, 0);
  return `${count}× in last ${humanizeWindow(windowMs)}`;
}

export function isCooldownActive(cooldownUntil: string | null | undefined, now = Date.now()): boolean {
  if (!cooldownUntil) return false;
  return new Date(cooldownUntil).getTime() > now;
}
