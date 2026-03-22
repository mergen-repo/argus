import axios, { type AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth'

export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
  withCredentials: true,
})

api.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let isRefreshing = false
let failedQueue: Array<{
  resolve: (token: string) => void
  reject: (error: unknown) => void
}> = []

function processQueue(error: unknown, token: string | null) {
  failedQueue.forEach(({ resolve, reject }) => {
    if (error) {
      reject(error)
    } else {
      resolve(token!)
    }
  })
  failedQueue = []
}

api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError<{ error?: { message?: string; code?: string; details?: Array<Record<string, unknown>> } }>) => {
    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean }

    if (error.response?.status === 401 && !originalRequest._retry) {
      if (isRefreshing) {
        return new Promise((resolve, reject) => {
          failedQueue.push({
            resolve: (token: string) => {
              originalRequest.headers.Authorization = `Bearer ${token}`
              resolve(api(originalRequest))
            },
            reject,
          })
        })
      }

      originalRequest._retry = true
      isRefreshing = true

      try {
        const res = await axios.post('/api/v1/auth/refresh', {}, { withCredentials: true })
        const newToken = res.data.data.token
        useAuthStore.getState().setToken(newToken)
        processQueue(null, newToken)
        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return api(originalRequest)
      } catch (refreshError) {
        processQueue(refreshError, null)
        useAuthStore.getState().logout()
        window.location.href = '/login'
        return Promise.reject(refreshError)
      } finally {
        isRefreshing = false
      }
    }

    if (error.response?.status === 423) {
      return Promise.reject(error)
    }

    const errorData = error.response?.data?.error
    const message = errorData?.message || error.message || 'An error occurred'

    if (error.response?.status !== 401) {
      toast.error(message)
    }

    return Promise.reject(error)
  },
)

export interface AuthLoginResponse {
  user: {
    id: string
    email: string
    name: string
    role: string
    onboarding_completed?: boolean
  }
  token: string
  requires_2fa: boolean
}

export interface AuthRefreshResponse {
  token: string
}

export interface Auth2FAResponse {
  token: string
}

export const authApi = {
  login: (email: string, password: string, rememberMe?: boolean) =>
    api.post<{ status: string; data: AuthLoginResponse }>('/auth/login', {
      email,
      password,
      remember_me: rememberMe,
    }),

  verify2FA: (code: string) => {
    const partialToken = useAuthStore.getState().partialToken
    return api.post<{ status: string; data: Auth2FAResponse }>(
      '/auth/2fa/verify',
      { code },
      { headers: { Authorization: `Bearer ${partialToken}` } },
    )
  },

  refresh: () =>
    api.post<{ status: string; data: AuthRefreshResponse }>('/auth/refresh'),

  logout: () => api.post('/auth/logout'),
}
