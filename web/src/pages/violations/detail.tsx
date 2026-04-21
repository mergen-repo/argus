import * as React from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  FileWarning,
  Ban,
  ArrowUpRight,
  CheckCircle2,
  ExternalLink,
  Shield,
  AlertCircle,
  Clock,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { InfoRow } from '@/components/ui/info-row'
import { EntityLink, RelatedAuditTab, FavoriteToggle } from '@/components/shared'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { useViolation, useRemediate } from '@/hooks/use-violation-detail'
import { timeAgo } from '@/lib/format'
import { toast } from 'sonner'
import { useUIStore } from '@/stores/ui'

type RemediateAction = 'suspend_sim' | 'escalate' | 'dismiss' | null

const ACTION_LABELS: Record<string, string> = {
  suspend_sim: 'Suspend SIM',
  escalate: 'Escalate',
  dismiss: 'Dismiss',
}

export default function ViolationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = React.useState('overview')
  const [actionOpen, setActionOpen] = React.useState<RemediateAction>(null)
  const [reason, setReason] = React.useState('')

  const { data: violation, isLoading, isError } = useViolation(id)
  const remediate = useRemediate(id)
  const { addRecentItem } = useUIStore()

  React.useEffect(() => {
    if (violation && id) {
      addRecentItem({ type: 'violation', id, label: violation.policy_name || id.slice(0, 8), path: `/violations/${id}` })
    }
  }, [violation, id, addRecentItem])

  function handleRemediate() {
    if (!actionOpen || !id) return
    remediate.mutate(
      { action: actionOpen, reason },
      {
        onSuccess: () => {
          toast.success(`Violation ${ACTION_LABELS[actionOpen].toLowerCase()} successful`)
          setActionOpen(null)
          setReason('')
        },
        onError: (err: unknown) => {
          const msg = (err as { response?: { data?: { error?: { message?: string } } } })
            ?.response?.data?.error?.message
          toast.error(msg ?? 'Action failed')
        },
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

  if (isError || !violation) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <AlertCircle className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">Violation not found</p>
        <Button variant="outline" onClick={() => navigate('/violations')}>
          <ArrowLeft className="h-4 w-4 mr-2" /> Back to Violations
        </Button>
      </div>
    )
  }

  const isOpen = !violation.acknowledged_at

  return (
    <div className="p-6 space-y-6">
      <Breadcrumb
        items={[
          { label: 'Violations', href: '/violations' },
          { label: violation.violation_type },
        ]}
      />

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)} className="mt-0.5">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2 mb-1">
              <FileWarning className={`h-5 w-5 ${isOpen ? 'text-danger' : 'text-text-tertiary'}`} />
              <h1 className="text-[15px] font-semibold text-text-primary">
                {violation.violation_type.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())}
              </h1>
            </div>
            <div className="flex items-center gap-2">
              <SeverityBadge severity={violation.severity} />
              <Badge variant={isOpen ? 'warning' : 'secondary'} className="text-[11px]">
                {isOpen ? 'open' : 'acknowledged'}
              </Badge>
              <span className="text-[11px] text-text-tertiary flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {timeAgo(violation.created_at)}
              </span>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <FavoriteToggle
            type="violation"
            id={id ?? ''}
            label={violation.policy_name || (id?.slice(0, 8) ?? '')}
            path={`/violations/${id}`}
          />
          {isOpen && (
            <>
            <Button
              variant="destructive"
              size="sm"
              className="gap-1.5"
              onClick={() => setActionOpen('suspend_sim')}
            >
              <Ban className="h-3.5 w-3.5" />
              Suspend SIM
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => navigate(`/policies/${violation.policy_id}`)}
            >
              <ExternalLink className="h-3.5 w-3.5" />
              Review Policy
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => setActionOpen('escalate')}
            >
              <ArrowUpRight className="h-3.5 w-3.5" />
              Escalate
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5 text-success"
              onClick={() => setActionOpen('dismiss')}
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              Dismiss
            </Button>
            </>
          )}
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <FileWarning className="h-3.5 w-3.5" />
            Overview
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
                <CardTitle className="text-[13px] font-medium text-text-primary">Violation Details</CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                <InfoRow label="Type" value={<span className="text-[12px] font-mono text-text-primary">{violation.violation_type}</span>} />
                <InfoRow label="Severity" value={<SeverityBadge severity={violation.severity} />} />
                <InfoRow label="Action Taken" value={<span className="text-[12px] text-text-secondary">{violation.action_taken}</span>} />
                <InfoRow label="Rule Index" value={<span className="text-[12px] font-mono text-text-secondary">Rule #{violation.rule_index}</span>} />
                <InfoRow label="Occurred" value={<span className="text-[12px] text-text-secondary" title={violation.created_at}>{timeAgo(violation.created_at)}</span>} />
                {violation.acknowledged_at && (
                  <InfoRow label="Acknowledged" value={<span className="text-[12px] text-success">{timeAgo(violation.acknowledged_at)}</span>} />
                )}
              </CardContent>
            </Card>

            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary">Linked Entities</CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                {violation.sim_id && (
                  <InfoRow label="SIM" value={<EntityLink entityType="sim" entityId={violation.sim_id.toString()} label={violation.sim_iccid} />} />
                )}
                {violation.policy_id && (
                  <InfoRow label="Policy" value={<EntityLink entityType="policy" entityId={violation.policy_id.toString()} label={violation.policy_name} />} />
                )}
                {violation.session_id && (
                  <InfoRow label="Session" value={<EntityLink entityType="session" entityId={violation.session_id.toString()} truncate />} />
                )}
                {violation.details && Object.keys(violation.details).length > 0 && (
                  <div className="mt-3">
                    <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-2">Details</p>
                    <pre className="text-[11px] font-mono bg-bg-primary p-2.5 rounded-[var(--radius-sm)] border border-border overflow-x-auto max-h-32 text-text-secondary whitespace-pre-wrap break-all">
                      {JSON.stringify(violation.details, null, 2)}
                    </pre>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="audit" className="mt-4">
          {id && <RelatedAuditTab entityId={id} entityType="policy_violation" />}
        </TabsContent>
      </Tabs>

      <Dialog open={actionOpen !== null} onOpenChange={() => setActionOpen(null)}>
        <DialogContent onClose={() => setActionOpen(null)}>
          <DialogHeader>
            <DialogTitle>
              {actionOpen ? ACTION_LABELS[actionOpen] : ''} Violation
            </DialogTitle>
            {actionOpen === 'suspend_sim' && (
              <DialogDescription>
                This will suspend the SIM associated with this violation and acknowledge the violation.
              </DialogDescription>
            )}
          </DialogHeader>
          <div className="py-2">
            <label className="text-[12px] font-medium text-text-secondary block mb-1.5">
              Reason {actionOpen === 'dismiss' ? '(optional)' : '(required)'}
            </label>
            <Input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={actionOpen === 'suspend_sim' ? 'policy violation' : actionOpen === 'escalate' ? 'needs review' : 'false positive'}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setActionOpen(null)}>Cancel</Button>
            <Button
              variant={actionOpen === 'suspend_sim' ? 'destructive' : 'default'}
              onClick={handleRemediate}
              disabled={remediate.isPending || (actionOpen !== 'dismiss' && !reason.trim())}
            >
              {remediate.isPending ? 'Processing…' : actionOpen ? ACTION_LABELS[actionOpen] : 'Confirm'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
