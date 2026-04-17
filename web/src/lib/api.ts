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
    const url = error.config?.url || ''
    const silentPaths = ['/users/me/views', '/onboarding/status', '/announcements/active']
    const isSilent = silentPaths.some((p) => url.includes(p))

    const isSessionFormatError =
      url.includes('/auth/sessions/') &&
      error.response?.status === 400 &&
      errorData?.code === 'INVALID_FORMAT'

    if (error.response?.status !== 401 && !isSilent && !isSessionFormatError) {
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
  session_id?: string
  partial?: boolean
  reason?: string
}

export interface AuthRefreshResponse {
  token: string
}

export interface Auth2FAResponse {
  token: string
}

export interface AuthChangePasswordResponse {
  message: string
}

export interface BackupCodesResponse {
  codes: string[]
}

export interface BackupCodesRemainingResponse {
  remaining: number
  totp_enabled: boolean
}

export const authApi = {
  login: (email: string, password: string, rememberMe?: boolean) =>
    api.post<{ status: string; data: AuthLoginResponse }>('/auth/login', {
      email,
      password,
      remember_me: rememberMe,
    }),

  verify2FA: (code?: string, backupCode?: string) => {
    const partialToken = useAuthStore.getState().partialToken
    const body = backupCode !== undefined ? { backup_code: backupCode } : { code }
    return api.post<{ status: string; data: Auth2FAResponse; meta?: { backup_codes_remaining?: number } }>(
      '/auth/2fa/verify',
      body,
      { headers: { Authorization: `Bearer ${partialToken}` } },
    )
  },

  changePassword: (currentPassword: string, newPassword: string) => {
    const partialToken = useAuthStore.getState().partialToken
    const headers: Record<string, string> = {}
    if (partialToken) {
      headers.Authorization = `Bearer ${partialToken}`
    }
    return api.post<{ status: string; data: AuthChangePasswordResponse }>(
      '/auth/password/change',
      { current_password: currentPassword, new_password: newPassword },
      { headers },
    )
  },

  generateBackupCodes: () =>
    api.post<{ status: string; data: BackupCodesResponse }>('/auth/2fa/backup-codes'),

  backupCodesRemaining: () =>
    api.get<{ status: string; data: BackupCodesRemainingResponse }>('/auth/2fa/backup-codes/remaining'),

  refresh: () =>
    api.post<{ status: string; data: AuthRefreshResponse }>('/auth/refresh'),

  logout: () => api.post('/auth/logout'),

  listSessions: (cursor?: string, limit = 50) => {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return api.get<{ status: string; data: Array<{ id: string; ip_address: string | null; user_agent: string | null; created_at: string; expires_at: string }>; meta: { cursor: string; has_more: boolean; limit: number } }>(`/auth/sessions?${params.toString()}`)
  },

  revokeSession: (id: string) => {
    const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
    if (!id || !UUID_RE.test(id)) {
      return Promise.reject(new Error('revokeSession: invalid session id'))
    }
    return api.delete<{ status: string; data: { revoked: boolean } }>(`/auth/sessions/${id}`)
  },
}

export const userApi = {
  unlock: (id: string) =>
    api.post<{ status: string; data: Record<string, unknown> }>(`/users/${id}/unlock`),

  revokeSessions: (id: string, includeApiKeys?: boolean) => {
    const params = includeApiKeys ? '?include_api_keys=true' : ''
    return api.post<{ status: string; data: { sessions_revoked: number; apikeys_revoked: number } }>(`/users/${id}/revoke-sessions${params}`)
  },

  resetPassword: (id: string) =>
    api.post<{ status: string; data: { temp_password: string } }>(`/users/${id}/reset-password`),
}
