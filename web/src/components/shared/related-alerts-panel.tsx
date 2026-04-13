import * as React from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, AlertTriangle, Info, ArrowRight, Radio } from 'lucide-react'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Anomaly } from '@/types/analytics'
import type { ListResponse } from '@/types/sim'
import { timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'

interface RelatedAlertsPanelProps {
  entityId: string
  entityType: 'sim' | 'operator' | 'apn' | string
}

function severityIcon(severity: string) {
  if (severity === 'critical') return <AlertCircle className="h-3.5 w-3.5 text-danger flex-shrink-0" />
  if (severity === 'warning') return <AlertTriangle className="h-3.5 w-3.5 text-warning flex-shrink-0" />
  return <Info className="h-3.5 w-3.5 text-accent flex-shrink-0" />
}

function severityVariant(s: string): 'danger' | 'warning' | 'default' | 'secondary' {
  if (s === 'critical') return 'danger'
  if (s === 'warning') return 'warning'
  return 'default'
}

function alertTitle(anomaly: Anomaly): string {
  return (
    (anomaly.type ?? 'unknown')
      .replace(/_/g, ' ')
      .replace(/\b\w/g, (c) => c.toUpperCase())
  )
}

function useRelatedAlerts(entityId: string, entityType: string) {
  return useQuery({
    queryKey: ['anomalies', 'related', entityType, entityId],
    queryFn: async () => {
      const params = new URLSearchParams({ limit: '20' })
      if (entityType === 'sim') params.set('sim_id', entityId)
      else if (entityType === 'operator') params.set('operator_id', entityId)
      else if (entityType === 'apn') params.set('apn_id', entityId)
      const res = await api.get<ListResponse<Anomaly>>(`/analytics/anomalies?${params.toString()}`)
      return res.data.data ?? []
    },
    staleTime: 30_000,
    enabled: !!entityId,
  })
}

function useAckAnomaly() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await api.patch(`/analytics/anomalies/${id}`, { state: 'acknowledged' })
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['anomalies'] })
      toast.success('Alert acknowledged')
    },
    onError: () => toast.error('Failed to acknowledge alert'),
  })
}

function AlertRow({ anomaly }: { anomaly: Anomaly }) {
  const ack = useAckAnomaly()

  return (
    <div className="flex items-start gap-3 px-4 py-3 hover:bg-bg-hover transition-colors duration-150 border-b border-border-subtle last:border-0">
      {severityIcon(anomaly.severity)}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <Link
            to={`/alerts/${anomaly.id}`}
            className="text-[12px] font-medium text-text-primary hover:text-accent transition-colors duration-150 truncate"
          >
            {alertTitle(anomaly)}
          </Link>
          <Badge variant={severityVariant(anomaly.severity)} className="text-[10px]">
            {anomaly.severity}
          </Badge>
        </div>
        <p className="text-[11px] text-text-tertiary">{timeAgo(anomaly.detected_at)}</p>
      </div>
      {anomaly.state === 'open' && (
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-[10px]"
          onClick={() => ack.mutate(anomaly.id)}
          disabled={ack.isPending}
          aria-label="Acknowledge alert"
        >
          Ack
        </Button>
      )}
    </div>
  )
}

export function RelatedAlertsPanel({ entityId, entityType }: RelatedAlertsPanelProps) {
  const { data: allAlerts = [], isLoading, isError } = useRelatedAlerts(entityId, entityType)
  const [tab, setTab] = React.useState('open')

  const now = Date.now()
  const sevenDaysAgo = now - 7 * 24 * 60 * 60 * 1000

  const openAlerts = allAlerts.filter((a) => a.state === 'open')
  const recentResolved = allAlerts.filter(
    (a) => a.state !== 'open' && new Date(a.detected_at).getTime() > sevenDaysAgo,
  )

  return (
    <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
      <CardHeader className="py-3 px-4 border-b border-border-subtle flex flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-2">
          <Radio className="h-4 w-4 text-text-tertiary" />
          <span className="text-[13px] font-medium text-text-primary">Alerts</span>
          {openAlerts.length > 0 && (
            <Badge variant="danger" className="text-[10px] h-4 px-1.5">
              {openAlerts.length} open
            </Badge>
          )}
        </div>
        <Link
          to={`/alerts?${entityType === 'sim' ? 'sim_id' : entityType === 'operator' ? 'operator_id' : 'apn_id'}=${entityId}`}
          className="inline-flex items-center gap-1 text-[11px] text-accent hover:text-accent/80 transition-colors duration-200"
        >
          View all <ArrowRight className="h-3 w-3" />
        </Link>
      </CardHeader>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : isError ? (
          <div className="py-6 text-center">
            <p className="text-[13px] text-danger">Failed to load alerts</p>
          </div>
        ) : allAlerts.length === 0 ? (
          <div className="py-6 text-center">
            <AlertCircle className="h-8 w-8 text-success mx-auto mb-2 opacity-40" />
            <p className="text-[13px] text-text-secondary">No alerts for this entity</p>
            <p className="text-[11px] text-text-tertiary mt-1">All systems nominal</p>
          </div>
        ) : (
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList className="w-full rounded-none border-b border-border-subtle bg-transparent">
              <TabsTrigger value="open" className="text-[11px] flex-1 rounded-none">
                Open ({openAlerts.length})
              </TabsTrigger>
              <TabsTrigger value="recent" className="text-[11px] flex-1 rounded-none">
                Last 7d ({recentResolved.length})
              </TabsTrigger>
            </TabsList>
            <TabsContent value="open" className="mt-0">
              {openAlerts.length === 0 ? (
                <div className="py-6 text-center">
                  <p className="text-[13px] text-success">No open alerts</p>
                </div>
              ) : (
                openAlerts.map((a) => <AlertRow key={a.id} anomaly={a} />)
              )}
            </TabsContent>
            <TabsContent value="recent" className="mt-0">
              {recentResolved.length === 0 ? (
                <div className="py-6 text-center">
                  <p className="text-[13px] text-text-secondary">No resolved alerts in the last 7 days</p>
                </div>
              ) : (
                recentResolved.map((a) => <AlertRow key={a.id} anomaly={a} />)
              )}
            </TabsContent>
          </Tabs>
        )}
      </CardContent>
    </Card>
  )
}
