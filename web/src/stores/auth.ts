import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface User {
  id: string
  email: string
  name: string
  role: string
  tenant_id: string
}

interface AuthState {
  user: User | null
  token: string | null
  refreshToken: string | null
  permissions: string[]
  isAuthenticated: boolean

  setUser: (user: User) => void
  setTokens: (token: string, refreshToken: string) => void
  setPermissions: (permissions: string[]) => void
  login: (user: User, token: string, refreshToken: string, permissions: string[]) => void
  logout: () => void
  hasPermission: (permission: string) => boolean
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      user: null,
      token: null,
      refreshToken: null,
      permissions: [],
      isAuthenticated: false,

      setUser: (user) => set({ user }),

      setTokens: (token, refreshToken) => set({ token, refreshToken }),

      setPermissions: (permissions) => set({ permissions }),

      login: (user, token, refreshToken, permissions) =>
        set({ user, token, refreshToken, permissions, isAuthenticated: true }),

      logout: () =>
        set({
          user: null,
          token: null,
          refreshToken: null,
          permissions: [],
          isAuthenticated: false,
        }),

      hasPermission: (permission) => get().permissions.includes(permission),
    }),
    {
      name: 'argus-auth',
      partialize: (state) => ({
        user: state.user,
        token: state.token,
        refreshToken: state.refreshToken,
        permissions: state.permissions,
        isAuthenticated: state.isAuthenticated,
      }),
    },
  ),
)
