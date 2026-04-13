import { useMemo } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { ArrowLeft, Radio } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Spinner } from '@/components/ui/spinner'
import { CompareView } from '@/components/shared/compare-view'
import { useOperator } from '@/hooks/use-operators'
import { formatBytes } from '@/lib/format'

function useOperators(ids: string[]) {
  const q0 = useOperator(ids[0] ?? '')
  const q1 = useOperator(ids[1] ?? '')
  const q2 = useOperator(ids[2] ?? '')
  const queries = [q0, q1, q2].slice(0, ids.length)
  const loading = queries.some((q) => q.isLoading)
  const data = queries.map((q) => q.data).filter(Boolean) as NonNullable<typeof q0.data>[]
  return { data, loading }
}

const COMPARE_FIELDS = [
  { key: 'name', label: 'Name' },
  { key: 'code', label: 'Code', render: (v: unknown) => <span className="font-mono text-xs">{String(v)}</span> },
  { key: 'mcc', label: 'MCC', render: (v: unknown) => <span className="font-mono text-xs">{String(v)}</span> },
  { key: 'mnc', label: 'MNC', render: (v: unknown) => <span className="font-mono text-xs">{String(v)}</span> },
  { key: 'adapter_type', label: 'Adapter' },
  { key: 'health_status', label: 'Health', render: (v: unknown) => <Badge variant={v === 'healthy' ? 'success' : v === 'degraded' ? 'warning' : 'danger'} className="text-[10px]">{String(v)}</Badge> },
  { key: 'state', label: 'State', render: (v: unknown) => <Badge variant={v === 'active' ? 'success' : 'secondary'} className="text-[10px]">{String(v)}</Badge> },
  { key: 'failover_policy', label: 'Failover Policy' },
  { key: 'failover_timeout_ms', label: 'Failover Timeout (ms)', render: (v: unknown) => <span className="font-mono text-xs">{String(v)}</span> },
  { key: 'circuit_breaker_threshold', label: 'CB Threshold', render: (v: unknown) => <span className="font-mono text-xs">{String(v)}</span> },
  { key: 'sim_count', label: 'SIMs', render: (v: unknown) => <span className="font-mono text-xs">{Number(v).toLocaleString()}</span> },
  { key: 'active_sessions', label: 'Active Sessions', render: (v: unknown) => <span className="font-mono text-xs">{Number(v).toLocaleString()}</span> },
  { key: 'total_traffic_bytes', label: 'Total Traffic', render: (v: unknown) => <span className="font-mono text-xs">{formatBytes(Number(v))}</span> },
  { key: 'sla_uptime_target', label: 'SLA Uptime Target', render: (v: unknown) => <span className="font-mono text-xs">{v != null ? `${v}%` : '—'}</span> },
]

export default function OperatorComparePage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()

  const ids = useMemo(() => {
    const raw = searchParams.get('ids') ?? ''
    return raw.split(',').filter(Boolean).slice(0, 3)
  }, [searchParams])

  const { data: operators, loading } = useOperators(ids)

  if (ids.length < 2) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <Radio className="h-10 w-10 text-text-tertiary" />
        <p className="text-sm text-text-secondary">Select 2–3 operators to compare. Use <code className="font-mono text-xs">?ids=id1,id2</code></p>
        <Button variant="outline" onClick={() => navigate('/operators')}>Back to Operators</Button>
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 mb-2">
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <Radio className="h-5 w-5 text-accent" />
        <h1 className="text-[16px] font-semibold text-text-primary">Compare Operators</h1>
      </div>

      {operators.length < 2 ? (
        <p className="text-sm text-text-secondary">Could not load the selected operators.</p>
      ) : (
        <CompareView entities={operators} fields={COMPARE_FIELDS} />
      )}
    </div>
  )
}
