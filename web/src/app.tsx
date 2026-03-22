import { useEffect } from 'react'
import { RouterProvider } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from 'sonner'
import { router } from './router'
import { queryClient } from './lib/query'
import { useUIStore } from './stores/ui'
import { useAuthStore } from './stores/auth'
import { wsClient } from './lib/ws'

export function App() {
  const darkMode = useUIStore((s) => s.darkMode)
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', darkMode)
  }, [darkMode])

  useEffect(() => {
    if (isAuthenticated) {
      wsClient.connect()
    } else {
      wsClient.disconnect()
    }
    return () => wsClient.disconnect()
  }, [isAuthenticated])

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster
        position="top-right"
        theme={darkMode ? 'dark' : 'light'}
        toastOptions={{
          style: {
            background: 'var(--color-bg-elevated)',
            border: '1px solid var(--color-border)',
            color: 'var(--color-text-primary)',
          },
        }}
      />
    </QueryClientProvider>
  )
}
