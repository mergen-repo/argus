import * as React from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Wifi,
  WifiOff,
  Timer,
  Activity,
  Shield,
  Zap,
  BarChart3,
  AlertCircle,
  FileBarChart,
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
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { InfoRow } from '@/components/ui/info-row'
import { RATBadge } from '@/components/ui/rat-badge'
import { EntityLink, CopyableId, RelatedAuditTab, RelatedAlertsPanel, FavoriteToggle } from '@/components/shared'
import { useSession, useDisconnectSession } from '@/hooks/use-sessions'
import { formatBytes, timeAgo } from '@/lib/format'
import { toast } from 'sonner'
import { useUIStore } from '@/stores/ui'

function useLiveDuration(startedAt: string | undefined): string {
  const [elapsed, setElapsed] = React.useState('')

  React.useEffect(() => {
    if (!startedAt) return
    function update() {
      const diff = Math.floor((Date.now() - new Date(startedAt!).getTime()) / 1000)
      const h = Math.floor(diff / 3600)
      const m = Math.floor((diff % 3600) / 60)
      const s = diff % 60
      setElapsed(`${String(h).padStart(2, '0')}h ${String(m).padStart(2, '0')}m ${String(s).padStart(2, '0')}s`)
    }
    update()
    const id = setInterval(update, 1000)
    return () => clearInterval(id)
  }, [startedAt])

  return elapsed
}

