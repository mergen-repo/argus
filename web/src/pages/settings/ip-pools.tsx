import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Globe,
  AlertCircle,
  RefreshCw,
  ChevronRight,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useIpPoolList } from '@/hooks/use-settings'
import { cn } from '@/lib/utils'
import type { IpPool } from '@/types/settings'

function UtilizationBar({ used, total }: { used: number; total: number }) {
  const pct = total > 0 ? (used / total) * 100 : 0
  const color = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'
  return (
    <div className="w-full">
      <div className="flex items-center justify-between mb-1">
        <span className="font-mono text-[11px] text-text-secondary">
          {used.toLocaleString()} / {total.toLocaleString()}
        </span>
        <span className="font-mono text-[11px] text-text-tertiary">{pct.toFixed(1)}%</span>
      </div>
      <div className="w-full h-2 bg-bg-hover rounded-full overflow-hidden">
        <div className={cn('h-full rounded-full transition-all duration-500', color)} style={{ width: `${Math.min(pct, 100)}%` }} />
      </div>
    </div>
  )
}

function PoolCard({ pool, onClick }: { pool: IpPool; onClick: () => void }) {
  const pct = pool.total_addresses > 0 ? (pool.used_addresses / pool.total_addresses) * 100 : 0
  const statusColor = pct > 90 ? 'var(--color-danger)' : pct > 70 ? 'var(--color-warning)' : 'var(--color-accent)'

  return (
    <Card className="card-hover cursor-pointer relative overflow-hidden" onClick={onClick}>
      <div className="absolute bottom-0 left-0 right-0 h-[2px]" style={{ backgroundColor: statusColor }} />
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">{pool.name}</CardTitle>
          <ChevronRight className="h-4 w-4 text-text-tertiary" />
        </div>
      </CardHeader>
      <CardContent className="pt-0 space-y-3">
        <div className="font-mono text-xs text-text-secondary">{pool.cidr}</div>
        <div className="grid grid-cols-3 gap-2 text-center">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Total</span>
            <span className="font-mono text-sm text-text-primary">{pool.total_addresses.toLocaleString()}</span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Used</span>
            <span className="font-mono text-sm text-text-primary">{pool.used_addresses.toLocaleString()}</span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Free</span>
            <span className="font-mono text-sm text-success">{pool.available_addresses.toLocaleString()}</span>
          </div>
        </div>
        <UtilizationBar used={pool.used_addresses} total={pool.total_addresses} />
      </CardContent>
    </Card>
  )
}

export default function IpPoolsPage() {
  const navigate = useNavigate()
  const { data: pools, isLoading, isError, refetch } = useIpPoolList()

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load IP pools</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch IP pool data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">IP Pools</h1>
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i}>
              <CardHeader className="pb-2"><Skeleton className="h-4 w-32" /></CardHeader>
              <CardContent className="pt-0 space-y-3">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-3 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (!pools || pools.length === 0) ? (
        <div className="flex flex-col items-center justify-center py-24">
          <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)] text-center">
            <Globe className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No IP pools</h3>
            <p className="text-xs text-text-secondary">IP pools will appear here when configured.</p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {pools.map((pool) => (
            <PoolCard key={pool.id} pool={pool} onClick={() => navigate(`/settings/ip-pools/${pool.id}`)} />
          ))}
        </div>
      )}
    </div>
  )
}
