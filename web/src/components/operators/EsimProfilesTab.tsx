import React, { useMemo } from 'react'
import { Cpu, AlertCircle } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import { InfoTooltip } from '@/components/ui/info-tooltip'
import { EntityLink } from '@/components/shared/entity-link'
import { EmptyState } from '@/components/shared/empty-state'
import { useESimList } from '@/hooks/use-esim'
import type { ESimProfileState } from '@/types/esim'

interface EsimProfilesTabProps {
  operatorId: string
}

function profileStateVariant(state: ESimProfileState): 'success' | 'secondary' | 'warning' | 'danger' {
  switch (state) {
    case 'enabled': return 'success'
    case 'available': return 'secondary'
    case 'disabled': return 'warning'
    case 'deleted': return 'danger'
    default: return 'secondary'
  }
}

function profileStateLabel(state: ESimProfileState): string {
  switch (state) {
    case 'enabled': return 'Enabled'
    case 'available': return 'Available'
    case 'disabled': return 'Disabled'
    case 'deleted': return 'Deleted'
    default: return state
  }
}

export const EsimProfilesTab = React.memo(function EsimProfilesTab({
  operatorId,
}: EsimProfilesTabProps) {
  const {
    data,
    isLoading,
    isError,
    refetch,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useESimList({ operator_id: operatorId })

  const profiles = useMemo(
    () => data?.pages.flatMap((p) => p.data) ?? [],
    [data],
  )

  const summary = useMemo(() => {
    const installed = profiles.filter((p) => p.profile_state === 'available').length
    const enabled = profiles.filter((p) => p.profile_state === 'enabled').length
    const disabled = profiles.filter((p) => p.profile_state === 'disabled').length
    return { installed, enabled, disabled }
  }, [profiles])

  if (isError) {
    return (
      <Card className="mt-4">
        <CardContent className="pt-6">
          <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim p-6 text-center">
            <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
            <p className="text-sm text-danger mb-3">Failed to load eSIM profiles.</p>
            <Button size="sm" variant="outline" onClick={() => refetch()}>Retry</Button>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="mt-4 space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">
          eSIM Profiles — {profiles.length}{hasNextPage ? '+' : ''} total
        </p>
        {profiles.length > 0 && (
          <div className="flex items-center gap-3 text-[11px] text-text-secondary font-mono">
            <span>
              Available: <span className="text-text-primary font-semibold">{summary.installed}</span>
            </span>
            <span>·</span>
            <span>
              Enabled: <span className="text-success font-semibold">{summary.enabled}</span>
            </span>
            <span>·</span>
            <span>
              Disabled: <span className="text-warning font-semibold">{summary.disabled}</span>
            </span>
          </div>
        )}
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>
                  <InfoTooltip term="EID">EID</InfoTooltip>
                </TableHead>
                <TableHead>
                  <InfoTooltip term="ICCID">ICCID</InfoTooltip>
                </TableHead>
                <TableHead>State</TableHead>
                <TableHead>SIM</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-36" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-28" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  </TableRow>
                ))}

              {!isLoading && profiles.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5}>
                    <EmptyState
                      icon={Cpu}
                      title="No eSIM profiles on this operator"
                      description="No eSIM profiles have been provisioned for this operator yet."
                    />
                  </TableCell>
                </TableRow>
              )}

              {profiles.map((profile) => (
                <TableRow key={profile.id} className="hover:bg-bg-elevated/50 transition-colors">
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary truncate max-w-[160px] block" title={profile.eid}>
                      {profile.eid.length > 20
                        ? `${profile.eid.slice(0, 10)}…${profile.eid.slice(-8)}`
                        : profile.eid}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">
                      {profile.iccid_on_profile ?? '—'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={profileStateVariant(profile.profile_state)}>
                      {profileStateLabel(profile.profile_state)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <EntityLink
                      entityType="sim"
                      entityId={profile.sim_id}
                      truncate
                      showIcon
                    />
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(profile.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>

      {hasNextPage && (
        <div className="text-center">
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
})
