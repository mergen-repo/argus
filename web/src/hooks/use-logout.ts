import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/lib/api'

export function useLogout() {
  const navigate = useNavigate()
  const logout = useAuthStore((s) => s.logout)

  return useCallback(async () => {
    try {
      await authApi.logout()
    } catch {
      // logout even if API call fails
    }
    logout()
    navigate('/login', { replace: true })
  }, [logout, navigate])
}
