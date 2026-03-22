import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback, useRef } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { Session, SessionStats, SessionStartedEvent, SessionEndedEvent } from '@/types/session'
import type { ListResponse, ApiResponse } from '@/types/sim'

const SESSIONS_KEY = ['sessions'] as const

export function useSessionList(filters: { operator_id?: string; apn_id?: string }) {
  return useInfiniteQuery({
    queryKey: [...SESSIONS_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (filters.operator_id) params.set('operator_id', filters.operator_id)
      if (filters.apn_id) params.set('apn_id', filters.apn_id)
      const res = await api.get<ListResponse<Session>>(`/sessions?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 10_000,
    refetchInterval: 30_000,
  })
}

export function useSessionStats() {
  return useQuery({
    queryKey: [...SESSIONS_KEY, 'stats'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<SessionStats>>('/sessions/stats')
      return res.data.data
    },
    staleTime: 15_000,
    refetchInterval: 15_000,
  })
}

export function useDisconnectSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ sessionId, reason }: { sessionId: string; reason?: string }) => {
      const res = await api.post(`/sessions/${sessionId}/disconnect`, reason ? { reason } : {})
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SESSIONS_KEY })
    },
  })
}

export function useRealtimeSessionStarted() {
  const queryClient = useQueryClient()
  const newSessionIdsRef = useRef<Set<string>>(new Set())

  const handler = useCallback(
    (data: unknown) => {
      const event = data as SessionStartedEvent
      if (!event.session_id) return

      newSessionIdsRef.current.add(event.session_id)
      setTimeout(() => newSessionIdsRef.current.delete(event.session_id), 3000)

      const newSession: Session = {
        id: event.session_id,
        sim_id: event.sim_id,
        tenant_id: '',
        operator_id: event.operator_id,
        apn_id: event.apn_id,
        imsi: event.imsi,
        msisdn: event.msisdn,
        acct_session_id: '',
        nas_ip: event.nas_ip,
        rat_type: event.rat_type,
        state: 'active',
        bytes_in: 0,
        bytes_out: 0,
        duration_sec: 0,
        ip_address: event.ip_address,
        started_at: event.started_at,
      }

      queryClient.setQueryData<{ pages: ListResponse<Session>[]; pageParams: string[] }>(
        [...SESSIONS_KEY, 'list', {}],
        (old) => {
          if (!old || !old.pages || old.pages.length === 0) return old
          const firstPage = old.pages[0]
          return {
            ...old,
            pages: [
              { ...firstPage, data: [newSession, ...firstPage.data] },
              ...old.pages.slice(1),
            ],
          }
        },
      )

      queryClient.invalidateQueries({ queryKey: [...SESSIONS_KEY, 'stats'] })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('session.started', handler)
    return unsub
  }, [handler])

  return newSessionIdsRef
}

export function useRealtimeSessionEnded() {
  const queryClient = useQueryClient()
  const endedSessionIdsRef = useRef<Set<string>>(new Set())

  const handler = useCallback(
    (data: unknown) => {
      const event = data as SessionEndedEvent
      if (!event.session_id) return

      endedSessionIdsRef.current.add(event.session_id)

      setTimeout(() => {
        endedSessionIdsRef.current.delete(event.session_id)
        queryClient.invalidateQueries({ queryKey: [...SESSIONS_KEY, 'list'] })
      }, 2000)

      queryClient.invalidateQueries({ queryKey: [...SESSIONS_KEY, 'stats'] })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('session.ended', handler)
    return unsub
  }, [handler])

  return endedSessionIdsRef
}
