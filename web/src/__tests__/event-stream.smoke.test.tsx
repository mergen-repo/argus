/**
 * FIX-213 smoke tests — Live Event Stream.
 * Type-level + structural validation via tsc --noEmit (no runtime runner).
 */
import {
  eventMatchesFilters,
  type EventFilters,
  type EventDateRange,
  type LiveEvent,
} from '@/stores/events'
import { formatRelativeTime } from '@/lib/format'
import type { Severity } from '@/lib/severity'
import type { BusEnvelope, EntityRef, EventSeverity } from '@/types/events'

// ---------- PAT-015 consumer proof — @/types/events ------------------------
const _severity: EventSeverity = 'critical'
void _severity
const _entity: EntityRef = { type: 'operator', id: 'abc', display_name: 'Turkcell' }
void _entity
const _env: BusEnvelope<{ bytes_in?: number }> = {
  event_version: 1,
  id: 'x',
  type: 'session.updated',
  timestamp: new Date().toISOString(),
  tenant_id: 't1',
  severity: 'info',
  source: 'aaa',
  title: 'Session updated',
  meta: { bytes_in: 42 },
}
void _env

// ---------- Filter predicate coverage -------------------------------------
const SESSION_START = Date.now() - 60_000
const NOW = Date.now()

function mkEvt(overrides: Partial<LiveEvent>): LiveEvent {
  return {
    id: overrides.id || 'evt-1',
    type: overrides.type || 'session.started',
    message: overrides.message || 'ok',
    severity: (overrides.severity || 'info') as Severity,
    timestamp: overrides.timestamp || new Date(NOW).toISOString(),
    ...overrides,
  }
}

function mkFilters(overrides: Partial<EventFilters>): EventFilters {
  return {
    types: [],
    severities: [],
    entityTypes: [],
    sources: [],
    dateRange: 'session',
    ...overrides,
  }
}

// Case 1 — empty filters match all events
const _case1 = eventMatchesFilters(
  mkEvt({ type: 'session.started' }),
  mkFilters({}),
  SESSION_START,
  NOW,
)
void _case1

// Case 2 — type filter positive match
const _case2 = eventMatchesFilters(
  mkEvt({ type: 'session.ended' }),
  mkFilters({ types: ['session.ended'] }),
  SESSION_START,
  NOW,
)
void _case2

// Case 3 — type filter negative
const _case3 = eventMatchesFilters(
  mkEvt({ type: 'session.started' }),
  mkFilters({ types: ['session.ended'] }),
  SESSION_START,
  NOW,
)
void _case3

// Case 4 — severity filter
const _case4 = eventMatchesFilters(
  mkEvt({ severity: 'critical' as Severity }),
  mkFilters({ severities: ['critical'] }),
  SESSION_START,
  NOW,
)
void _case4

// Case 5 — entityType via envelope entity
const _case5 = eventMatchesFilters(
  mkEvt({ entity: { type: 'operator', id: 'op-1' } }),
  mkFilters({ entityTypes: ['operator'] }),
  SESSION_START,
  NOW,
)
void _case5

// Case 6 — date range '1h' filters out older events
const _case6 = eventMatchesFilters(
  mkEvt({ timestamp: new Date(NOW - 2 * 3_600_000).toISOString() }),
  mkFilters({ dateRange: '1h' as EventDateRange }),
  SESSION_START,
  NOW,
)
void _case6

// Case 7 — source filter
const _case7 = eventMatchesFilters(
  mkEvt({ source: 'policy' }),
  mkFilters({ sources: ['policy'] }),
  SESSION_START,
  NOW,
)
void _case7

// ---------- formatRelativeTime coverage -----------------------------------
const _ft1: string = formatRelativeTime(new Date(Date.now() - 5_000).toISOString()) // 'şimdi'
const _ft2: string = formatRelativeTime(new Date(Date.now() - 30_000).toISOString()) // '30sn önce'
const _ft3: string = formatRelativeTime(new Date(Date.now() - 65_000).toISOString()) // '1dk önce'
const _ft4: string = formatRelativeTime(new Date(Date.now() - 3_700_000).toISOString()) // '1sa önce'
const _ft5: string = formatRelativeTime('not-an-iso') // ''
void _ft1
void _ft2
void _ft3
void _ft4
void _ft5

// ---------- LiveEvent envelope fields present -----------------------------
const _envEvt: LiveEvent = {
  id: 'e1',
  type: 'alert.triggered',
  message: 'body',
  severity: 'high' as Severity,
  timestamp: new Date().toISOString(),
  title: 'Hello',
  source: 'operator',
  entity: { type: 'operator', id: 'x', display_name: 'Turkcell' },
  meta: { alert_id: 'a1', bytes_in: 100 },
  dedup_key: 'd',
  event_version: 1,
}
void _envEvt

export { _case1, _case2, _case3, _case4, _case5, _case6, _case7 }
