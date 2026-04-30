import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { queryClient } from '@/lib/query'
import { useAuthStore } from '@/stores/auth'
import { wsClient } from '@/lib/ws'
import type { ApiResponse } from '@/types/sim'

interface SwitchTenantResponse {
  jwt: string
  user_id: string
  home_tenant_id: string
  active_tenant_id?: string | null
  role: string
}

// useSwitchTenant mints a new access token scoped to the target tenant.
// The new token carries `active_tenant` so the backend treats every
// subsequent tenant-scoped query as if it originated from that tenant.
// After switching we:
//   1. Replace the stored token (persists via zustand/persist)
//   2. Blow away ALL TanStack caches — otherwise the previous tenant's
//      data would leak into the new view for a few frames
//   3. Force a WebSocket reconnect so the /events stream re-authenticates
//      with the new claims (tenant isolation applies there too)
export function useSwitchTenant() {
  const setToken = useAuthStore((s) => s.setToken)

  return useMutation({
    mutationFn: async (tenantId: string) => {
      const res = await api.post<ApiResponse<SwitchTenantResponse>>(
        '/auth/switch-tenant',
        { tenant_id: tenantId },
      )
      return res.data.data
    },
    onSuccess: (data) => {
      setToken(data.jwt)
      queryClient.clear()
      wsClient.reconnectNow()
      toast.success('Tenant context switched')
    },
    onError: (err: unknown) => {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      toast.error(msg || 'Failed to switch tenant')
    },
  })
}

// useExitTenantContext clears the active tenant and returns the admin to
// System View. Same cache/WS invalidation behavior as useSwitchTenant.
// Idempotent — safe to call when no context is active.
export function useExitTenantContext() {
  const setToken = useAuthStore((s) => s.setToken)

  return useMutation({
    mutationFn: async () => {
      const res = await api.post<ApiResponse<SwitchTenantResponse>>(
        '/auth/exit-tenant-context',
        {},
      )
      return res.data.data
    },
    onSuccess: (data) => {
      setToken(data.jwt)
      queryClient.clear()
      wsClient.reconnectNow()
      toast.success('Returned to System View')
    },
    onError: (err: unknown) => {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      toast.error(msg || 'Failed to exit tenant context')
    },
  })
}
