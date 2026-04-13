import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { Operator, OperatorHealthDetail, OperatorTestResult } from '@/types/operator'
import type { ListResponse, ApiResponse } from '@/types/sim'

const OPERATORS_KEY = ['operators'] as const

export function useOperatorList() {
  return useQuery({
    queryKey: [...OPERATORS_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<Operator>>('/operators?limit=100')
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useOperator(id: string) {
  return useQuery({
    queryKey: [...OPERATORS_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ListResponse<Operator>>('/operators?limit=100')
      const op = res.data.data.find((o) => o.id === id)
      if (!op) throw new Error('Operator not found')
      return op
    },
    enabled: !!id,
    staleTime: 15_000,
  })
}

export function useOperatorHealth(id: string) {
  return useQuery({
    queryKey: [...OPERATORS_KEY, 'health', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<OperatorHealthDetail>>(`/operators/${id}/health`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
}

export function useTestConnection(id: string) {
  return useMutation({
    mutationFn: async () => {
      const res = await api.post<ApiResponse<OperatorTestResult>>(`/operators/${id}/test`)
      return res.data.data
    },
  })
}

export interface CreateOperatorData {
  name: string
  code: string
  mcc: string
  mnc: string
  adapter_type: string
  supported_rat_types: string[]
  health_check_interval_sec?: number
  failover_policy?: string
  failover_timeout_ms?: number
  circuit_breaker_threshold?: number
  circuit_breaker_recovery_sec?: number
  sla_uptime_target?: number
}

export function useCreateOperator() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: CreateOperatorData) => {
      const res = await api.post<ApiResponse<Operator>>('/operators', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: OPERATORS_KEY })
    },
  })
}

export function useUpdateOperator(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: Partial<CreateOperatorData>) => {
      const res = await api.patch<ApiResponse<Operator>>(`/operators/${id}`, data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: OPERATORS_KEY })
    },
  })
}

export function useOperatorGrants() {
  return useQuery({
    queryKey: [...OPERATORS_KEY, 'grants'],
    queryFn: async () => {
      const res = await api.get<ListResponse<{ id: string; tenant_id: string; operator_id: string; supported_rat_types: string[]; created_at: string }>>('/operator-grants?limit=200')
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useAssignOperator() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: { operator_id: string; supported_rat_types?: string[] }) => {
      const res = await api.post<ApiResponse<{ id: string }>>('/operator-grants', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: OPERATORS_KEY })
    },
  })
}

export function useRemoveOperatorGrant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (grantId: string) => {
      await api.delete(`/operator-grants/${grantId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: OPERATORS_KEY })
    },
  })
}

export function useRealtimeOperatorHealth() {
  const queryClient = useQueryClient()

  const handler = useCallback(
    (data: unknown) => {
      const event = data as { operator_id?: string; health_status?: string }
      if (!event.operator_id) return
      queryClient.invalidateQueries({ queryKey: [...OPERATORS_KEY, 'list'] })
      queryClient.invalidateQueries({ queryKey: [...OPERATORS_KEY, 'health', event.operator_id] })
      queryClient.invalidateQueries({ queryKey: [...OPERATORS_KEY, 'detail', event.operator_id] })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('operator.health_changed', handler)
    return unsub
  }, [handler])
}
