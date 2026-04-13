import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { Network } from 'lucide-react'
import { CopyableId } from '@/components/shared'
import type { ListResponse } from '@/types/sim'

interface IPHistoryEntry {
  id: string
  state?: string
  from_state?: string
  to_state?: string
  changed_at?: string
  created_at?: string
  ip_address?: string
  changed_by?: string
  reason?: string
}

interface IPHistoryTabProps {
  simId: string
}

function useIPHistory(simId: string) {
  return useQuery({
    queryKey: ['sims', simId, 'ip-history'],
    queryFn: async () => {
      const res = await api.get<ListResponse<IPHistoryEntry>>(`/sims/${simId}/history?limit=50`)
      return (res.data.data ?? []).filter((e) => e.ip_address || e.from_state?.includes('ip') || e.to_state?.includes('ip') || e.state?.includes('ip'))
    },
    staleTime: 60_000,
    enabled: !!simId,
  })
}

export function IPHistoryTab({ simId }: IPHistoryTabProps) {
  const { data: entries = [], isLoading, isError } = useIPHistory(simId)

  if (isLoading) {
    return (
      <div className="space-y-2 mt-4 p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-10 mt-4 text-center">
        <p className="text-[13px] text-danger">Failed to load IP history</p>
      </div>
    )
  }

  if (entries.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 mt-4 text-center">
        <Network className="h-10 w-10 text-text-tertiary mx-auto mb-3 opacity-40" />
        <p className="text-[13px] text-text-secondary mb-1">No IP allocation history available</p>
        <p className="text-[11px] text-text-tertiary">IP allocations will appear here when sessions are established</p>
      </div>
    )
  }

  return (
    <div className="mt-4">
      <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
        IP Allocation History
      </p>
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border-subtle hover:bg-transparent">
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">IP Address</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">State</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Time</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Reason</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((entry) => (
            <TableRow key={entry.id} className="hover:bg-bg-hover transition-colors duration-150">
              <TableCell className="py-2.5">
                {entry.ip_address ? (
                  <CopyableId value={entry.ip_address} mono />
                ) : (
                  <span className="text-[11px] text-text-tertiary">-</span>
                )}
              </TableCell>
              <TableCell className="py-2.5">
                <span className="text-[12px] text-text-primary">
                  {entry.to_state ?? entry.state ?? '-'}
                </span>
              </TableCell>
              <TableCell className="py-2.5">
                <span className="text-[11px] text-text-tertiary">
                  {new Date(entry.changed_at ?? entry.created_at ?? '').toLocaleString()}
                </span>
              </TableCell>
              <TableCell className="py-2.5">
                <span className="text-[11px] text-text-secondary">{entry.reason ?? '-'}</span>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
