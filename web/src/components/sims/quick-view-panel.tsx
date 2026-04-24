import React, { useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { ExternalLink, AlertCircle } from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Sparkline } from '@/components/ui/sparkline'
import { Spinner } from '@/components/ui/spinner'
import { InfoRow } from '@/components/ui/info-row'
import {
  useSIMUsage,
  useSIMSessions,
  useSIMStateAction,
} from '@/hooks/use-sims'
import { useCDRStats } from '@/hooks/use-cdrs'
import { useUndo } from '@/hooks/use-undo'
import { formatBytes, formatDuration, timeAgo } from '@/lib/format'
import { stateVariant } from '@/lib/sim-utils'
import type { SIM } from '@/types/sim'

interface QuickViewPanelBodyProps {
  sim: SIM
  onClose: () => void
}

export function QuickViewPanelBody({ sim, onClose }: QuickViewPanelBodyProps): React.ReactElement {
  const navigate = useNavigate()

  const { data: usage, isLoading: usageLoading, isError: usageError } = useSIMUsage(sim.id, '7d')

  const { from, to } = useMemo(() => {
    const now = new Date()
    const sevenDaysAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
    return {
      from: sevenDaysAgo.toISOString(),
      to: now.toISOString(),
    }
  }, [])

  const { data: cdrStats, isLoading: statsLoading, isError: statsError } = useCDRStats({ sim_id: sim.id, from, to })
  const { data: sessionsPages, isLoading: sessionsLoading, isError: sessionsError } = useSIMSessions(sim.id)
  const stateMut = useSIMStateAction()
  const { register: registerUndo } = useUndo([['sims']])

  useEffect(() => {
    if (usageError || statsError || sessionsError) {
      toast.error('Failed to load SIM quick view data', {
        id: `sim-quickview-error-${sim.id}`,
      })
    }
  }, [usageError, statsError, sessionsError, sim.id])

  const bytesInSeries: number[] = useMemo(
    () => usage?.series?.map((b) => b.bytes_in) ?? [],
    [usage],
  )
  const bytesOutSeries: number[] = useMemo(
    () => usage?.series?.map((b) => b.bytes_out) ?? [],
    [usage],
  )

  const sevenDaysAgoMs = useMemo(() => Date.now() - 7 * 24 * 60 * 60 * 1000, [])

  const avgDurationSec: number = useMemo(() => {
    const firstPage = sessionsPages?.pages[0]?.data ?? []
    const recent = firstPage.filter((s) => new Date(s.started_at).getTime() >= sevenDaysAgoMs)
    if (recent.length === 0) return 0
    const total = recent.reduce((sum, s) => sum + s.duration_sec, 0)
    return total / (cdrStats?.unique_sessions || 1)
  }, [sessionsPages, cdrStats, sevenDaysAgoMs])

  const lastSessionRelative: string = useMemo(() => {
    const firstSession = sessionsPages?.pages[0]?.data[0]
    if (!firstSession?.started_at) return '—'
    return timeAgo(firstSession.started_at)
  }, [sessionsPages])

  async function handleSuspend() {
    try {
      const result = await stateMut.mutateAsync({ simId: sim.id, action: 'suspend' })
      if (result.undoActionId) {
        registerUndo(result.undoActionId, 'SIM suspend')
      }
      onClose()
    } catch {
      // Fail-soft: mutation hook handles global error toast; panel stays open
    }
  }

  return (
    <div className="space-y-4">
      {/* Card 1 — Identity */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Identity</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2.5">
          <InfoRow label="ICCID" value={sim.iccid} mono />
          <InfoRow label="IMSI" value={sim.imsi} mono />
          <InfoRow label="MSISDN" value={sim.msisdn ?? '—'} mono={!!sim.msisdn} />
          <InfoRow
            label="State"
            value={
              <Badge variant={stateVariant(sim.state)} className="text-[10px]">
                {sim.state.toUpperCase()}
              </Badge>
            }
          />
          <InfoRow
            label="Policy"
            value={
              sim.policy_name ? (
                <span className="font-mono text-xs text-text-primary">{sim.policy_name}</span>
              ) : (
                'None'
              )
            }
          />
          <InfoRow label="Last Session" value={lastSessionRelative} />
        </CardContent>
      </Card>

      {/* Card 2 — Usage (last 7 days) */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Usage (last 7 days)</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {usageLoading ? (
            <div className="flex justify-center py-4">
              <Spinner className="h-5 w-5 text-text-tertiary" />
            </div>
          ) : usageError ? (
            <div className="flex items-center gap-2 py-2 text-xs text-danger">
              <AlertCircle className="h-3.5 w-3.5" />
              <span>Failed to load usage</span>
            </div>
          ) : bytesInSeries.length < 2 && bytesOutSeries.length < 2 ? (
            <p className="text-xs text-text-tertiary">No usage data in last 7 days</p>
          ) : (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-secondary">Data In</span>
                  {bytesInSeries.length >= 2 && (
                    <Sparkline
                      data={bytesInSeries}
                      color="var(--color-accent)"
                      width={240}
                      height={32}
                    />
                  )}
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-secondary">Data Out</span>
                  {bytesOutSeries.length >= 2 && (
                    <Sparkline
                      data={bytesOutSeries}
                      color="var(--color-purple)"
                      width={240}
                      height={32}
                    />
                  )}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3 pt-1">
                <div>
                  <div className="font-mono text-sm font-semibold text-accent">
                    {formatBytes(usage?.total_bytes_in ?? 0)}
                  </div>
                  <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-0.5">
                    Total In
                  </div>
                </div>
                <div>
                  <div
                    className="font-mono text-sm font-semibold"
                    style={{ color: 'var(--color-purple)' }}
                  >
                    {formatBytes(usage?.total_bytes_out ?? 0)}
                  </div>
                  <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-0.5">
                    Total Out
                  </div>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Card 3 — CDR Summary (7d) */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">CDR Summary (7d)</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2.5">
          {statsLoading ? (
            <div className="flex justify-center py-4">
              <Spinner className="h-5 w-5 text-text-tertiary" />
            </div>
          ) : statsError || sessionsError ? (
            <div className="flex items-center gap-2 py-2 text-xs text-danger">
              <AlertCircle className="h-3.5 w-3.5" />
              <span>Failed to load CDR summary</span>
            </div>
          ) : (
            <>
              <InfoRow
                label="Sessions"
                value={
                  <span className="font-mono text-sm font-semibold text-text-primary">
                    {cdrStats?.unique_sessions ?? 0}
                  </span>
                }
              />
              <InfoRow
                label="Total Bytes"
                value={
                  <span className="font-mono text-sm font-semibold text-text-primary">
                    {formatBytes((cdrStats?.total_bytes_in ?? 0) + (cdrStats?.total_bytes_out ?? 0))}
                  </span>
                }
              />
              <InfoRow
                label="Avg Duration"
                value={
                  <span className="font-mono text-sm font-semibold text-text-primary">
                    {avgDurationSec > 0 ? formatDuration(avgDurationSec) : '—'}
                  </span>
                }
              />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-3">
                Top Destinations{' '}
                <span className="text-[9px] italic normal-case">(coming soon)</span>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Quick Actions Footer */}
      <div className="flex items-center gap-2 pt-1">
        <Button
          variant="default"
          size="sm"
          className="gap-1.5"
          onClick={() => navigate('/sims/' + sim.id)}
        >
          <ExternalLink className="h-3.5 w-3.5" />
          View Full Details
        </Button>
        <Button
          variant="destructive"
          size="sm"
          disabled={sim.state !== 'active' || stateMut.isPending}
          onClick={handleSuspend}
        >
          Suspend
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => navigate('/cdrs?sim_id=' + sim.id)}
        >
          View CDRs
        </Button>
      </div>
    </div>
  )
}
