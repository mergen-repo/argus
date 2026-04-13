import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

export interface SearchResult {
  id: string
  label: string
  sub?: string
  [key: string]: unknown
}

export interface SIMResult extends SearchResult {
  state: string
  operator_name: string
}

export interface APNResult extends SearchResult {
  mcc: string
  operator_name: string
}

export interface OperatorResult extends SearchResult {
  mcc: string
  health_status: string
}

export interface PolicyResult extends SearchResult {
  state: string
}

export interface UserResult extends SearchResult {
  role: string
}

export interface SearchResults {
  sims: SIMResult[]
  apns: APNResult[]
  operators: OperatorResult[]
  policies: PolicyResult[]
  users: UserResult[]
}

interface SearchParams {
  q: string
  types?: string[]
  limit?: number
  enabled?: boolean
}

export function useSearch({ q, types, limit = 5, enabled = true }: SearchParams) {
  const trimmed = q.trim()

  return useQuery({
    queryKey: ['search', trimmed, types, limit],
    queryFn: async () => {
      const params = new URLSearchParams({ q: trimmed })
      if (types && types.length > 0) params.set('types', types.join(','))
      if (limit) params.set('limit', String(limit))
      const res = await api.get<{ status: string; data: SearchResults }>(
        `/search?${params.toString()}`,
      )
      return res.data.data
    },
    enabled: enabled && trimmed.length >= 2,
    staleTime: 30_000,
    placeholderData: (prev) => prev,
  })
}
