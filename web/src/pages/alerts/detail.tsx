import * as React from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import {
  ArrowLeft,
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  ArrowUpRight,
  Clock,
  Shield,
  ExternalLink,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { InfoRow } from '@/components/ui/info-row'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { EntityLink, RelatedAuditTab, FavoriteToggle } from '@/components/shared'
import { useAlert, useSimilarAlerts, useUpdateAlertState } from '@/hooks/use-alert-detail'
import { timeAgo } from '@/lib/format'
import { toast } from 'sonner'
import type { Alert } from '@/types/analytics'
import { useUIStore } from '@/stores/ui'

function stateVariant(s: string): 'success' | 'secondary' | 'warning' | 'default' {
  if (s === 'resolved') return 'success'
  if (s === 'acknowledged') return 'warning'
  if (s === 'suppressed') return 'secondary'
  return 'default'
}

function alertTitle(alert: Alert): string {
  if (alert.title) return alert.title
  return (alert.type ?? 'unknown').replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

type ActionType = 'acknowledge' | 'resolve' | 'escalate' | null

export default function AlertDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = React.useState('overview')
  const [actionOpen, setActionOpen] = React.useState<ActionType>(null)
  const [note, setNote] = React.useState('')

  const { data: alert, isLoading, isError } = useAlert(id)
  const { data: similar = [] } = useSimilarAlerts(alert?.type)
  const updateState = useUpdateAlertState()
  const { addRecentItem } = useUIStore()

  React.useEffect(() => {
    if (alert && id) {
      addRecentItem({ type: 'alert', id, label: alertTitle(alert), path: `/alerts/${id}` })
    }
  }, [alert, id, addRecentItem])

  function handleAction() {
    if (!id || !actionOpen) return
    const stateMap: Record<string, string> = {
      acknowledge: 'acknowledged',
      resolve: 'resolved',
      escalate: 'acknowledged',
    }
    updateState.mutate(
      { id, state: stateMap[actionOpen], note },
      {
        onSuccess: () => {
          toast.success(`Alert ${actionOpen}d`)
          setActionOpen(null)
          setNote('')
        },
        onError: () => toast.error('Failed to update alert'),
      },
    )
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (isError || !alert) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <AlertCircle className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">Alert not found</p>
        <Button variant="outline" onClick={() => navigate('/alerts')}>
          <ArrowLeft className="h-4 w-4 mr-2" /> Back to Alerts
        </Button>
      </div>
    )
  }

  const similarOthers = similar.filter((a) => a.id !== alert.id).slice(0, 5)

  const anomalyId = alert.source === 'sim' && typeof alert.meta?.anomaly_id === 'string'
    ? alert.meta.anomaly_id
    : null

  return (
    <div className="p-6 space-y-6">
      <Breadcrumb
        items={[
          { label: 'Alerts', href: '/alerts' },
          { label: alertTitle(alert) },
        ]}
      />

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)} className="mt-0.5">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2 mb-1">
              <SeverityBadge severity={alert.severity} iconOnly />
              <h1 className="text-[15px] font-semibold text-text-primary">{alertTitle(alert)}</h1>
            </div>
            <div className="flex items-center gap-2">
              <SeverityBadge severity={alert.severity} />
              <Badge variant={stateVariant(alert.state)} className="text-[11px]">
                {alert.state}
              </Badge>
              <span className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-text-secondary">
                {alert.source}
              </span>
              <span className="text-[11px] text-text-tertiary flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {timeAgo(alert.fired_at)}
              </span>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <FavoriteToggle
            type="alert"
            id={id ?? ''}
            label={alertTitle(alert)}
            path={`/alerts/${id}`}
          />
          {alert.state === 'open' && (
            <>
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                onClick={() => setActionOpen('acknowledge')}
              >
                <CheckCircle2 className="h-3.5 w-3.5" />
                Acknowledge
              </Button>
              {/* FIX-209 Gate (F-A2): Escalate only for SIM-linked alerts (meta.anomaly_id). */}
              {anomalyId && (
                <Button
                  variant="outline"
                  size="sm"
                  className="gap-1.5"
                  onClick={() => setActionOpen('escalate')}
                >
                  <ArrowUpRight className="h-3.5 w-3.5" />
                  Escalate
                </Button>
              )}
            </>
          )}
          {alert.state !== 'resolved' && (
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5 text-success"
              onClick={() => setActionOpen('resolve')}
            >
              <XCircle className="h-3.5 w-3.5" />
              Resolve
            </Button>
          )}
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="similar" className="gap-1.5">
            <AlertTriangle className="h-3.5 w-3.5" />
            Similar ({similarOthers.length})
          </TabsTrigger>
          <TabsTrigger value="audit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary">Alert Details</CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                <InfoRow label="Type" value={<span className="text-[12px] font-mono text-text-primary">{alert.type}</span>} />
                <InfoRow label="Source" value={
                  <span className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-text-secondary">
                    {alert.source}
                  </span>
                } />
                <InfoRow label="Severity" value={<SeverityBadge severity={alert.severity} />} />
                <InfoRow label="State" value={<Badge variant={stateVariant(alert.state)} className="text-[11px]">{alert.state}</Badge>} />
                <InfoRow label="Fired" value={<span className="text-[12px] text-text-secondary" title={alert.fired_at}>{timeAgo(alert.fired_at)}</span>} />
                {alert.acknowledged_at && (
                  <InfoRow label="Acknowledged" value={<span className="text-[12px] text-text-secondary">{timeAgo(alert.acknowledged_at)}</span>} />
                )}
                {alert.resolved_at && (
                  <InfoRow label="Resolved" value={<span className="text-[12px] text-text-secondary">{timeAgo(alert.resolved_at)}</span>} />
                )}
                {anomalyId && (
                  <InfoRow label="Anomaly" value={
                    <Link
                      to={`/dashboard/analytics?anomaly=${anomalyId}`}
                      className="inline-flex items-center gap-1 text-[12px] text-accent hover:underline"
                    >
                      <ExternalLink className="h-3 w-3" />
                      View anomaly detail
                    </Link>
                  } />
                )}
              </CardContent>
            </Card>

            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary">Affected Resources</CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                {alert.sim_id && (
                  <InfoRow label="SIM" value={<EntityLink entityType="sim" entityId={alert.sim_id} label={alert.sim_id} />} />
                )}
                {alert.operator_id && (
                  <InfoRow label="Operator" value={<EntityLink entityType="operator" entityId={alert.operator_id} label={alert.operator_id} />} />
                )}
                {alert.apn_id && (
                  <InfoRow label="APN" value={<EntityLink entityType="apn" entityId={alert.apn_id} label={alert.apn_id} />} />
                )}
                {!alert.sim_id && !alert.operator_id && !alert.apn_id && (
                  <div className="py-4 text-center">
                    <p className="text-[12px] text-text-tertiary">No specific resource linked</p>
                  </div>
                )}
                {alert.meta && Object.keys(alert.meta).length > 0 && (
                  <div className="mt-3">
                    <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-2">Meta</p>
                    <pre className="text-[11px] font-mono bg-bg-primary p-2.5 rounded-[var(--radius-sm)] border border-border overflow-x-auto max-h-32 text-text-secondary whitespace-pre-wrap break-all">
                      {JSON.stringify(alert.meta, null, 2)}
                    </pre>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="similar" className="mt-4">
          {similarOthers.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-10 text-center">
              <AlertCircle className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
              <p className="text-[13px] text-text-secondary">No similar alerts found in the last 30 days</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="border-b border-border-subtle hover:bg-transparent">
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Type</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Severity</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">State</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Fired</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {similarOthers.map((a) => (
                  <TableRow
                    key={a.id}
                    className="hover:bg-bg-hover transition-colors duration-150 cursor-pointer"
                    onClick={() => navigate(`/alerts/${a.id}`)}
                  >
                    <TableCell className="py-2.5">
                      <EntityLink entityType="alert" entityId={a.id} label={alertTitle(a)} />
                    </TableCell>
                    <TableCell className="py-2.5">
                      <SeverityBadge severity={a.severity} />
                    </TableCell>
                    <TableCell className="py-2.5">
                      <Badge variant={stateVariant(a.state)} className="text-[10px]">{a.state}</Badge>
                    </TableCell>
                    <TableCell className="py-2.5">
                      <span className="text-[11px] text-text-tertiary">{timeAgo(a.fired_at)}</span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </TabsContent>

        <TabsContent value="audit" className="mt-4">
          {id && (
            <RelatedAuditTab
              entityId={anomalyId ?? id}
              entityType={anomalyId ? 'anomaly' : 'alert'}
            />
          )}
        </TabsContent>
      </Tabs>

      <Dialog open={actionOpen !== null} onOpenChange={() => setActionOpen(null)}>
        <DialogContent onClose={() => setActionOpen(null)}>
          <DialogHeader>
            <DialogTitle>
              {actionOpen === 'acknowledge' && 'Acknowledge Alert'}
              {actionOpen === 'resolve' && 'Resolve Alert'}
              {actionOpen === 'escalate' && 'Escalate Alert'}
            </DialogTitle>
          </DialogHeader>
          <div className="py-2">
            <label className="text-[12px] font-medium text-text-secondary block mb-1.5">
              Note (optional)
            </label>
            <Input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Add a note..."
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setActionOpen(null)}>Cancel</Button>
            <Button onClick={handleAction} disabled={updateState.isPending}>
              {updateState.isPending ? 'Updating…' : 'Confirm'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
