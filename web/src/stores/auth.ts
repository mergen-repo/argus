import { create } from 'zustand'
import { persist } from 'zustand/middleware'

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

  setAuth: (user: User, token: string, permissions?: string[], sessionId?: string) => void
  setToken: (token: string) => void
  setPartial2FA: (token: string, user: User) => void
  clear2FA: () => void
  setPartialSession: (token: string, reason: string) => void
  clearPartial: () => void
  logout: () => void
  hasPermission: (permission: string) => boolean
  setOnboardingCompleted: (completed: boolean) => void
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

  setAuth: (user, token, permissions = [], sessionId) =>
    set({
      user,
      token,
      permissions,
      isAuthenticated: true,
      partialToken: null,
      requires2FA: false,
      sessionId: sessionId ?? null,
    }),

  setToken: (token) => set({ token }),

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

  logout: () =>
    set({
      user: null,
      token: null,
      permissions: [],
      isAuthenticated: false,
      partialToken: null,
      partial2faReason: undefined,
      requires2FA: false,
      sessionId: null,
    }),

  hasPermission: (permission) => get().permissions.includes(permission),

  setOnboardingCompleted: (completed) =>
    set((s) => ({
      user: s.user ? { ...s.user, onboarding_completed: completed } : null,
    })),
}), {
  name: 'argus-auth',
  partialize: (state) => ({
    user: state.user,
    token: state.token,
    permissions: state.permissions,
    isAuthenticated: state.isAuthenticated,
    sessionId: state.sessionId,
  }),
}))
