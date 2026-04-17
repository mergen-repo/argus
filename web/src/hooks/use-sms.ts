import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'

const SMS_KEY = ['sms'] as const

export interface SMSOutbound {
  id: string
  sim_id: string
  msisdn: string
  text_hash: string
  text_preview: string
  status: string
  provider_message_id?: string
  error_code?: string
  queued_at: string
  sent_at?: string
  delivered_at?: string
}

export function useSendSMS() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (req: { sim_id: string; text: string; priority?: string }) => {
      const res = await api.post<ApiResponse<SMSOutbound>>('/sms/send', req)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SMS_KEY })
    },
  })
}

export function useSMSHistory(filters: { sim_id?: string; status?: string } = {}, cursor: string = '', limit: number = 50) {
  return useQuery({
    queryKey: [...SMS_KEY, 'history', filters, cursor, limit],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (filters.sim_id) params.set('sim_id', filters.sim_id)
      if (filters.status) params.set('status', filters.status)
      if (cursor) params.set('cursor', cursor)
      params.set('limit', String(limit))
      const res = await api.get<ListResponse<SMSOutbound>>(`/sms/history?${params.toString()}`)
      return res.data
    },
    staleTime: 15_000,
  })
}
