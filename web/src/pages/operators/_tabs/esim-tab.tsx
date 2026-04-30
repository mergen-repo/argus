import React, { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Cpu, Database, Shuffle, AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { EsimProfilesTab } from '@/components/operators/EsimProfilesTab'
import { useEsimStockSummary } from '@/hooks/use-esim'
import { useESimList } from '@/hooks/use-esim'

interface StatCardProps {
  label: string
  value: string
  icon: React.ReactNode
  tone?: 'accent' | 'success' | 'warning' | 'danger'
}

function StatCard({ label, value, icon, tone }: StatCardProps) {
  const toneClass =
    tone === 'success'
      ? 'text-success'
      : tone === 'warning'
        ? 'text-warning'
        : tone === 'danger'
          ? 'text-danger'
          : tone === 'accent'
            ? 'text-accent'
            : 'text-text-primary'
  return (
    <div className="flex items-center gap-3 px-4 py-3 rounded-[var(--radius-md)] bg-bg-surface border border-border">
      <span className={`${toneClass} opacity-70`}>{icon}</span>
      <div className="min-w-0">
        <p className="text-xs uppercase tracking-widest text-text-secondary font-medium">{label}</p>
        <p className="font-mono text-sm font-bold text-text-primary leading-none mt-0.5 truncate">{value}</p>
      </div>
    </div>
  )
}

interface ESimTabProps {
  operatorId: string
}

export default function ESimTab({ operatorId }: ESimTabProps) {
  const navigate = useNavigate()

  const { data: stockEntries, isLoading: stockLoading, isError: stockError } = useEsimStockSummary(operatorId)
  const { data: listData, isLoading: listLoading } = useESimList({ operator_id: operatorId })

  const stock = useMemo(() => {
    if (!stockEntries || stockEntries.length === 0) return null
    return stockEntries.reduce(
      (acc, e) => ({
        total: acc.total + e.total,
        allocated: acc.allocated + e.allocated,
        available: acc.available + e.available,
      }),
      { total: 0, allocated: 0, available: 0 },
    )
  }, [stockEntries])

  const stateCounts = useMemo(() => {
    const profiles = listData?.pages.flatMap((p) => p.data) ?? []
    return {
      enabled: profiles.filter((p) => p.profile_state === 'enabled').length,
      disabled: profiles.filter((p) => p.profile_state === 'disabled').length,
      available: profiles.filter((p) => p.profile_state === 'available').length,
      deleted: profiles.filter((p) => p.profile_state === 'deleted').length,
    }
  }, [listData])

  const utilPct = stock && stock.total > 0
    ? Math.round((stock.allocated / stock.total) * 100)
    : 0

  const fmt = (n: number) => n.toLocaleString()

  return (
    <div className="mt-4 space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div className="rounded-[var(--radius-md)] bg-bg-surface border border-border p-4 space-y-2">
          <p className="text-xs uppercase tracking-widest text-text-secondary font-medium flex items-center gap-1.5">
            <Database className="h-3 w-3" />
            Stock
          </p>
          {stockLoading ? (
            <div className="space-y-1.5">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </div>
          ) : stockError || !stock ? (
            <div className="flex items-center gap-2 text-danger text-xs">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              Failed to load stock data
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-x-4 gap-y-1">
              <span className="text-xs text-text-secondary">Total</span>
              <span className="font-mono text-xs font-semibold text-text-primary text-right">{fmt(stock.total)}</span>
              <span className="text-xs text-text-secondary">Allocated</span>
              <span className="font-mono text-xs font-semibold text-warning text-right">{fmt(stock.allocated)}</span>
              <span className="text-xs text-text-secondary">Available</span>
              <span className="font-mono text-xs font-semibold text-success text-right">{fmt(stock.available)}</span>
              <span className="text-xs text-text-secondary">Utilization</span>
              <span
                className={`font-mono text-xs font-semibold text-right ${utilPct >= 90 ? 'text-danger' : utilPct >= 70 ? 'text-warning' : 'text-accent'}`}
              >
                {utilPct}%
              </span>
            </div>
          )}
        </div>

        <div className="rounded-[var(--radius-md)] bg-bg-surface border border-border p-4 space-y-2">
          <p className="text-xs uppercase tracking-widest text-text-secondary font-medium flex items-center gap-1.5">
            <Cpu className="h-3 w-3" />
            State Breakdown
          </p>
          {listLoading ? (
            <div className="space-y-1.5">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-x-4 gap-y-1">
              <span className="text-xs text-text-secondary">Enabled</span>
              <span className="font-mono text-xs font-semibold text-success text-right">{fmt(stateCounts.enabled)}</span>
              <span className="text-xs text-text-secondary">Disabled</span>
              <span className="font-mono text-xs font-semibold text-warning text-right">{fmt(stateCounts.disabled)}</span>
              <span className="text-xs text-text-secondary">Available</span>
              <span className="font-mono text-xs font-semibold text-text-primary text-right">{fmt(stateCounts.available)}</span>
              <span className="text-xs text-text-secondary">Deleted</span>
              <span className="font-mono text-xs font-semibold text-danger text-right">{fmt(stateCounts.deleted)}</span>
            </div>
          )}
        </div>
      </div>

      <div className="flex items-center gap-2">
        <StatCard
          label="Profiles"
          value={listLoading ? '—' : `${fmt(stateCounts.enabled + stateCounts.disabled + stateCounts.available + stateCounts.deleted)}`}
          icon={<Cpu className="h-4 w-4" />}
          tone="accent"
        />
        <div className="flex-1" />
        <Button
          variant="outline"
          size="sm"
          className="gap-2 shrink-0"
          onClick={() => navigate(`/esim?operator_id=${operatorId}`)}
        >
          <Shuffle className="h-3.5 w-3.5" />
          Bulk Switch
        </Button>
      </div>

      <EsimProfilesTab operatorId={operatorId} />
    </div>
  )
}
