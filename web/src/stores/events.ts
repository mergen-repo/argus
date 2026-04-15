import { create } from 'zustand'

export interface LiveEvent {
  id: string
  type: string
  message: string
  severity: 'critical' | 'warning' | 'info'
  timestamp: string
  entity_type?: string
  entity_id?: string
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
}

interface MinuteBucket {
  minute: number
  count: number
}

interface EventState {
  events: LiveEvent[]
  histogram: MinuteBucket[]
  drawerOpen: boolean
  totalCount: number

  addEvent: (event: LiveEvent) => void
  setDrawerOpen: (open: boolean) => void
  toggleDrawer: () => void
}

function currentMinute() {
  return Math.floor(Date.now() / 60_000)
}

export const useEventStore = create<EventState>()((set) => ({
  events: [],
  histogram: [],
  drawerOpen: false,
  totalCount: 0,

  addEvent: (event) =>
    set((s) => {
      const now = currentMinute()
      const newEvents = [event, ...s.events].slice(0, 100)

      const newHisto = [...s.histogram]
      const existing = newHisto.find((b) => b.minute === now)
      if (existing) {
        existing.count++
      } else {
        newHisto.push({ minute: now, count: 1 })
      }
      const cutoff = now - 15
      const trimmed = newHisto.filter((b) => b.minute > cutoff)

      return {
        events: newEvents,
        histogram: trimmed,
        totalCount: s.totalCount + 1,
      }
    }),

  setDrawerOpen: (open) => set({ drawerOpen: open }),
  toggleDrawer: () => set((s) => ({ drawerOpen: !s.drawerOpen })),
}))
