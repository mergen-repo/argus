import { useMemo } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { ArrowLeft, Shield } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Spinner } from '@/components/ui/spinner'
import { CompareView } from '@/components/shared/compare-view'
import { usePolicy } from '@/hooks/use-policies'

function usePolicies(ids: string[]) {
  const q0 = usePolicy(ids[0] ?? '')
  const q1 = usePolicy(ids[1] ?? '')
  const q2 = usePolicy(ids[2] ?? '')
  const queries = [q0, q1, q2].slice(0, ids.length)
  const loading = queries.some((q) => q.isLoading)
  const data = queries.map((q) => q.data).filter(Boolean) as NonNullable<typeof q0.data>[]
  return { data, loading }
}

const COMPARE_FIELDS = [
  { key: 'name', label: 'Name' },
  { key: 'scope', label: 'Scope' },
  { key: 'state', label: 'State', render: (v: unknown) => <Badge variant={v === 'active' ? 'success' : 'secondary'} className="text-[10px]">{String(v)}</Badge> },
  { key: 'description', label: 'Description' },
  { key: 'current_version_id', label: 'Active Version ID', render: (v: unknown) => <span className="font-mono text-[10px]">{String(v ?? '—')}</span> },
  { key: 'created_at', label: 'Created', render: (v: unknown) => <span className="font-mono text-[11px]">{v ? new Date(String(v)).toLocaleDateString() : '—'}</span> },
  { key: 'updated_at', label: 'Updated', render: (v: unknown) => <span className="font-mono text-[11px]">{v ? new Date(String(v)).toLocaleDateString() : '—'}</span> },
]

export default function PolicyComparePage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()

  const ids = useMemo(() => {
    const raw = searchParams.get('ids') ?? ''
    return raw.split(',').filter(Boolean).slice(0, 3)
  }, [searchParams])

  const { data: policies, loading } = usePolicies(ids)

  if (ids.length < 2) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <Shield className="h-10 w-10 text-text-tertiary" />
        <p className="text-sm text-text-secondary">Select 2–3 policies to compare. Use <code className="font-mono text-xs">?ids=id1,id2</code></p>
        <Button variant="outline" onClick={() => navigate('/policies')}>Back to Policies</Button>
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
        <Shield className="h-5 w-5 text-accent" />
        <h1 className="text-[16px] font-semibold text-text-primary">Compare Policies</h1>
      </div>

      {policies.length < 2 ? (
        <p className="text-sm text-text-secondary">Could not load the selected policies.</p>
      ) : (
        <CompareView entities={policies} fields={COMPARE_FIELDS} />
      )}
    </div>
  )
}
