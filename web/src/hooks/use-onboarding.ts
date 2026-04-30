import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

const ONBOARDING_KEY = ['onboarding'] as const

export interface OnboardingStatus {
  operator_configured: boolean
  apn_created: boolean
  sim_imported: boolean
  policy_created: boolean
}

export function useOnboarding() {
  return useQuery({
    queryKey: [...ONBOARDING_KEY, 'status'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<OnboardingStatus>>('/onboarding/status')
      return res.data.data
    },
    staleTime: 60_000,
  })
}
const SESSION_LS_KEY = 'argus_onboarding_session'

export interface OnboardingSession {
  session_id: string
  current_step: number
  steps_total?: number
  data_by_step?: Record<string, unknown>
  state?: string
  completed?: boolean
}

export function getStoredSessionID(): string | null {
  if (typeof window === 'undefined') return null
  return window.localStorage.getItem(SESSION_LS_KEY)
}

export function setStoredSessionID(id: string | null) {
  if (typeof window === 'undefined') return
  if (id) window.localStorage.setItem(SESSION_LS_KEY, id)
  else window.localStorage.removeItem(SESSION_LS_KEY)
}

export function useOnboardingSession(sessionID: string | null) {
  return useQuery({
    queryKey: [...ONBOARDING_KEY, 'session', sessionID],
    queryFn: async () => {
      const res = await api.get<ApiResponse<OnboardingSession>>(`/onboarding/${sessionID}`)
      return res.data.data
    },
    enabled: !!sessionID,
    staleTime: 0,
  })
}

export function useStartOnboarding() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const res = await api.post<ApiResponse<OnboardingSession>>('/onboarding/start')
      const session = res.data.data
      setStoredSessionID(session.session_id)
      return session
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ONBOARDING_KEY })
    },
  })
}

export function useSubmitOnboardingStep(sessionID: string | null) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ step, payload }: { step: number; payload: unknown }) => {
      const res = await api.post<ApiResponse<OnboardingSession>>(
        `/onboarding/${sessionID}/step/${step}`,
        payload,
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...ONBOARDING_KEY, 'session', sessionID] })
    },
  })
}

export function useCompleteOnboarding(sessionID: string | null) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const res = await api.post<ApiResponse<OnboardingSession>>(`/onboarding/${sessionID}/complete`)
      setStoredSessionID(null)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ONBOARDING_KEY })
    },
  })
}
