import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'

interface ImpersonateResponse {
  jwt: string
  user: { id: string; email: string; name: string; role: string }
  tenant: { id: string; name: string }
}

export function useImpersonation() {
  const { token, setToken } = useAuthStore()

  const isImpersonating = (() => {
    if (!token) return false
    try {
      const payload = JSON.parse(atob(token.split('.')[1]))
      return !!payload.impersonated
    } catch {
      return false
    }
  })()

  const impersonatedBy = (() => {
    if (!token) return null
    try {
      const payload = JSON.parse(atob(token.split('.')[1]))
      return payload.act?.sub ?? null
    } catch {
      return null
    }
  })()

  const impersonate = useMutation({
    mutationFn: async (userId: string) => {
      const res = await api.post<{ status: string; data: ImpersonateResponse }>(
        `/admin/impersonate/${userId}`,
        {},
      )
      return res.data.data
    },
    onSuccess: (data) => {
      setToken(data.jwt)
      window.location.href = '/'
    },
  })

  const exitImpersonation = useMutation({
    mutationFn: async () => {
      const res = await api.post<{ status: string; data: { jwt: string } }>(
        '/admin/impersonate/exit',
        {},
      )
      return res.data.data
    },
    onSuccess: (data) => {
      setToken(data.jwt)
      window.location.href = '/'
    },
  })

  return { isImpersonating, impersonatedBy, impersonate, exitImpersonation }
}
