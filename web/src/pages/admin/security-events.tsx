import { useState } from 'react'
import { RefreshCw, AlertCircle, Shield } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ListResponse } from '@/types/sim'
import type { AuditLog } from '@/types/audit'

const SECURITY_ACTIONS = [
  'auth.login_failed',
  'auth.token_revoked',
  'auth.mfa_bypassed',
  'session.force_logout',
  'killswitch.toggled',
  'user.role_changed',
  'apikey.revoked',
]

function severityFromAction(action: string): 'high' | 'medium' | 'low' {
  if (action.includes('failed') || action.includes('bypassed') || action.includes('force_logout')) return 'high'
  if (action.includes('toggled') || action.includes('revoked') || action.includes('role_changed')) return 'medium'
  return 'low'
}

function severityBadge(action: string) {
  const sev = severityFromAction(action)
  switch (sev) {
    case 'high': return <Badge variant="danger">High</Badge>
    case 'medium': return <Badge variant="warning">Medium</Badge>
    default: return <Badge variant="outline">Low</Badge>
  }
}

export default function SecurityEventsPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const isTenantAdmin = user?.role === 'tenant_admin'
  const [search, setSearch] = useState('')
  const [cursor, setCursor] = useState<string | undefined>()

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['admin', 'security-events', cursor],
    queryFn: async () => {
      const params = new URLSearchParams({ actions: SECURITY_ACTIONS.join(','), limit: '50' })
      if (cursor) params.set('cursor', cursor)
      const res = await api.get<ListResponse<AuditLog>>(`/audit?${params.toString()}`)
      return res.data
    },
    staleTime: 30_000,
  })

  if (!isSuperAdmin && !isTenantAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">Insufficient permissions.</p>
        </div>
      </div>
    )
  }

  const events = (data?.data ?? []).filter((e) =>
    search
      ? e.action.includes(search.toLowerCase()) ||
        (e.user_id ?? '').toLowerCase().includes(search.toLowerCase())
      : true
  )

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Security Events</h1>
          <p className="text-sm text-text-secondary mt-0.5">Authentication failures, role changes, and kill-switch activity</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      <div className="relative max-w-xs">
        <Shield className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-tertiary" />
        <Input
          placeholder="Filter by action or user…"
          className="pl-9"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load security events.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Action</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Actor</TableHead>
                <TableHead>Entity</TableHead>
                <TableHead>Time</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="text-center py-12 text-text-tertiary">
                    No security events found.
                  </TableCell>
                </TableRow>
              ) : (
                events.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell>
                      <span className="font-mono text-xs text-text-primary">{e.action}</span>
                    </TableCell>
                    <TableCell>{severityBadge(e.action)}</TableCell>
                    <TableCell className="text-sm text-text-secondary">
                      {e.user_id ? e.user_id.slice(0, 8) : '—'}
                    </TableCell>
                    <TableCell className="text-xs text-text-tertiary font-mono">
                      {e.entity_type} {e.entity_id ? e.entity_id.slice(0, 8) : ''}
                    </TableCell>
                    <TableCell className="text-xs text-text-tertiary">
                      {timeAgo(e.created_at)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </Card>

      {data?.meta?.has_more && (
        <div className="flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setCursor(data.meta!.cursor)}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
