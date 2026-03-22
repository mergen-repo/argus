import { create } from 'zustand'

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
  requires2FA: boolean

  setAuth: (user: User, token: string, permissions?: string[]) => void
  setToken: (token: string) => void
  setPartial2FA: (token: string, user: User) => void
  clear2FA: () => void
  logout: () => void
  hasPermission: (permission: string) => boolean
  setOnboardingCompleted: (completed: boolean) => void
}

export const useAuthStore = create<AuthState>()((set, get) => ({
  user: null,
  token: null,
  permissions: [],
  isAuthenticated: false,
  partialToken: null,
  requires2FA: false,

  setAuth: (user, token, permissions = []) =>
    set({
      user,
      token,
      permissions,
      isAuthenticated: true,
      partialToken: null,
      requires2FA: false,
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

  logout: () =>
    set({
      user: null,
      token: null,
      permissions: [],
      isAuthenticated: false,
      partialToken: null,
      requires2FA: false,
    }),

  hasPermission: (permission) => get().permissions.includes(permission),

  setOnboardingCompleted: (completed) =>
    set((s) => ({
      user: s.user ? { ...s.user, onboarding_completed: completed } : null,
    })),
}))
