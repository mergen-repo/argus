import axios from 'axios'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { decodeToken } from '@/lib/jwt'

const broadcastChannel = typeof BroadcastChannel !== 'undefined'
  ? new BroadcastChannel('argus-auth-broadcast')
  : null

let refreshTimer: ReturnType<typeof setTimeout> | null = null

function schedulePreemptiveRefresh(expiresAtMs: number) {
  if (refreshTimer) {
    clearTimeout(refreshTimer)
    refreshTimer = null
  }
  const delay = expiresAtMs - Date.now() - 5 * 60 * 1000
  if (delay <= 0) return
  refreshTimer = setTimeout(() => {
    useAuthStore.getState().refreshAccessToken().catch(() => {
      // failure path is handled by interceptor — logout will happen there
    })
  }, delay)
}

export interface User {
  id: string
  email: string
  name: string
  role: string
  onboarding_completed?: boolean
}

interface AuthState {
  user: User | null
  token: string | null
  permissions: string[]
  isAuthenticated: boolean
  partialToken: string | null
  partial2faReason?: string
  requires2FA: boolean
  sessionId: string | null
  tokenExpiresAt: number | null

  setAuth: (user: User, token: string, permissions?: string[], sessionId?: string) => void
  setToken: (token: string) => void
  setTokenExpiresAt: (ms: number | null) => void
  refreshAccessToken: () => Promise<void>
  setPartial2FA: (token: string, user: User) => void
  clear2FA: () => void
  setPartialSession: (token: string, reason: string) => void
  clearPartial: () => void
  logout: () => void
  hasPermission: (permission: string) => boolean
  setOnboardingCompleted: (completed: boolean) => void

  // Derived from the current access token. Null when no token or the
  // claim is absent. `activeTenantId` is null = System View; a non-null
  // value = super_admin is viewing-as that tenant. `homeTenantId` is
  // always the user's own tenant from the JWT `tenant_id` claim.
  homeTenantId: () => string | null
  activeTenantId: () => string | null
}

export const useAuthStore = create<AuthState>()(persist((set, get) => ({
  user: null,
  token: null,
  permissions: [],
  isAuthenticated: false,
  partialToken: null,
  partial2faReason: undefined,
  requires2FA: false,
  sessionId: null,
  tokenExpiresAt: null,

  setAuth: (user, token, permissions = [], sessionId) => {
    const decoded = decodeToken(token)
    const expiresAtMs = decoded?.exp ? decoded.exp * 1000 : null
    set({
      user,
      token,
      permissions,
      isAuthenticated: true,
      partialToken: null,
      requires2FA: false,
      sessionId: sessionId ?? null,
      tokenExpiresAt: expiresAtMs,
    })
    if (expiresAtMs) {
      schedulePreemptiveRefresh(expiresAtMs)
    }
  },

  setToken: (token) => {
    const decoded = decodeToken(token)
    const expiresAtMs = decoded?.exp ? decoded.exp * 1000 : null
    set({ token, tokenExpiresAt: expiresAtMs })
    if (expiresAtMs) {
      schedulePreemptiveRefresh(expiresAtMs)
    }
  },

  setTokenExpiresAt: (ms) => {
    set({ tokenExpiresAt: ms })
    if (ms != null) {
      schedulePreemptiveRefresh(ms)
    } else if (refreshTimer) {
      // Null expiry clears the scheduler so a dangling timer cannot fire
      // with a stale token after the store was externally invalidated.
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
  },

  refreshAccessToken: async () => {
    const res = await axios.post('/api/v1/auth/refresh', {}, { withCredentials: true })
    const data = res.data?.data
    if (!data?.token) throw new Error('refresh: missing token in response')
    get().setToken(data.token)
    if (typeof data.expires_in === 'number') {
      get().setTokenExpiresAt(Date.now() + data.expires_in * 1000)
    }
    if (broadcastChannel) {
      broadcastChannel.postMessage({
        type: 'token_refreshed',
        token: data.token,
        expiresAt: Date.now() + (data.expires_in ?? 3600) * 1000,
      })
    }
  },

  setPartial2FA: (token, user) =>
    set({
      partialToken: token,
      user,
      requires2FA: true,
      isAuthenticated: false,
    }),

  clear2FA: () => set({ partialToken: null, requires2FA: false }),

  setPartialSession: (token, reason) =>
    set({
      partialToken: token,
      partial2faReason: reason,
      isAuthenticated: false,
    }),

  clearPartial: () => set({ partialToken: null, partial2faReason: undefined }),

  logout: () => {
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    set({
      user: null,
      token: null,
      permissions: [],
      isAuthenticated: false,
      partialToken: null,
      partial2faReason: undefined,
      requires2FA: false,
      sessionId: null,
      tokenExpiresAt: null,
    })
  },

  hasPermission: (permission) => get().permissions.includes(permission),

  setOnboardingCompleted: (completed) =>
    set((s) => ({
      user: s.user ? { ...s.user, onboarding_completed: completed } : null,
    })),

  homeTenantId: () => {
    const payload = decodeToken(get().token)
    return payload?.tenant_id ?? null
  },
  activeTenantId: () => {
    const payload = decodeToken(get().token)
    return payload?.active_tenant ?? null
  },
}), {
  name: 'argus-auth',
  partialize: (state) => ({
    user: state.user,
    token: state.token,
    permissions: state.permissions,
    isAuthenticated: state.isAuthenticated,
    sessionId: state.sessionId,
    tokenExpiresAt: state.tokenExpiresAt,
  }),
  onRehydrateStorage: () => (state) => {
    if (state?.tokenExpiresAt && state.tokenExpiresAt > Date.now()) {
      schedulePreemptiveRefresh(state.tokenExpiresAt)
    }
  },
}))

broadcastChannel?.addEventListener('message', (event) => {
  const msg = event.data as { type?: string; token?: string; expiresAt?: number }
  if (msg?.type === 'token_refreshed' && typeof msg.token === 'string') {
    // setToken() already derives tokenExpiresAt from the JWT `exp` claim and
    // arms the scheduler. Only override with msg.expiresAt when the JWT carried
    // no exp, to avoid double-scheduling.
    useAuthStore.getState().setToken(msg.token)
    const post = useAuthStore.getState()
    if (post.tokenExpiresAt == null && typeof msg.expiresAt === 'number') {
      post.setTokenExpiresAt(msg.expiresAt)
    }
  }
})
