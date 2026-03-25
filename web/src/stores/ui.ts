import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface RecentItem {
  type: string
  id: string
  label: string
  path: string
  timestamp: number
}

interface FavoriteItem {
  type: string
  id: string
  label: string
  path: string
}

interface UIState {
  sidebarCollapsed: boolean
  darkMode: boolean
  locale: string
  commandPaletteOpen: boolean
  tableDensity: 'compact' | 'comfortable' | 'spacious'
  recentItems: RecentItem[]
  favorites: FavoriteItem[]

  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  toggleDarkMode: () => void
  setDarkMode: (dark: boolean) => void
  setLocale: (locale: string) => void
  setCommandPaletteOpen: (open: boolean) => void
  setTableDensity: (d: 'compact' | 'comfortable' | 'spacious') => void
  addRecentItem: (item: Omit<RecentItem, 'timestamp'>) => void
  toggleFavorite: (item: FavoriteItem) => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      darkMode: true,
      locale: 'en',
      commandPaletteOpen: false,
      tableDensity: 'compact' as const,
      recentItems: [] as RecentItem[],
      favorites: [] as FavoriteItem[],

      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      toggleDarkMode: () =>
        set((s) => {
          const next = !s.darkMode
          document.documentElement.classList.toggle('dark', next)
          return { darkMode: next }
        }),
      setDarkMode: (dark) => {
        document.documentElement.classList.toggle('dark', dark)
        set({ darkMode: dark })
      },
      setLocale: (locale) => set({ locale }),
      setCommandPaletteOpen: (open) => set({ commandPaletteOpen: open }),
      setTableDensity: (d) => set({ tableDensity: d }),
      addRecentItem: (item) =>
        set((s) => {
          const filtered = s.recentItems.filter((r) => r.id !== item.id)
          return { recentItems: [{ ...item, timestamp: Date.now() }, ...filtered].slice(0, 10) }
        }),
      toggleFavorite: (item) =>
        set((s) => {
          const exists = s.favorites.some((f) => f.id === item.id)
          return {
            favorites: exists
              ? s.favorites.filter((f) => f.id !== item.id)
              : [...s.favorites, item].slice(0, 5),
          }
        }),
    }),
    {
      name: 'argus-ui',
      partialize: (state) => ({
        sidebarCollapsed: state.sidebarCollapsed,
        darkMode: state.darkMode,
        locale: state.locale,
        tableDensity: state.tableDensity,
        recentItems: state.recentItems,
        favorites: state.favorites,
      }),
    },
  ),
)
