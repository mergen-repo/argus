import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { DollarSign, TrendingUp, TrendingDown } from 'lucide-react'
import { formatCurrency, formatBytes } from '@/lib/format'
import type { ApiResponse } from '@/types/sim'
import type { CostResponse } from '@/types/analytics'

interface CostAttributionTabProps {
  simId: string
}

function useSIMCost(simId: string) {
  return useQuery({
    queryKey: ['analytics', 'cost', 'sim', simId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<CostResponse>>(`/analytics/cost?period=30d`)
      const data = res.data.data
      if (!data) return null
      const simEntry = data.top_expensive_sims?.find((s) => s.sim_id === simId)
      if (!simEntry) return null
      return {
        ...data,
        by_operator: data.by_operator?.filter(
          (op) => data.top_expensive_sims?.some(
            (s) => s.sim_id === simId && s.operator_id === op.operator_id
          )
        ) ?? [],
        total_cost: simEntry.total_usage_cost,
        sim_bytes: simEntry.total_bytes,
      }
    },
    staleTime: 60_000,
    enabled: !!simId,
  })
}

export function CostAttributionTab({ simId }: CostAttributionTabProps) {
  const { data, isLoading, isError } = useSIMCost(simId)

  if (isLoading) {
    return (
      <div className="mt-4 grid grid-cols-2 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="flex flex-col items-center justify-center py-10 mt-4 text-center">
        <DollarSign className="h-10 w-10 text-text-tertiary mx-auto mb-3 opacity-40" />
        <p className="text-[13px] text-text-secondary mb-1">Cost data unavailable</p>
        <p className="text-[11px] text-text-tertiary">Cost attribution requires active sessions with CDR data</p>
      </div>
    )
  }

  return (
    <div className="mt-4 space-y-4">
      <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">
        Cost Attribution — Last 30 Days
      </p>
      <div className="grid grid-cols-2 gap-4">
        <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-2">
              <DollarSign className="h-4 w-4 text-accent" />
              <span className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">Total Cost</span>
            </div>
            <p className="text-[28px] font-bold font-mono text-text-primary">
              {formatCurrency(data.total_cost ?? 0)}
            </p>
            <p className="text-[11px] text-text-tertiary mt-1">Across all operators</p>
          </CardContent>
        </Card>

        <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
          <CardContent className="p-4">
            <div className="flex items-center gap-2 mb-2">
              <TrendingUp className="h-4 w-4 text-success" />
              <span className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">Data Usage</span>
            </div>
            <p className="text-[28px] font-bold font-mono text-text-primary">
              {formatBytes(data.cost_per_mb?.reduce((sum, e) => sum + e.total_mb * 1_000_000, 0) ?? 0)}
            </p>
            <p className="text-[11px] text-text-tertiary mt-1">Total data transferred</p>
          </CardContent>
        </Card>
      </div>

      {data.by_operator && data.by_operator.length > 0 && (
        <div>
          <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
            Cost Breakdown by Operator
          </p>
          <div className="space-y-2">
            {data.by_operator.map((op) => (
              <div key={op.operator_id} className="flex items-center justify-between p-3 bg-bg-surface rounded-[10px] border border-border">
                <span className="text-[12px] font-mono text-text-secondary">{op.operator_id.slice(0, 8)}...</span>
                <span className="text-[13px] font-medium text-text-primary">{formatCurrency(op.total_usage_cost)}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
