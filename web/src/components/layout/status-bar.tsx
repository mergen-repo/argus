import { useEffect, useState } from 'react'
import { WifiOff, Cpu } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { useUIStore } from '@/stores/ui'
import { wsClient } from '@/lib/ws'

function StatusBar() {
  const [wsConnected, setWsConnected] = useState(true)
  const [lastSync, setLastSync] = useState<Date>(new Date())
  const user = useAuthStore((s) => s.user)
  const sidebarCollapsed = useUIStore((s) => s.sidebarCollapsed)

  useEffect(() => {
    const interval = setInterval(() => {
      setWsConnected((wsClient as unknown as { isConnected?: () => boolean }).isConnected?.() ?? true)
      setLastSync(new Date())
    }, 5000)
    return () => clearInterval(interval)
  }, [])

  const syncAgo = Math.round((Date.now() - lastSync.getTime()) / 1000)

  return (
    <div
      className={cn(
        'fixed bottom-0 right-0 z-30 h-7 border-t border-border bg-bg-surface/90 backdrop-blur-sm flex items-center px-4 text-[10px] text-text-tertiary gap-4 transition-all duration-200',
        sidebarCollapsed ? 'left-16' : 'left-60',
      )}
    >
      <span className="flex items-center gap-1.5">
        {wsConnected ? (
          <>
            <span className="h-1.5 w-1.5 rounded-full bg-success pulse-dot" />
            <span>Connected</span>
          </>
        ) : (
          <>
            <WifiOff className="h-3 w-3 text-danger" />
            <span className="text-danger">Disconnected</span>
          </>
        )}
      </span>
      <span className="border-l border-border h-3" />
      <span>Sync: {syncAgo < 5 ? 'just now' : `${syncAgo}s ago`}</span>
      {user && (
        <>
          <span className="border-l border-border h-3" />
          <span className="flex items-center gap-1">
            <Cpu className="h-3 w-3" />
            {user.email}
          </span>
          {user.role && (
            <>
              <span className="border-l border-border h-3" />
              <span className="text-accent">{user.role.replace('_', ' ')}</span>
            </>
          )}
        </>
      )}
      <span className="ml-auto font-mono text-accent/70">Argus v2.1.0</span>
    </div>
  )
}

export { StatusBar }
