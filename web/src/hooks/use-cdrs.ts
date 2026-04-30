import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

export interface CDR {
  id: number
  session_id: string
  sim_id: string
  operator_id: string
  apn_id?: string
  rat_type?: string
  record_type: string
  bytes_in: number
  bytes_out: number
  duration_sec: number
  usage_cost: string | null
  carrier_cost: string | null
  rate_per_mb: string | null
  rat_multiplier: string | null
  timestamp: string
}

export interface CDRStats {
  total_count: number
  total_bytes_in: number
  total_bytes_out: number
  total_cost: number
  unique_sims: number
  unique_sessions: number
}

export interface CDRFilters {
  sim_id?: string
  operator_id?: string
  apn_id?: string
  session_id?: string
  record_type?: string
  rat_type?: string
  from?: string
  to?: string
}

interface ListMeta {
  cursor?: string
  limit?: number
  has_more?: boolean
}

interface ListResponse<T> {
  status: string
  data: T[]
  meta?: ListMeta
}

interface ApiResponse<T> {
  status: string
  data: T
}

interface SimByIDDTO {
  id: string
  iccid: string
  imsi: string
  msisdn?: string
  state: string
  sim_type: string
}

function toQueryParams(filters: CDRFilters, cursor?: string, limit?: number): URLSearchParams {
  const p = new URLSearchParams()
  if (filters.sim_id) p.set('sim_id', filters.sim_id)
  if (filters.operator_id) p.set('operator_id', filters.operator_id)
  if (filters.apn_id) p.set('apn_id', filters.apn_id)
  if (filters.session_id) p.set('session_id', filters.session_id)
  if (filters.record_type) p.set('record_type', filters.record_type)
  if (filters.rat_type) p.set('rat_type', filters.rat_type)
  if (filters.from) p.set('from', filters.from)
  if (filters.to) p.set('to', filters.to)
  if (cursor) p.set('cursor', cursor)
  if (limit) p.set('limit', String(limit))
  return p
}

export function useCDRList(filters: CDRFilters, pageSize = 50, enabled = true) {
  return useInfiniteQuery({
    queryKey: ['cdrs', 'list', filters, pageSize],
    enabled: enabled && Boolean(filters.from) && Boolean(filters.to),
    initialPageParam: '',
    queryFn: async ({ pageParam }) => {
      const p = toQueryParams(filters, pageParam as string, pageSize)
      const res = await api.get<ListResponse<CDR>>(`/cdrs?${p.toString()}`)
      return res.data
    },
    getNextPageParam: (last) => last.meta?.has_more ? last.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useCDRStats(filters: CDRFilters, enabled = true) {
  return useQuery({
    queryKey: ['cdrs', 'stats', filters],
    enabled: enabled && Boolean(filters.from) && Boolean(filters.to),
    queryFn: async () => {
      const p = toQueryParams(filters)
      const res = await api.get<ApiResponse<CDRStats>>(`/cdrs/stats?${p.toString()}`)
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export interface SessionTimeline {
  session_id: string
  count: number
  items: CDR[]
  stats?: {
    total_bytes_in: number
    total_bytes_out: number
    total_cost: number
    duration_sec: number
  }
}

export function useSessionTimeline(sessionID: string | undefined) {
  return useQuery({
    queryKey: ['cdrs', 'by-session', sessionID],
    enabled: Boolean(sessionID),
    queryFn: async () => {
      const res = await api.get<ApiResponse<SessionTimeline>>(`/cdrs/by-session/${sessionID}`)
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useSimBatch(ids: string[]) {
  const uniq = Array.from(new Set(ids.filter(Boolean))).sort()
  const key = uniq.join(',')
  return useQuery({
    queryKey: ['sims', 'batch', key],
    enabled: uniq.length > 0,
    queryFn: async () => {
      const res = await api.get<ListResponse<SimByIDDTO>>(`/sims?ids=${encodeURIComponent(key)}`)
      const map: Record<string, SimByIDDTO> = {}
      for (const s of res.data.data ?? []) {
        map[s.id] = s
      }
      return map
    },
    staleTime: 5 * 60_000,
  })
}
