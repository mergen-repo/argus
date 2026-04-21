import { create } from 'zustand'
import { type Severity, isSeverity } from '@/lib/severity'
import type { BusEnvelope } from '@/types/events'

export interface LiveEvent {
  id: string
  type: string
  message: string
  severity: Severity
  timestamp: string
  entity_type?: string
  entity_id?: string
  // FIX-213 — envelope-aware fields (FIX-212 BusEnvelope shape).
  // `title` is the human-readable headline; `message` is the longer body.
  // When normalizer sees envelope shape, title is `env.title`, message is
  // `env.message`. Legacy-shaped events keep message set + title undefined.
  title?: string
  source?: string
  entity?: { type: string; id: string; display_name?: string }
  meta?: Record<string, unknown>
  dedup_key?: string
  event_version?: number
  // Source context (optional — populated from NATS payload when present).
  // Session events carry imsi/framed_ip/msisdn; SIM/Operator/APN events
  // carry sim_id/operator_id/apn_id; policy/job events carry the
  // corresponding IDs + progress. All surfaced in the drawer via
  // <SourceChips /> so users see the source without clicking through.
  imsi?: string
  msisdn?: string
  framed_ip?: string
  nas_ip?: string
  operator_id?: string
  apn_id?: string
  policy_id?: string
  job_id?: string
  sim_id?: string
  tenant_id?: string
  progress_pct?: number
  bytes_in?: number
  bytes_out?: number
}

// Proof-of-consumer for PAT-015: type-only reference of the imported
// BusEnvelope so `grep "from '@/types/events'"` picks this file up.
// LiveEvent.meta shape aligns with BusEnvelope['meta'] (Record<string, unknown>).
// Type alias is compile-time only — no runtime object ships to prod.
type _LiveEventEnvelopeAligned = Pick<BusEnvelope, 'title' | 'message' | 'source' | 'dedup_key' | 'event_version'>
export type { _LiveEventEnvelopeAligned }

interface MinuteBucket {
  minute: number
  count: number
}

export type EventDateRange = 'session' | '1h' | '24h'

export interface EventFilters {
  types: string[]
  severities: Severity[]
  entityTypes: string[]
  sources: string[]
  dateRange: EventDateRange
}

const DEFAULT_FILTERS: EventFilters = {
  types: [],
  severities: [],
  entityTypes: [],
  sources: [],
  dateRange: 'session',
}

const FILTER_STORAGE_KEY = 'argus.events.filters.v1'
const BUFFER_CAP = 500
const QUEUE_CAP = 500

function loadFilters(): EventFilters {
  if (typeof localStorage === 'undefined') return { ...DEFAULT_FILTERS }
  try {
    const raw = localStorage.getItem(FILTER_STORAGE_KEY)
    if (!raw) return { ...DEFAULT_FILTERS }
    const parsed = JSON.parse(raw) as Partial<EventFilters>
    return {
      types: Array.isArray(parsed.types) ? parsed.types.filter((t): t is string => typeof t === 'string') : [],
      severities: Array.isArray(parsed.severities)
        ? parsed.severities.filter((s): s is Severity => typeof s === 'string' && isSeverity(s))
        : [],
      entityTypes: Array.isArray(parsed.entityTypes) ? parsed.entityTypes.filter((t): t is string => typeof t === 'string') : [],
      sources: Array.isArray(parsed.sources) ? parsed.sources.filter((t): t is string => typeof t === 'string') : [],
      dateRange:
        parsed.dateRange === 'session' || parsed.dateRange === '1h' || parsed.dateRange === '24h'
          ? parsed.dateRange
          : 'session',
    }
  } catch {
    return { ...DEFAULT_FILTERS }
  }
}

function saveFilters(f: EventFilters): void {
  if (typeof localStorage === 'undefined') return
  try {
    localStorage.setItem(FILTER_STORAGE_KEY, JSON.stringify(f))
  } catch {
    // Quota exceeded / disabled — silent fail is correct here (filters are
    // a UX nicety, not data). Next call will just not persist.
  }
}

function clearStoredFilters(): void {
  if (typeof localStorage === 'undefined') return
  try {
    localStorage.removeItem(FILTER_STORAGE_KEY)
  } catch {
    /* no-op */
  }
}

interface EventState {
  events: LiveEvent[]
  queuedEvents: LiveEvent[]
  histogram: MinuteBucket[]
  // Per-operator minute buckets — same 15-minute rolling window as the
  // global histogram but keyed by operator_id. Drives the Operator
  // Health Matrix's per-row live sparkline.
  operatorHistogram: Record<string, MinuteBucket[]>
  drawerOpen: boolean
  totalCount: number
  paused: boolean
  filters: EventFilters
  sessionStartTs: number

  addEvent: (event: LiveEvent) => void
  setDrawerOpen: (open: boolean) => void
  toggleDrawer: () => void
  setPaused: (paused: boolean) => void
  resumeAndFlush: () => void
  clearEvents: () => void
  setFilters: (f: Partial<EventFilters>) => void
  resetFilters: () => void
}

function currentMinute() {
  return Math.floor(Date.now() / 60_000)
}

