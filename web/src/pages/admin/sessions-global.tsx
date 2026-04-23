import { useState } from 'react'
import { RefreshCw, AlertCircle, LogOut, Search } from 'lucide-react'
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
import { useActiveSessions, useForceLogoutSession } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { timeAgo } from '@/lib/format'
import { EntityLink } from '@/components/shared/entity-link'
import type { SessionFilters } from '@/types/admin'

function idleColor(seconds: number) {
  if (seconds < 300) return 'success'
  if (seconds < 1800) return 'warning'
  return 'danger'
}

function formatIdle(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

export default function GlobalSessionsPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [filters, setFilters] = useState<SessionFilters>({})
  const [tenantSearch, setTenantSearch] = useState('')

  const { data, isLoading, isError, refetch } = useActiveSessions(filters)
  const forceLogout = useForceLogoutSession()

  const sessions = data?.data ?? []
  const filtered = tenantSearch
    ? sessions.filter(
        (s) =>
          s.tenant_name.toLowerCase().includes(tenantSearch.toLowerCase()) ||
          s.user_email.toLowerCase().includes(tenantSearch.toLowerCase())
      )
    : sessions

  if (!isSuperAdmin && user?.role !== 'tenant_admin') {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">Insufficient permissions.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Active Sessions</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            {isSuperAdmin ? 'All tenants' : 'Your tenant'} — {filtered.length} active
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex items-center gap-2">
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-tertiary" />
          <Input
            placeholder="Search user or tenant…"
            className="pl-9"
            value={tenantSearch}
            onChange={(e) => setTenantSearch(e.target.value)}
          />
        </div>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load sessions.
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
                <TableHead>User</TableHead>
                {isSuperAdmin && <TableHead>Tenant</TableHead>}
                <TableHead>IP</TableHead>
                <TableHead>Browser / OS</TableHead>
                <TableHead>Idle</TableHead>
                <TableHead>Started</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={isSuperAdmin ? 7 : 6} className="text-center py-12 text-text-tertiary">
                    No active sessions found.
                  </TableCell>
                </TableRow>
              ) : (
                filtered.map((s) => (
                  <TableRow key={s.session_id}>
                    <TableCell>
                      <div className="font-medium text-text-primary">{s.user_email}</div>
                      <EntityLink entityType="user" entityId={s.user_id} label={s.user_email} />
                    </TableCell>
                    {isSuperAdmin && (
                      <TableCell className="text-text-secondary">{s.tenant_name}</TableCell>
                    )}
                    <TableCell className="font-mono text-xs text-text-secondary">{s.ip_address}</TableCell>
                    <TableCell className="text-xs text-text-secondary">
                      {s.browser} / {s.os}
                    </TableCell>
                    <TableCell>
                      <Badge variant={idleColor(s.idle_seconds) as 'success' | 'warning' | 'danger'}>
                        {formatIdle(s.idle_seconds)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-text-tertiary">
                      {timeAgo(s.created_at)}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-danger hover:text-danger/80"
                        disabled={forceLogout.isPending}
                        onClick={() => forceLogout.mutate(s.session_id)}
                      >
                        <LogOut className="h-3.5 w-3.5" />
                      </Button>
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
            onClick={() => setFilters((f) => ({ ...f, cursor: data.meta!.cursor }))}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
