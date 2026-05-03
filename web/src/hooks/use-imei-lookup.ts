import { useQuery, type UseQueryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  IMEILookupApiEnvelope,
  IMEILookupResult,
} from '@/types/imei-lookup'
import { isValidIMEI } from '@/types/imei-lookup'

const IMEI_LOOKUP_KEY = ['imei-lookup'] as const

export function imeiLookupQueryKey(imei: string) {
  return [...IMEI_LOOKUP_KEY, imei] as const
}

export interface UseIMEILookupOptions
  extends Omit<UseQueryOptions<IMEILookupResult, Error>, 'queryKey' | 'queryFn'> {
  /**
   * When false, the query is disabled regardless of IMEI validity.
   * Useful for gating the lookup behind a "submitted" UI state.
   */
  enabled?: boolean
}

export function useIMEILookup(imei: string, options?: UseIMEILookupOptions) {
  const valid = isValidIMEI(imei)
  return useQuery<IMEILookupResult, Error>({
    queryKey: imeiLookupQueryKey(imei),
    queryFn: async () => {
      const params = new URLSearchParams({ imei })
      const res = await api.get<IMEILookupApiEnvelope>(
        `/imei-pools/lookup?${params.toString()}`,
      )
      return res.data.data
    },
    enabled: valid && (options?.enabled ?? true),
    staleTime: 30_000,
    retry: false,
    ...options,
  })
}
