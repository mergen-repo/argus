import { formatBytes } from '@/lib/format'
import type { LiveEvent } from '@/stores/events'

// Chip line: IMSI / IP / MSISDN (session context) first, then IDs, then
// bytes (session.* types only). Keeps scan density — one chip per
// meaningful data point, no padding noise.
function pickNumber(v: unknown): number | undefined {
  return typeof v === 'number' ? v : undefined
}

function pickString(v: unknown): string | undefined {
  return typeof v === 'string' && v ? v : undefined
}

export function EventSourceChips({ event }: { event: LiveEvent }) {
  const meta = event.meta || {}
  const chips: Array<{ label: string; value: string; highlight?: boolean; accent?: boolean }> = []

  const imsi = event.imsi || pickString(meta.imsi)
  const ip = event.framed_ip || pickString(meta.framed_ip)
  const msisdn = event.msisdn || pickString(meta.msisdn)

  if (imsi) chips.push({ label: 'IMSI', value: imsi, highlight: true })
  if (ip) chips.push({ label: 'IP', value: ip, highlight: true })
  if (msisdn) chips.push({ label: 'MSISDN', value: msisdn })

  if (event.operator_id && !imsi) chips.push({ label: 'Op', value: event.operator_id.slice(0, 8) })
  if (event.apn_id && !imsi) chips.push({ label: 'APN', value: event.apn_id.slice(0, 8) })
  if (event.policy_id) chips.push({ label: 'Policy', value: event.policy_id.slice(0, 8) })
  if (event.job_id) chips.push({ label: 'Job', value: event.job_id.slice(0, 8) })

  const progress = event.progress_pct ?? pickNumber(meta.progress_pct)
  if (typeof progress === 'number') chips.push({ label: '%', value: `${Math.round(progress)}` })

  // F-12 fix — bytes chips for session.* events. Read meta first (envelope
  // shape), fall back to top-level fields (legacy shim). AC-3 says "when
  // present" — so render 0-byte idle sessions too (F-A4), not only >0.
  if (event.type.startsWith('session.')) {
    const bytesIn = event.bytes_in ?? pickNumber(meta.bytes_in)
    const bytesOut = event.bytes_out ?? pickNumber(meta.bytes_out)
    if (typeof bytesIn === 'number') {
      chips.push({ label: '↓', value: formatBytes(bytesIn), accent: true })
    }
    if (typeof bytesOut === 'number') {
      chips.push({ label: '↑', value: formatBytes(bytesOut), accent: true })
    }
  }

  const entityType = event.entity?.type || event.entity_type
  const entityId = event.entity?.id || event.entity_id
  if (chips.length === 0 && entityType && entityId) {
    chips.push({ label: entityType, value: entityId.slice(0, 8) })
  }

  if (chips.length === 0) return null

  return (
    <>
      {chips.map((c, i) => (
        <span key={i} className="inline-flex items-center gap-1 text-[10px] font-mono">
          <span className="text-text-tertiary opacity-60">{c.label}</span>
          <span
            className={
              c.accent
                ? 'text-accent font-semibold'
                : c.highlight
                  ? 'text-accent'
                  : 'text-text-secondary'
            }
          >
            {c.value}
          </span>
        </span>
      ))}
    </>
  )
}
