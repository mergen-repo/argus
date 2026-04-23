import * as React from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Tooltip } from '@/components/ui/tooltip'
import { formatBytes } from '@/lib/format'

interface TwoWayTrafficProps {
  in: number
  out: number
  className?: string
}

export function TwoWayTraffic({ in: bytesIn, out: bytesOut, className }: TwoWayTrafficProps) {
  if (bytesIn === 0 && bytesOut === 0) {
    return <span className="text-text-tertiary font-mono text-xs">—</span>
  }

  const total = bytesIn + bytesOut
  const tooltipContent = `In: ${formatBytes(bytesIn)} · Out: ${formatBytes(bytesOut)} · Total: ${formatBytes(total)}`

  return (
    <Tooltip content={tooltipContent}>
      <span className={cn('inline-flex items-center gap-2 font-mono text-xs', className)}>
        <span className="inline-flex items-center gap-0.5">
          <ArrowDown className="h-3 w-3 text-success" />
          {formatBytes(bytesIn)}
        </span>
        <span className="inline-flex items-center gap-0.5">
          <ArrowUp className="h-3 w-3 text-info" />
          {formatBytes(bytesOut)}
        </span>
      </span>
    </Tooltip>
  )
}
