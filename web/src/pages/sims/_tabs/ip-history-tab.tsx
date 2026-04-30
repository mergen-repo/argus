import { Network, CheckCircle2 } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { CopyableId } from '@/components/shared'
import { useSIMCurrentIP } from '@/hooks/use-sims'

interface IPHistoryTabProps {
  simId: string
}

// Scope-down (STORY-082 follow-up, plan jiggly-shimmying-harp):
// full IP allocation history requires a dedicated ip_assignments table +
// RADIUS hook + list endpoint — tracked as STORY-087. Until then this
// tab shows the SIM's current reserved IP allocation with its pool
// metadata, which covers the primary operator question ("which IP does
// this SIM have?"). The "Full history" section below is an explicit
// placeholder so users know more is coming.
export function IPHistoryTab({ simId }: IPHistoryTabProps) {
  const { data, isLoading, isError } = useSIMCurrentIP(simId)

  return (
    <div className="mt-4 space-y-6">
      <section>
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
          Current Allocation
        </p>

        {isLoading && <Skeleton className="h-24 w-full" />}

        {isError && (
          <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface p-4">
            <p className="text-[13px] text-danger">Failed to load current IP</p>
          </div>
        )}

        {!isLoading && !isError && data && !data.allocated && (
          <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface p-6 text-center">
            <Network className="h-8 w-8 text-text-tertiary mx-auto mb-2 opacity-40" />
            <p className="text-[13px] text-text-secondary">No IP currently allocated</p>
            <p className="text-[11px] text-text-tertiary mt-1">An IP will be assigned on the next authentication</p>
          </div>
        )}

        {!isLoading && !isError && data && data.allocated && (
          <div className="rounded-[var(--radius-md)] border border-accent/40 bg-accent/5 p-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">IP Address</p>
                {data.address_v4 ? (
                  <CopyableId value={data.address_v4} mono />
                ) : (
                  <span className="text-[13px] text-text-primary">{data.address_v6 ?? '-'}</span>
                )}
              </div>
              <div>
                <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">State</p>
                <div className="flex items-center gap-1.5">
                  <CheckCircle2 className="h-3.5 w-3.5 text-success" />
                  <Badge variant={data.state === 'reserved' ? 'default' : 'default'}>
                    {data.state ?? '-'}
                  </Badge>
                </div>
              </div>
              <div>
                <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">Allocation Type</p>
                <span className="text-[13px] text-text-primary capitalize">{data.allocation_type ?? '-'}</span>
              </div>
              <div>
                <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">Allocated At</p>
                <span className="text-[12px] text-text-secondary">
                  {data.allocated_at ? new Date(data.allocated_at).toLocaleString() : '-'}
                </span>
              </div>
              {data.pool_name && (
                <div>
                  <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">Pool</p>
                  <span className="text-[13px] text-text-primary">{data.pool_name}</span>
                </div>
              )}
              {data.pool_cidr_v4 && (
                <div>
                  <p className="text-[10px] uppercase tracking-[0.5px] text-text-tertiary mb-1">Pool CIDR</p>
                  <CopyableId value={data.pool_cidr_v4} mono />
                </div>
              )}
            </div>
          </div>
        )}
      </section>

      <section>
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
          Full Allocation History
        </p>
        <div className="rounded-[var(--radius-md)] border border-dashed border-border bg-bg-surface p-6 text-center">
          <p className="text-[12px] text-text-secondary mb-1">Historical IP changes tracking coming soon</p>
          <p className="text-[11px] text-text-tertiary">Every allocation / release / rotation will be recorded here once the <code className="px-1 bg-bg-elevated rounded text-[10px]">ip_assignments</code> feature lands.</p>
        </div>
      </section>
    </div>
  )
}
