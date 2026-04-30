import { Wifi } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { EntityLink } from '@/components/shared'
import { useSIMList } from '@/hooks/use-sims'
import { stateVariant } from '@/lib/sim-utils'

interface AssignedSimsTabProps {
  versionId: string | undefined
}

export function AssignedSimsTab({ versionId }: AssignedSimsTabProps) {
  const {
    data,
    isLoading,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useSIMList(
    versionId ? { policy_version_id: versionId } : {}
  )

  const sims = data?.pages.flatMap((p) => p.data) ?? []

  if (!versionId) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <Wifi className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
        <p className="text-[13px] text-text-secondary">No active version selected</p>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    )
  }

  if (sims.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <Wifi className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
        <p className="text-[13px] text-text-secondary">No SIMs assigned to this policy version</p>
      </div>
    )
  }

  return (
    <div className="p-4">
      <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
        Assigned SIMs — {sims.length}{hasNextPage ? '+' : ''} total
      </p>
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border-subtle hover:bg-transparent">
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">ICCID</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">IMSI</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">State</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Operator</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sims.map((sim) => (
            <TableRow key={sim.id} className="hover:bg-bg-hover transition-colors duration-150">
              <TableCell className="py-2.5">
                <EntityLink entityType="sim" entityId={sim.id} label={sim.iccid} truncate />
              </TableCell>
              <TableCell className="py-2.5">
                <span className="text-[12px] font-mono text-text-secondary">{sim.imsi}</span>
              </TableCell>
              <TableCell className="py-2.5">
                <Badge variant={stateVariant(sim.state)} className="text-[10px]">{sim.state}</Badge>
              </TableCell>
              <TableCell className="py-2.5">
                <EntityLink entityType="operator" entityId={sim.operator_id} label={sim.operator_name} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {hasNextPage && (
        <div className="mt-3 text-center">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
          >
            {isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        </div>
      )}
    </div>
  )
}
