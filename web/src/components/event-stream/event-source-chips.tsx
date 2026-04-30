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

  // Name-aware priority chain (FIX-219 / AC-7):
  // P1: envelope entity display_name (FIX-212 shape)
  // P2: legacy name fields from meta bag
  // P3: UUID slice fallback
  const entityType = event.entity?.type
  const entityDisplayName = event.entity?.display_name

  function resolveId(id: string, matchType: string, metaNameKey: string): string {
    if (entityType === matchType && entityDisplayName) return entityDisplayName
    const metaName = pickString(meta[metaNameKey])
    if (metaName) return metaName
    return id.slice(0, 8)
  }

  if (event.operator_id && !imsi) chips.push({ label: 'Op', value: resolveId(event.operator_id, 'operator', 'operator_name') })
  if (event.apn_id && !imsi) chips.push({ label: 'APN', value: resolveId(event.apn_id, 'apn', 'apn_name') })
  if (event.policy_id) chips.push({ label: 'Policy', value: resolveId(event.policy_id, 'policy', 'policy_name') })
  if (event.job_id) chips.push({ label: 'Job', value: resolveId(event.job_id, 'job', 'job_name') })

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

  const fallbackEntityType = event.entity?.type || event.entity_type
  const fallbackEntityId = event.entity?.id || event.entity_id
  if (chips.length === 0 && fallbackEntityType && fallbackEntityId) {
    const fallbackValue = entityDisplayName || fallbackEntityId.slice(0, 8)
    chips.push({ label: fallbackEntityType, value: fallbackValue })
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
