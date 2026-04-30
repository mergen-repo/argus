import * as React from 'react'
import { RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import type { AutoRefreshOption } from '@/hooks/use-data-freshness'

interface DataFreshnessProps {
  indicator: 'live' | 'stale' | 'offline'
  label: string
  onRefresh?: () => void
  autoRefresh: AutoRefreshOption
  setAutoRefresh: (v: AutoRefreshOption) => void
  className?: string
}

const REFRESH_OPTIONS = [
  { value: '15', label: '15s' },
  { value: '30', label: '30s' },
  { value: '60', label: '1m' },
  { value: '0', label: 'Off' },
]

export const DataFreshness = React.memo(function DataFreshness({
  indicator,
  label,
  onRefresh,
  autoRefresh,
  setAutoRefresh,
  className,
}: DataFreshnessProps) {
  return (
    <div className={cn('flex items-center gap-3 text-xs text-text-secondary', className)}>
      <Tooltip content={`Last updated: ${label}`}>
        <Badge
          className={cn(
            'rounded-[var(--radius-sm)] px-1.5 py-0.5 text-[10px] font-mono cursor-default',
            indicator === 'live' && 'bg-success-dim text-success',
            indicator === 'stale' && 'bg-warning-dim text-warning',
            indicator === 'offline' && 'bg-danger-dim text-danger',
          )}
        >
          {indicator === 'live' ? '● Live' : indicator === 'stale' ? '○ Stale' : '✕ Offline'}
        </Badge>
      </Tooltip>

      <span className="text-text-tertiary">{label}</span>

      {onRefresh && (
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 text-text-tertiary hover:text-text-primary"
          onClick={onRefresh}
          aria-label="Refresh"
        >
          <RefreshCw className="h-3 w-3" />
        </Button>
      )}

      <Select
        value={String(autoRefresh)}
        onChange={(e) => setAutoRefresh(Number(e.target.value) as AutoRefreshOption)}
        options={REFRESH_OPTIONS}
        className="h-6 w-16 text-[10px] px-1.5 border-border-subtle"
      />
    </div>
  )
})
