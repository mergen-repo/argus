import { useState, Fragment } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AlertCircle, AlertTriangle, Info, RefreshCw, ChevronDown, ChevronRight,
  Shield, CheckCircle, Eye,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Select } from '@/components/ui/select'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Spinner } from '@/components/ui/spinner'
import { Skeleton } from '@/components/ui/skeleton'
import { useAnomalyList, useAnomalyStateUpdate, type AnomalyFilters } from '@/hooks/use-analytics'
import type { Anomaly, AnomalyState, AnomalySeverity } from '@/types/analytics'
import { timeAgo } from '@/lib/format'

const SEVERITY_OPTIONS = [
  { value: '', label: 'All Severities' },
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
]

const STATE_OPTIONS = [
  { value: '', label: 'All States' },
  { value: 'open', label: 'Open' },
  { value: 'acknowledged', label: 'Acknowledged' },
  { value: 'resolved', label: 'Resolved' },
  { value: 'false_positive', label: 'False Positive' },
]

const TYPE_OPTIONS = [
  { value: '', label: 'All Types' },
  { value: 'velocity_anomaly', label: 'Velocity Anomaly' },
  { value: 'location_anomaly', label: 'Location Anomaly' },
  { value: 'usage_spike', label: 'Usage Spike' },
  { value: 'auth_flood', label: 'Auth Flood' },
  { value: 'sim_cloning', label: 'SIM Cloning' },
  { value: 'credential_stuffing', label: 'Credential Stuffing' },
]

function severityIcon(severity: string) {
  switch (severity) {
    case 'critical':
      return <AlertCircle className="h-4 w-4 text-danger flex-shrink-0" />
    case 'warning':
      return <AlertTriangle className="h-4 w-4 text-warning flex-shrink-0" />
    default:
      return <Info className="h-4 w-4 text-accent flex-shrink-0" />
  }
}

function severityVariant(severity: string): 'danger' | 'warning' | 'default' {
  switch (severity) {
    case 'critical': return 'danger'
    case 'warning': return 'warning'
    default: return 'default'
  }
}

function stateVariant(state: string): 'default' | 'success' | 'secondary' | 'outline' {
  switch (state) {
    case 'open': return 'default'
    case 'acknowledged': return 'secondary'
    case 'resolved': return 'success'
    case 'false_positive': return 'outline'
    default: return 'default'
  }
}

function AnomalySkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-6 w-48" />
      <div className="flex gap-3">
        <Skeleton className="h-9 w-36" />
        <Skeleton className="h-9 w-36" />
        <Skeleton className="h-9 w-36" />
      </div>
      <Card>
        <CardContent className="pt-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full mb-2" />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
        <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
        <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load anomalies</h2>
        <p className="text-sm text-text-secondary mb-4">Unable to fetch anomaly data. Please try again.</p>
        <Button onClick={onRetry} variant="outline" className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    </div>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <Shield className="h-10 w-10 text-text-tertiary" />
      <p className="text-sm text-text-secondary">No anomalies found for current filters</p>
    </div>
  )
}

function DetailRow({ anomaly }: { anomaly: Anomaly }) {
  return (
    <TableRow className="bg-bg-surface/50">
      <TableCell colSpan={6} className="py-3">
        <div className="space-y-3 pl-8">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <div>
              <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Type</span>
              <span className="text-xs text-text-primary">{anomaly.type}</span>
            </div>
            <div>
              <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Source</span>
              <span className="text-xs text-text-primary">{anomaly.source || 'N/A'}</span>
            </div>
            <div>
              <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Detected</span>
              <span className="text-xs text-text-primary font-mono">
                {new Date(anomaly.detected_at).toLocaleString()}
              </span>
            </div>
            {anomaly.acknowledged_at && (
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Acknowledged</span>
                <span className="text-xs text-text-primary font-mono">
                  {new Date(anomaly.acknowledged_at).toLocaleString()}
                </span>
              </div>
            )}
            {anomaly.resolved_at && (
              <div>
                <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-0.5">Resolved</span>
                <span className="text-xs text-text-primary font-mono">
                  {new Date(anomaly.resolved_at).toLocaleString()}
                </span>
              </div>
            )}
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-wider text-text-tertiary block mb-1">Details (JSON)</span>
            <pre className="text-xs font-mono text-text-secondary bg-bg-primary rounded-[var(--radius-sm)] p-3 overflow-x-auto max-h-[200px] border border-border">
              {JSON.stringify(anomaly.details, null, 2)}
            </pre>
          </div>
        </div>
      </TableCell>
    </TableRow>
  )
}

