import { useEffect, useState } from 'react'
import { Wifi, WifiOff, Loader2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Tooltip } from '@/components/ui/tooltip'
import { wsClient, type WSStatus } from '@/lib/ws'
import { cn } from '@/lib/utils'

export function WSIndicator() {
  const [status, setStatus] = useState<WSStatus>(wsClient.getStatus())

  useEffect(() => {
    const unsub = wsClient.onStatus(setStatus)
    return unsub
  }, [])

  if (status === 'connected') {
    return (
      <Tooltip content="Real-time updates active" side="bottom">
        <Badge variant="success" className="gap-1.5 cursor-default">
          <span className="h-1.5 w-1.5 rounded-full bg-success pulse-dot" />
          <Wifi className="h-3 w-3" />
          <span className="text-[10px]">Live</span>
        </Badge>
      </Tooltip>
    )
  }

  if (status === 'connecting') {
    return (
      <Tooltip content="Live updates paused — attempting reconnect" side="bottom">
        <Badge variant="warning" className="gap-1.5 cursor-default">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span className="text-[10px]">Reconnecting…</span>
        </Badge>
      </Tooltip>
    )
  }

  return (
    <Tooltip content="Live updates paused — click to reconnect" side="bottom">
      <Badge
        variant="danger"
        className={cn('gap-1.5 cursor-pointer')}
        onClick={() => wsClient.reconnectNow()}
      >
        <WifiOff className="h-3 w-3" />
        <span className="text-[10px]">Offline</span>
      </Badge>
    </Tooltip>
  )
}
