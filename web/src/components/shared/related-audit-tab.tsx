import * as React from 'react'
import { Link } from 'react-router-dom'
import { ChevronRight, ChevronDown, ArrowRight } from 'lucide-react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { EntityLink } from './entity-link'
import { useAuditList } from '@/hooks/use-audit'
import type { AuditLog } from '@/types/audit'
import { timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

interface RelatedAuditTabProps {
  entityId: string
  entityType: string
  maxRows?: number
}

function actionVariant(action: string): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  if (action.includes('create') || action.includes('activate') || action.includes('enable')) return 'success'
  if (action.includes('suspend') || action.includes('disable')) return 'warning'
  if (action.includes('terminate') || action.includes('delete') || action.includes('disconnect') || action.includes('remediat') || action.includes('dismiss')) return 'danger'
  return 'secondary'
}

function AuditRowExpandable({ entry }: { entry: AuditLog }) {
  const [expanded, setExpanded] = React.useState(false)

  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-bg-hover transition-colors duration-150"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
      >
        <TableCell className="w-6 py-2 pl-3">
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5 text-text-tertiary" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 text-text-tertiary" />
          )}
        </TableCell>
        <TableCell className="py-2">
          <span
            className="text-[11px] text-text-tertiary font-mono"
            title={new Date(entry.created_at).toISOString()}
          >
            {timeAgo(entry.created_at)}
          </span>
        </TableCell>
        <TableCell className="py-2">
          <Badge variant={actionVariant(entry.action)} className="text-[10px] px-1.5 py-0.5">
            {entry.action}
          </Badge>
        </TableCell>
        <TableCell className="py-2">
          {entry.user_id ? (
            <EntityLink entityType="user" entityId={entry.user_id} truncate />
          ) : (
            <span className="text-[11px] text-text-tertiary italic">system</span>
          )}
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="bg-bg-surface border-b border-border-subtle">
          <TableCell colSpan={4} className="py-2 px-4">
            <pre className="text-[11px] font-mono bg-bg-primary p-2.5 rounded-[var(--radius-sm)] border border-border overflow-x-auto max-h-40 text-text-secondary whitespace-pre-wrap break-all">
              {entry.diff
                ? JSON.stringify(entry.diff, null, 2)
                : 'No diff data available'}
            </pre>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

export function RelatedAuditTab({
  entityId,
  entityType,
  maxRows = 20,
}: RelatedAuditTabProps) {
  const { data, isLoading, isError } = useAuditList({
    entity_id: entityId,
    entity_type: entityType,
  })

  const entries = React.useMemo(() => {
    const allEntries = data?.pages.flatMap((p) => p.data) ?? []
    return allEntries.slice(0, maxRows)
  }, [data, maxRows])

  if (isLoading) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <p className="text-[13px] text-danger mb-2">Failed to load audit entries</p>
        <p className="text-[11px] text-text-tertiary">Please try refreshing the page</p>
      </div>
    )
  }

  if (entries.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-center">
        <p className="text-[13px] text-text-secondary mb-1">No audit entries for this entity yet.</p>
        <p className="text-[11px] text-text-tertiary">Actions on this {entityType} will appear here.</p>
      </div>
    )
  }

  return (
    <div className="space-y-0">
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border-subtle hover:bg-transparent">
            <TableHead className="w-6" />
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Time</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Action</TableHead>
            <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Actor</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((entry) => (
            <AuditRowExpandable key={entry.id} entry={entry} />
          ))}
        </TableBody>
      </Table>
      <div className="flex justify-end px-3 py-2 border-t border-border-subtle">
        <Link
          to={`/audit?entity_id=${entityId}&entity_type=${entityType}`}
          className="inline-flex items-center gap-1 text-[11px] text-accent hover:text-accent/80 transition-colors duration-200"
        >
          View all in Audit Log
          <ArrowRight className="h-3 w-3" />
        </Link>
      </div>
    </div>
  )
}