function AnomalyRow({
  anomaly,
  isExpanded,
  onToggle,
  onAcknowledge,
  onResolve,
  isUpdating,
}: {
  anomaly: Anomaly
  isExpanded: boolean
  onToggle: () => void
  onAcknowledge: () => void
  onResolve: () => void
  isUpdating: boolean
}) {
  const navigate = useNavigate()

  return (
    <>
      <TableRow className="cursor-pointer" onClick={onToggle}>
        <TableCell className="w-8">
          {isExpanded ? (
            <ChevronDown className="h-4 w-4 text-text-tertiary" />
          ) : (
            <ChevronRight className="h-4 w-4 text-text-tertiary" />
          )}
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-2">
            {severityIcon(anomaly.severity)}
            <Badge variant={severityVariant(anomaly.severity)}>
              {anomaly.severity}
            </Badge>
          </div>
        </TableCell>
        <TableCell>
          <span className="text-xs">{anomaly.type.replace(/_/g, ' ')}</span>
        </TableCell>
        <TableCell>
          {anomaly.sim_id ? (
            <span
              className="font-mono text-xs text-accent hover:underline cursor-pointer"
              onClick={(e) => {
                e.stopPropagation()
                navigate(`/sims/${anomaly.sim_id}`)
              }}
            >
              {anomaly.sim_iccid || anomaly.sim_id.slice(0, 8) + '...'}
            </span>
          ) : (
            <span className="text-xs text-text-tertiary">N/A</span>
          )}
        </TableCell>
        <TableCell>
          <span className="text-xs font-mono text-text-secondary">{timeAgo(anomaly.detected_at)}</span>
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
            <Badge variant={stateVariant(anomaly.state)} className="text-[10px]">
              {anomaly.state}
            </Badge>
            {anomaly.state === 'open' && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs gap-1"
                onClick={onAcknowledge}
                disabled={isUpdating}
              >
                {isUpdating ? <Spinner className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                Ack
              </Button>
            )}
            {(anomaly.state === 'open' || anomaly.state === 'acknowledged') && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs gap-1"
                onClick={onResolve}
                disabled={isUpdating}
              >
                {isUpdating ? <Spinner className="h-3 w-3" /> : <CheckCircle className="h-3 w-3" />}
                Resolve
              </Button>
            )}
          </div>
        </TableCell>
      </TableRow>
      {isExpanded && <DetailRow anomaly={anomaly} />}
    </>
  )
}

export default function AnalyticsAnomaliesPage() {
  const [typeFilter, setTypeFilter] = useState('')
  const [severityFilter, setSeverityFilter] = useState<AnomalySeverity>('')
  const [stateFilter, setStateFilter] = useState<AnomalyState>('')
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const [updatingId, setUpdatingId] = useState<string | null>(null)

  const filters: AnomalyFilters = {
    type: typeFilter || undefined,
    severity: severityFilter || undefined,
    state: stateFilter || undefined,
  }

  const {
    data, isLoading, isError, refetch, hasNextPage, fetchNextPage, isFetchingNextPage,
  } = useAnomalyList(filters)
  const updateState = useAnomalyStateUpdate()

  const anomalies = data?.pages.flatMap((p) => p.data) ?? []

  const toggleExpanded = (id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const handleStateChange = async (id: string, newState: string) => {
    setUpdatingId(id)
    try {
      await updateState.mutateAsync({ id, state: newState })
    } finally {
      setUpdatingId(null)
    }
  }

  if (isLoading) return <AnomalySkeleton />
  if (isError) return <ErrorState onRetry={() => refetch()} />

  const criticalCount = anomalies.filter((a) => a.severity === 'critical' && a.state === 'open').length
  const warningCount = anomalies.filter((a) => a.severity === 'warning' && a.state === 'open').length
  const openCount = anomalies.filter((a) => a.state === 'open').length

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-3">
          <h1 className="text-[16px] font-semibold text-text-primary">Analytics &mdash; Anomalies</h1>
          {openCount > 0 && (
            <Badge variant="danger" className="text-[10px]">{openCount} open</Badge>
          )}
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1">
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Select
          options={TYPE_OPTIONS}
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="w-44"
        />
        <Select
          options={SEVERITY_OPTIONS}
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value as AnomalySeverity)}
          className="w-36"
        />
        <Select
          options={STATE_OPTIONS}
          value={stateFilter}
          onChange={(e) => setStateFilter(e.target.value as AnomalyState)}
          className="w-36"
        />
        {(criticalCount > 0 || warningCount > 0) && (
          <div className="flex items-center gap-2 ml-auto">
            {criticalCount > 0 && (
              <div className="flex items-center gap-1 text-xs">
                <AlertCircle className="h-3.5 w-3.5 text-danger" />
                <span className="text-danger font-mono">{criticalCount} critical</span>
              </div>
            )}
            {warningCount > 0 && (
              <div className="flex items-center gap-1 text-xs">
                <AlertTriangle className="h-3.5 w-3.5 text-warning" />
                <span className="text-warning font-mono">{warningCount} warning</span>
              </div>
            )}
          </div>
        )}
      </div>

      {anomalies.length === 0 ? (
        <EmptyState />
      ) : (
        <Card>
          <CardContent className="pt-0 px-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead>Severity</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>SIM</TableHead>
                  <TableHead>Detected</TableHead>
                  <TableHead>State / Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {anomalies.map((anomaly) => (
                  <Fragment key={anomaly.id}>
                    <AnomalyRow
                      anomaly={anomaly}
                      isExpanded={expandedIds.has(anomaly.id)}
                      onToggle={() => toggleExpanded(anomaly.id)}
                      onAcknowledge={() => handleStateChange(anomaly.id, 'acknowledged')}
                      onResolve={() => handleStateChange(anomaly.id, 'resolved')}
                      isUpdating={updatingId === anomaly.id}
                    />
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {hasNextPage && (
        <div className="flex justify-center pt-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="gap-2"
          >
            {isFetchingNextPage ? <Spinner className="h-3.5 w-3.5" /> : null}
            Load More
          </Button>
        </div>
      )}
    </div>
  )
}
