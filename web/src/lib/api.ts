import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth'

export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError<{ error?: string }>) => {
    if (error.response?.status === 401) {
      const authStore = useAuthStore.getState()
      if (authStore.refreshToken) {
        try {
          const res = await axios.post('/api/v1/auth/refresh', {
            refresh_token: authStore.refreshToken,
          })
          authStore.setTokens(res.data.data.access_token, res.data.data.refresh_token)
          if (error.config) {
            error.config.headers.Authorization = `Bearer ${res.data.data.access_token}`
            return api(error.config)
          }
        } catch {
          authStore.logout()
          window.location.href = '/login'
        }
      } else {
        authStore.logout()
        window.location.href = '/login'
      }
    }

    const message = error.response?.data?.error || error.message || 'An error occurred'
    toast.error(message)
    return Promise.reject(error)
  },
)
