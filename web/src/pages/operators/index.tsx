import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  RefreshCw,
  AlertCircle,
  Radio,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useOperatorList, useRealtimeOperatorHealth } from '@/hooks/use-operators'
import type { Operator } from '@/types/operator'
import { cn } from '@/lib/utils'

const RAT_DISPLAY: Record<string, string> = {
  nb_iot: 'NB-IoT',
  lte_m: 'LTE-M',
  lte: 'LTE',
  nr_5g: '5G NR',
}

const ADAPTER_DISPLAY: Record<string, string> = {
  mock: 'Mock',
  radius: 'RADIUS',
  diameter: 'Diameter',
  sba: '5G SBA',
}

function healthColor(status: string) {
  switch (status) {
    case 'healthy': return 'var(--color-success)'
    case 'degraded': return 'var(--color-warning)'
    case 'down': return 'var(--color-danger)'
    default: return 'var(--color-text-tertiary)'
  }
}

function healthGlow(status: string) {
  switch (status) {
    case 'healthy': return '0 0 8px rgba(0,255,136,0.4)'
    case 'degraded': return '0 0 8px rgba(255,184,0,0.4)'
    case 'down': return '0 0 8px rgba(255,68,102,0.4)'
    default: return 'none'
  }
}

function healthVariant(status: string): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'down': return 'danger'
    default: return 'secondary'
  }
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

function OperatorCard({ operator, onClick }: { operator: Operator; onClick: () => void }) {
  const mockSimCount = useMemo(() => Math.floor(Math.random() * 100000) + 1000, [])
  const mockLastCheck = useMemo(() => {
    const d = new Date()
    d.setMinutes(d.getMinutes() - Math.floor(Math.random() * 30))
    return d.toISOString()
  }, [])

  return (
    <Card
      className="card-hover cursor-pointer p-4 space-y-3 relative overflow-hidden"
      onClick={onClick}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <span
            className="h-3 w-3 rounded-full flex-shrink-0 pulse-dot"
            style={{
              backgroundColor: healthColor(operator.health_status),
              boxShadow: healthGlow(operator.health_status),
            }}
          />
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-text-primary truncate">{operator.name}</h3>
            <p className="font-mono text-[11px] text-text-tertiary">{operator.code}</p>
          </div>
        </div>
        <Badge variant={healthVariant(operator.health_status)} className="text-[10px] flex-shrink-0">
          {operator.health_status.toUpperCase()}
        </Badge>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">SIM Count</span>
          <div className="font-mono text-sm font-semibold text-text-primary">{mockSimCount.toLocaleString()}</div>
        </div>
        <div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">Protocol</span>
          <div className="text-sm font-medium text-text-primary">
            {ADAPTER_DISPLAY[operator.adapter_type] ?? operator.adapter_type}
          </div>
        </div>
      </div>

      <div>
        <span className="text-[10px] uppercase tracking-wider text-text-tertiary">MCC/MNC</span>
        <div className="font-mono text-xs text-text-secondary">{operator.mcc} / {operator.mnc}</div>
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 flex-wrap">
          {operator.supported_rat_types.slice(0, 3).map((rat) => (
            <span
              key={rat}
              className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium"
            >
              {RAT_DISPLAY[rat] ?? rat}
            </span>
          ))}
          {operator.supported_rat_types.length > 3 && (
            <span className="text-[10px] text-text-tertiary">+{operator.supported_rat_types.length - 3}</span>
          )}
        </div>
        <span className="text-[10px] text-text-tertiary">{timeAgo(mockLastCheck)}</span>
      </div>
    </Card>
  )
}

function OperatorCardSkeleton() {
  return (
    <Card className="p-4 space-y-3">
      <div className="flex justify-between">
        <div className="flex gap-3">
          <Skeleton className="h-3 w-3 rounded-full" />
          <div>
            <Skeleton className="h-4 w-28 mb-1" />
            <Skeleton className="h-3 w-16" />
          </div>
        </div>
        <Skeleton className="h-5 w-16" />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <Skeleton className="h-2.5 w-16 mb-1" />
          <Skeleton className="h-4 w-12" />
        </div>
        <div>
          <Skeleton className="h-2.5 w-14 mb-1" />
          <Skeleton className="h-4 w-16" />
        </div>
      </div>
      <Skeleton className="h-3 w-20" />
      <Skeleton className="h-5 w-full" />
    </Card>
  )
}

export default function OperatorListPage() {
  const navigate = useNavigate()
  const { data: operators, isLoading, isError, refetch } = useOperatorList()
  useRealtimeOperatorHealth()

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load operators</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch operator data. Please try again.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Operators</h1>
      </div>

      {isLoading && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <OperatorCardSkeleton key={i} />
          ))}
        </div>
      )}

      {!isLoading && (!operators || operators.length === 0) && (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
            <Radio className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No operators configured</h3>
            <p className="text-xs text-text-secondary">Contact a super admin to create operators.</p>
          </div>
        </div>
      )}

      {!isLoading && operators && operators.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {operators.map((op, i) => (
            <div key={op.id} style={{ animationDelay: `${i * 50}ms` }} className="animate-in fade-in slide-in-from-bottom-1">
              <OperatorCard
                operator={op}
                onClick={() => navigate(`/operators/${op.id}`)}
              />
            </div>
          ))}
        </div>
      )}

      {!isLoading && operators && operators.length > 0 && (
        <p className="text-center text-xs text-text-tertiary">
          Showing {operators.length} operator{operators.length !== 1 ? 's' : ''}
        </p>
      )}
    </div>
  )
}
