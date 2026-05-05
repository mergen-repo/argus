// STORY-098 Task 6 — TanStack Query hooks for SCR-198 Log Forwarding.
// Backend endpoints (internal/api/settings/log_forwarding.go):
//   GET    /settings/log-forwarding              — List
//   POST   /settings/log-forwarding              — Upsert (create or update by name)
//   POST   /settings/log-forwarding/{id}/enabled — SetEnabled (toggle)
//   DELETE /settings/log-forwarding/{id}         — Delete
//   POST   /settings/log-forwarding/test         — Test connection (no DB write)

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'
import type {
  SyslogDestination,
  UpsertSyslogDestinationRequest,
  TestSyslogConnectionRequest,
  TestSyslogConnectionResponse,
} from '@/types/log-forwarding'

const LOG_FORWARDING_KEY = ['log-forwarding'] as const

export function useLogForwardingList() {
  return useQuery({
    queryKey: [...LOG_FORWARDING_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<SyslogDestination>>('/settings/log-forwarding')
      return res.data.data
    },
    staleTime: 15_000,
  })
}

export function useLogForwardingUpsert() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: UpsertSyslogDestinationRequest) => {
      const res = await api.post<ApiResponse<SyslogDestination>>('/settings/log-forwarding', payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...LOG_FORWARDING_KEY, 'list'] })
    },
  })
}

export function useLogForwardingSetEnabled() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      const res = await api.post<ApiResponse<SyslogDestination>>(
        `/settings/log-forwarding/${id}/enabled`,
        { enabled },
      )
      return res.data.data
    },
    onMutate: async ({ id, enabled }) => {
      await queryClient.cancelQueries({ queryKey: [...LOG_FORWARDING_KEY, 'list'] })
      const previous = queryClient.getQueryData<SyslogDestination[]>([...LOG_FORWARDING_KEY, 'list'])
      if (previous) {
        queryClient.setQueryData<SyslogDestination[]>(
          [...LOG_FORWARDING_KEY, 'list'],
          previous.map((d) => (d.id === id ? { ...d, enabled } : d)),
        )
      }
      return { previous }
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        queryClient.setQueryData([...LOG_FORWARDING_KEY, 'list'], ctx.previous)
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: [...LOG_FORWARDING_KEY, 'list'] })
    },
  })
}

export function useLogForwardingDelete() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/settings/log-forwarding/${id}`)
      return id
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...LOG_FORWARDING_KEY, 'list'] })
    },
  })
}

export function useLogForwardingTest() {
  return useMutation({
    mutationFn: async (payload: TestSyslogConnectionRequest) => {
      const res = await api.post<ApiResponse<TestSyslogConnectionResponse>>(
        '/settings/log-forwarding/test',
        payload,
      )
      return res.data.data
    },
  })
}