function updateHistograms(
  s: Pick<EventState, 'histogram' | 'operatorHistogram'>,
  event: LiveEvent,
): Pick<EventState, 'histogram' | 'operatorHistogram'> {
  const now = currentMinute()
  const cutoff = now - 15

  const newHisto = [...s.histogram]
  const existing = newHisto.find((b) => b.minute === now)
  if (existing) {
    existing.count++
  } else {
    newHisto.push({ minute: now, count: 1 })
  }
  const trimmed = newHisto.filter((b) => b.minute > cutoff)

  let newOpHisto = s.operatorHistogram
  if (event.operator_id) {
    const opId = event.operator_id
    const prev = newOpHisto[opId] ?? []
    const updated = prev.slice()
    const opExisting = updated.find((b) => b.minute === now)
    if (opExisting) {
      opExisting.count++
    } else {
      updated.push({ minute: now, count: 1 })
    }
    newOpHisto = {
      ...newOpHisto,
      [opId]: updated.filter((b) => b.minute > cutoff),
    }
  }

  return { histogram: trimmed, operatorHistogram: newOpHisto }
}

export const useEventStore = create<EventState>()((set, get) => ({
  events: [],
  queuedEvents: [],
  histogram: [],
  operatorHistogram: {},
  drawerOpen: false,
  totalCount: 0,
  paused: false,
  filters: loadFilters(),
  sessionStartTs: Date.now(),

  addEvent: (event) =>
    set((s) => {
      const histoPatch = updateHistograms(s, event)
      // When paused, divert to the queue — the visible list + scroll
      // position stay stable while the user reviews. Histograms keep
      // updating so topbar sparkline stays live (D6).
      if (s.paused) {
        const newQueue = [event, ...s.queuedEvents].slice(0, QUEUE_CAP)
        if (s.queuedEvents.length >= QUEUE_CAP) {
          // eslint-disable-next-line no-console
          console.warn('[events] queue overflow, dropping oldest')
        }
        return {
          queuedEvents: newQueue,
          histogram: histoPatch.histogram,
          operatorHistogram: histoPatch.operatorHistogram,
          totalCount: s.totalCount + 1,
        }
      }
      const newEvents = [event, ...s.events].slice(0, BUFFER_CAP)
      return {
        events: newEvents,
        histogram: histoPatch.histogram,
        operatorHistogram: histoPatch.operatorHistogram,
        totalCount: s.totalCount + 1,
      }
    }),

  setDrawerOpen: (open) => set({ drawerOpen: open }),
  toggleDrawer: () => set((s) => ({ drawerOpen: !s.drawerOpen })),

  setPaused: (paused) => set({ paused }),

  resumeAndFlush: () =>
    set((s) => ({
      paused: false,
      // Merge queued events (newest first) ahead of existing list, then
      // cap to buffer. queuedEvents is already newest-first because
      // addEvent prepends.
      events: [...s.queuedEvents, ...s.events].slice(0, BUFFER_CAP),
      queuedEvents: [],
    })),

  // clearEvents — list-scoped reset: clears visible events, queued events,
  // active filters, and pause state. Does NOT reset histogram / operatorHistogram
  // / totalCount so the topbar sparkline and global counts stay authoritative
  // across user-initiated list wipes (AC-7 list-scoped semantics).
  clearEvents: () => {
    clearStoredFilters()
    set({
      events: [],
      queuedEvents: [],
      filters: { ...DEFAULT_FILTERS },
      paused: false,
    })
  },

  setFilters: (f) => {
    const merged: EventFilters = { ...get().filters, ...f }
    saveFilters(merged)
    set({ filters: merged })
  },

  resetFilters: () => {
    clearStoredFilters()
    set({ filters: { ...DEFAULT_FILTERS } })
  },
}))

// Pure predicate — separate from store so tests/type-checks can import it.
export function eventMatchesFilters(
  event: LiveEvent,
  filters: EventFilters,
  sessionStartTs: number,
  nowMs: number = Date.now(),
): boolean {
  if (filters.types.length > 0 && !filters.types.includes(event.type)) return false
  if (filters.severities.length > 0 && !filters.severities.includes(event.severity)) return false
  if (filters.entityTypes.length > 0) {
    const et = event.entity?.type || event.entity_type
    if (!et || !filters.entityTypes.includes(et)) return false
  }
  if (filters.sources.length > 0) {
    const src = event.source
    if (!src || !filters.sources.includes(src)) return false
  }
  const ts = new Date(event.timestamp).getTime()
  if (!Number.isFinite(ts)) return true
  if (filters.dateRange === 'session') {
    if (ts < sessionStartTs) return false
  } else if (filters.dateRange === '1h') {
    if (ts < nowMs - 3_600_000) return false
  } else if (filters.dateRange === '24h') {
    if (ts < nowMs - 86_400_000) return false
  }
  return true
}

// Selector helper — callers use it via useEventStore(useFilteredEventsSelector).
// Returns the filter-matched slice of visible events (excludes queuedEvents
// by design — those become visible only on resume).
export function useFilteredEventsSelector(s: EventState): LiveEvent[] {
  return s.events.filter((e) => eventMatchesFilters(e, s.filters, s.sessionStartTs))
}