export default function SessionDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = React.useState('overview')
  const [disconnectOpen, setDisconnectOpen] = React.useState(false)
  const [disconnectReason, setDisconnectReason] = React.useState('')

  const { data: session, isLoading, isError } = useSession(id)
  const disconnectMutation = useDisconnectSession()
  const liveDuration = useLiveDuration(session?.state === 'active' ? session.started_at : undefined)
  const { addRecentItem } = useUIStore()

  React.useEffect(() => {
    if (session && id) {
      addRecentItem({ type: 'session', id, label: session.imsi || id.slice(0, 8), path: `/sessions/${id}` })
    }
  }, [session, id, addRecentItem])

  function handleDisconnect() {
    if (!id) return
    disconnectMutation.mutate(
      { sessionId: id, reason: disconnectReason || 'admin_disconnect' },
      {
        onSuccess: () => {
          toast.success('Session disconnected')
          setDisconnectOpen(false)
        },
        onError: () => toast.error('Failed to disconnect session'),
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

  if (isError || !session) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <AlertCircle className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">Session not found</p>
        <p className="text-[13px] text-text-secondary mb-6">This session may have been terminated and pruned.</p>
        <Button variant="outline" onClick={() => navigate('/sessions')}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back to Sessions
        </Button>
      </div>
    )
  }

  const isActive = session.state === 'active'
  const quotaPct = session.quota_usage?.pct ?? 0

  return (
    <div className="p-6 space-y-6">
      <Breadcrumb
        items={[
          { label: 'Sessions', href: '/sessions' },
          { label: session.id.slice(0, 8) + '…' },
        ]}
      />

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)} className="mt-0.5">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2 mb-1">
              {isActive ? (
                <span className="relative flex h-2.5 w-2.5">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-success opacity-75" />
                  <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-success" />
                </span>
              ) : (
                <span className="inline-flex rounded-full h-2.5 w-2.5 bg-text-tertiary" />
              )}
              <Badge variant={isActive ? 'success' : 'secondary'} className="text-[11px]">
                {session.state}
              </Badge>
              {session.rat_type && <RATBadge ratType={session.rat_type} />}
              {isActive && <span className="text-[12px] font-mono text-text-secondary">{liveDuration}</span>}
            </div>
            <h1 className="text-[15px] font-semibold text-text-primary font-mono">
              <CopyableId value={session.id} />
            </h1>
            <div className="flex items-center gap-3 mt-1">
              {session.sim_id && (
                <span className="text-[12px] text-text-secondary">
                  SIM: <EntityLink entityType="sim" entityId={session.sim_id} label={session.imsi || session.sim_id} />
                </span>
              )}
              {session.operator_id && (
                <span className="text-[12px] text-text-secondary">
                  Operator: <EntityLink entityType="operator" entityId={session.operator_id} label={session.operator_name} />
                </span>
              )}
              {session.apn_id && (
                <span className="text-[12px] text-text-secondary">
                  APN: <EntityLink entityType="apn" entityId={session.apn_id} label={session.apn_name} />
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <FavoriteToggle
            type="session"
            id={id ?? ''}
            label={session.imsi || (id?.slice(0, 8) ?? '')}
            path={`/sessions/${id}`}
          />
          {/*
            FIX-214 deep-link → CDR Explorer filtered to this session.
            Session detail's per-session CDR panel is stubbed until FIX-248 lands
            (reports subsystem + embedded CDR pane). Until then, this button is
            the canonical path for drilling into a session's CDR timeline.
          */}
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              const endedAt = (session as unknown as { ended_at?: string }).ended_at
              const from = session.started_at
              const to = endedAt ?? new Date().toISOString()
              const p = new URLSearchParams()
              p.set('session_id', session.id)
              if (session.sim_id) p.set('sim_id', session.sim_id)
              p.set('from', from)
              p.set('to', to)
              navigate(`/cdrs?${p.toString()}`)
            }}
            className="gap-1.5"
          >
            <FileBarChart className="h-3.5 w-3.5" />
            CDR Kayıtları
          </Button>
          {isActive && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDisconnectOpen(true)}
              className="gap-1.5"
            >
              <WifiOff className="h-3.5 w-3.5" />
              Force Disconnect
            </Button>
          )}
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <Activity className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="sor" className="gap-1.5">
            <Zap className="h-3.5 w-3.5" />
            SoR Decision
          </TabsTrigger>
          <TabsTrigger value="policy" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Policy
          </TabsTrigger>
          <TabsTrigger value="quota" className="gap-1.5">
            <BarChart3 className="h-3.5 w-3.5" />
            Quota
          </TabsTrigger>
          <TabsTrigger value="audit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
          <TabsTrigger value="alerts" className="gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            Alerts
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary flex items-center gap-2">
                  <Wifi className="h-4 w-4 text-text-tertiary" />
                  Connection Details
                </CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                <InfoRow label="NAS IP" value={session.nas_ip ? <CopyableId value={session.nas_ip} mono /> : '-'} />
                {session.rat_type && (
                  <InfoRow label="RAT Type" value={<RATBadge ratType={session.rat_type} />} />
                )}
                <InfoRow label="Framed IP" value={session.framed_ip ? <CopyableId value={session.framed_ip} mono /> : '-'} />
                <InfoRow label="IMSI" value={session.imsi ? <CopyableId value={session.imsi} mono masked /> : '-'} />
                {session.msisdn && (
                  <InfoRow label="MSISDN" value={<CopyableId value={session.msisdn} mono />} />
                )}
                <InfoRow label="Started" value={<span className="text-[12px] text-text-secondary" title={session.started_at}>{timeAgo(session.started_at)}</span>} />
              </CardContent>
            </Card>

            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary flex items-center gap-2">
                  <BarChart3 className="h-4 w-4 text-text-tertiary" />
                  Data Transfer
                </CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                <InfoRow label="Bytes In" value={<span className="text-[12px] font-mono text-success">{formatBytes(session.bytes_in)}</span>} />
                <InfoRow label="Bytes Out" value={<span className="text-[12px] font-mono text-accent">{formatBytes(session.bytes_out)}</span>} />
                <InfoRow label="Duration" value={<span className="text-[12px] font-mono text-text-primary">{isActive ? liveDuration : `${Math.round(session.duration_sec)}s`}</span>} />
                <InfoRow label="Acct Session ID" value={session.acct_session_id ? <CopyableId value={session.acct_session_id} mono /> : '-'} />
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="sor" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">
                Selection of Route Decision
              </CardTitle>
            </CardHeader>
            <CardContent className="p-4">
              {session.sor_decision?.scoring && session.sor_decision.scoring.length > 0 ? (
                <div className="space-y-2">
                  {session.sor_decision.scoring
                    .sort((a, b) => b.score - a.score)
                    .map((entry, i) => {
                      const isChosen = entry.operator_id === session.sor_decision?.chosen_operator_id
                      return (
                        <div
                          key={entry.operator_id}
                          className={`flex items-center gap-3 p-3 rounded-[10px] border ${isChosen ? 'border-accent bg-accent/5' : 'border-border bg-bg-primary'}`}
                        >
                          <span className={`text-[11px] font-mono w-5 ${isChosen ? 'text-accent font-bold' : 'text-text-tertiary'}`}>
                            #{i + 1}
                          </span>
                          <div className="flex-1">
                            <EntityLink entityType="operator" entityId={entry.operator_id} />
                            {entry.reason && (
                              <p className="text-[11px] text-text-tertiary mt-0.5">{entry.reason}</p>
                            )}
                          </div>
                          <span className={`text-[13px] font-mono font-bold ${isChosen ? 'text-accent' : 'text-text-secondary'}`}>
                            {entry.score.toFixed(2)}
                          </span>
                          {isChosen && (
                            <Badge variant="success" className="text-[10px]">chosen</Badge>
                          )}
                        </div>
                      )
                    })}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center py-8 text-center">
                  <Zap className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
                  <p className="text-[13px] text-text-secondary">SoR decision data not available for this session</p>
                  <p className="text-[11px] text-text-tertiary mt-1">Available for sessions created after STORY-065</p>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="policy" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">
                Applied Policy
              </CardTitle>
            </CardHeader>
            <CardContent className="p-4">
              {session.policy_applied?.policy_id ? (
                <div className="space-y-3">
                  <InfoRow label="Policy" value={<EntityLink entityType="policy" entityId={session.policy_applied.policy_id} />} />
                  {session.policy_applied.version_id && (
                    <InfoRow label="Version" value={<span className="text-[12px] font-mono text-text-secondary">{session.policy_applied.version_id}</span>} />
                  )}
                  {session.policy_applied.matched_rules && session.policy_applied.matched_rules.length > 0 && (
                    <div>
                      <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-2">
                        Matched Rules
                      </p>
                      <div className="flex flex-wrap gap-1.5">
                        {session.policy_applied.matched_rules.map((ruleIdx) => (
                          <Badge key={ruleIdx} variant="secondary" className="text-[11px] font-mono">
                            Rule #{ruleIdx}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center py-8 text-center">
                  <Shield className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
                  <p className="text-[13px] text-text-secondary">No policy applied to this session</p>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="quota" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">
                Quota Usage
              </CardTitle>
            </CardHeader>
            <CardContent className="p-4">
              {session.quota_usage ? (
                <div className="space-y-4">
                  <div>
                    <div className="flex justify-between mb-1.5">
                      <span className="text-[12px] text-text-secondary">
                        {formatBytes(session.quota_usage.used_bytes)} used
                      </span>
                      <span className="text-[12px] text-text-secondary">
                        {formatBytes(session.quota_usage.limit_bytes)} limit
                      </span>
                    </div>
                    <div className="w-full bg-bg-primary rounded-full h-3 overflow-hidden">
                      <div
                        className={`h-3 rounded-full transition-all duration-500 ${
                          quotaPct >= 90 ? 'bg-danger' : quotaPct >= 70 ? 'bg-warning' : 'bg-success'
                        }`}
                        style={{ width: `${Math.min(quotaPct, 100)}%` }}
                      />
                    </div>
                    <p className={`text-[13px] font-bold mt-2 ${quotaPct >= 90 ? 'text-danger' : quotaPct >= 70 ? 'text-warning' : 'text-success'}`}>
                      {quotaPct.toFixed(1)}% used
                    </p>
                  </div>
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center py-8 text-center">
                  <BarChart3 className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
                  <p className="text-[13px] text-text-secondary">No quota configured for this session</p>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="audit" className="mt-4">
          {id && <RelatedAuditTab entityId={id} entityType="session" />}
        </TabsContent>

        <TabsContent value="alerts" className="mt-4">
          {session.sim_id && <RelatedAlertsPanel entityId={session.sim_id} entityType="sim" />}
        </TabsContent>
      </Tabs>

      <Dialog open={disconnectOpen} onOpenChange={setDisconnectOpen}>
        <DialogContent onClose={() => setDisconnectOpen(false)}>
          <DialogHeader>
            <DialogTitle>Force Disconnect Session?</DialogTitle>
          </DialogHeader>
          <div className="py-2">
            <label className="text-[12px] font-medium text-text-secondary block mb-1.5">
              Reason (optional)
            </label>
            <Input
              value={disconnectReason}
              onChange={(e) => setDisconnectReason(e.target.value)}
              placeholder="admin_disconnect"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDisconnectOpen(false)}>Cancel</Button>
            <Button
              variant="destructive"
              onClick={handleDisconnect}
              disabled={disconnectMutation.isPending}
            >
              {disconnectMutation.isPending ? 'Disconnecting…' : 'Disconnect'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
