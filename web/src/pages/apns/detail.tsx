import { useState, useMemo, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTabUrlSync } from '@/hooks/use-tab-url-sync'
import {
  ArrowLeft,
  RefreshCw,
  AlertCircle,
  Wifi,
  Server,
  BarChart3,
  Settings,
  Pencil,
  Trash2,
  Loader2,
  Lock,
  Activity,
  TrendingUp,
  Plus,
  Shield,
  Layers,
  FileText,
  ExternalLink,
} from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { TimeframeSelector } from '@/components/ui/timeframe-selector'
import { Spinner } from '@/components/ui/spinner'
import { useAPN, useAPNIPPools, useAPNSims, useUpdateAPN, useDeleteAPN, useCreateIPPool, useAPNReferencingPolicies } from '@/hooks/use-apns'
import { useSIMList } from '@/hooks/use-sims'
import { useAPNTraffic } from '@/hooks/use-apn-traffic'
import { useOperatorList } from '@/hooks/use-operators'
import { useIpPoolAddresses } from '@/hooks/use-settings'
import { Skeleton } from '@/components/ui/skeleton'
import type { SIM, SIMState } from '@/types/sim'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { RAT_DISPLAY } from '@/lib/constants'
import { formatBytes } from '@/lib/format'
import { stateVariant } from '@/lib/sim-utils'
import { InfoRow } from '@/components/ui/info-row'
import { RelatedAuditTab, RelatedNotificationsPanel, RelatedAlertsPanel, FavoriteToggle } from '@/components/shared'
import { InfoTooltip } from '@/components/ui/info-tooltip'
import { KPICard } from '@/components/shared/kpi-card'

const APN_TYPE_DISPLAY: Record<string, string> = {
  private_managed: 'Private Managed',
  operator_managed: 'Operator Managed',
  customer_managed: 'Customer Managed',
}

function ConfigTab({ apn, operatorName }: { apn: NonNullable<ReturnType<typeof useAPN>['data']>; operatorName: string }) {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card>
        <CardHeader>
          <CardTitle>General Configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="APN Name" value={apn.name} mono />
          {apn.display_name && <InfoRow label="Display Name" value={apn.display_name} />}
          <InfoRow label="Type" value={APN_TYPE_DISPLAY[apn.apn_type] ?? apn.apn_type} />
          <InfoRow label="State" value={apn.state.toUpperCase()} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Network Configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Operator" value={operatorName} />
          <InfoRow label="Operator ID" value={apn.operator_id} mono />
          <InfoRow label="Default Policy" value={apn.default_policy_id ?? 'None'} mono={!!apn.default_policy_id} />
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-secondary">RAT Types</span>
            <div className="flex gap-1">
              {apn.supported_rat_types.map((rat) => (
                <span
                  key={rat}
                  className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium"
                >
                  {RAT_DISPLAY[rat] ?? rat}
                </span>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Timeline</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <InfoRow label="Created" value={new Date(apn.created_at).toLocaleString()} />
          <InfoRow label="Last Updated" value={new Date(apn.updated_at).toLocaleString()} />
        </CardContent>
      </Card>
    </div>
  )
}

function IPPoolsTab({ apnId }: { apnId: string }) {
  const navigate = useNavigate()
  const { data: pools, isLoading } = useAPNIPPools(apnId)
  const createPool = useCreateIPPool()
  const [createOpen, setCreateOpen] = useState(false)
  const [poolName, setPoolName] = useState('')
  const [cidrV4, setCidrV4] = useState('')
  const [cidrV6, setCidrV6] = useState('')
  const [selectedPoolId, setSelectedPoolId] = useState<string | null>(null)
  const selectedPoolData = pools?.find((p) => p.id === selectedPoolId)
  const { data: addrPages } = useIpPoolAddresses(selectedPoolId ?? '')
  const reservedAddresses = useMemo(() => {
    if (!addrPages?.pages) return []
    return addrPages.pages.flatMap((p) => p.data).filter((a) => a.state === 'reserved' || a.state === 'assigned')
  }, [addrPages])

  const handleCreate = async () => {
    if (!poolName || (!cidrV4 && !cidrV6)) return
    try {
      await createPool.mutateAsync({
        apn_id: apnId,
        name: poolName,
        cidr_v4: cidrV4 || undefined,
        cidr_v6: cidrV6 || undefined,
      })
      setCreateOpen(false)
      setPoolName('')
      setCidrV4('')
      setCidrV6('')
    } catch {
      // handled by api interceptor
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full mb-2" />
          ))}
        </CardContent>
      </Card>
    )
  }

  const totalAddresses = pools?.reduce((a, p) => a + p.total_addresses, 0) ?? 0
  const usedAddresses = pools?.reduce((a, p) => a + p.used_addresses, 0) ?? 0
  const availableAddresses = pools?.reduce((a, p) => a + p.available_addresses, 0) ?? 0
  const overallPct = totalAddresses > 0 ? (usedAddresses / totalAddresses) * 100 : 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <InfoTooltip term="static_ip">
          <span className="text-xs text-text-secondary font-medium">IP Pools</span>
        </InfoTooltip>
        <Button size="sm" className="gap-1.5" onClick={() => setCreateOpen(true)}>
          <Plus className="h-3.5 w-3.5" />
          Create IP Pool
        </Button>
      </div>

      {pools && pools.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-4">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Total IPs</div>
              <div className="font-mono text-xl font-bold text-accent">{totalAddresses.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-4">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Used</div>
              <div className="font-mono text-xl font-bold text-warning">{usedAddresses.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-4">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Available</div>
              <div className="font-mono text-xl font-bold text-success">{availableAddresses.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-4">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Utilization</div>
              <div className="font-mono text-xl font-bold text-text-primary">{overallPct.toFixed(1)}%</div>
              <div className="w-full h-2 bg-bg-hover rounded-full overflow-hidden mt-2">
                <div
                  className={cn(
                    'h-full rounded-full transition-all',
                    overallPct >= 90 ? 'bg-danger' : overallPct >= 75 ? 'bg-warning' : 'bg-success',
                  )}
                  style={{ width: `${Math.min(overallPct, 100)}%` }}
                />
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {(!pools || pools.length === 0) ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Server className="h-8 w-8 text-text-tertiary mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No IP Pools</h3>
            <p className="text-xs text-text-secondary mb-3">No IP pools configured for this APN.</p>
            <Button size="sm" className="gap-1.5" onClick={() => setCreateOpen(true)}>
              <Plus className="h-3.5 w-3.5" />
              Create First Pool
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Card className="overflow-hidden density-compact">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Pool Name</TableHead>
                <TableHead>CIDR</TableHead>
                <TableHead>Total</TableHead>
                <TableHead>Used</TableHead>
                <TableHead>Available</TableHead>
                <TableHead>Utilization</TableHead>
                <TableHead>State</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {pools.map((pool) => (
                <TableRow key={pool.id} className="cursor-pointer" onClick={() => setSelectedPoolId(pool.id)}>
                  <TableCell>
                    <span className="text-sm text-text-primary font-medium">{pool.name}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">
                      {pool.cidr_v4 || pool.cidr_v6 || '-'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs">{pool.total_addresses}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs">{pool.used_addresses}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs">{pool.available_addresses}</span>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <div className="w-16 h-1.5 bg-bg-hover rounded-full overflow-hidden">
                        <div
                          className={cn(
                            'h-full rounded-full',
                            pool.utilization_pct >= 90 ? 'bg-danger' : pool.utilization_pct >= 75 ? 'bg-warning' : 'bg-success',
                          )}
                          style={{ width: `${Math.min(pool.utilization_pct, 100)}%` }}
                        />
                      </div>
                      <span className="font-mono text-[10px] text-text-tertiary w-8">
                        {pool.utilization_pct.toFixed(0)}%
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={pool.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">
                      {pool.state.toUpperCase()}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}

      <SlidePanel
        open={!!selectedPoolId && !!selectedPoolData}
        onOpenChange={(open) => { if (!open) setSelectedPoolId(null) }}
        title={selectedPoolData?.name ?? 'Pool Detail'}
        description={`CIDR: ${selectedPoolData?.cidr_v4 || selectedPoolData?.cidr_v6 || '-'}`}
        width="lg"
      >
        {selectedPoolData && (
          <div className="space-y-4">
            <div className="grid grid-cols-4 gap-3">
              <div className="rounded-[var(--radius-sm)] border border-border p-3 text-center">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Total</div>
                <div className="font-mono text-lg font-bold text-accent">{selectedPoolData.total_addresses}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3 text-center">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Used</div>
                <div className="font-mono text-lg font-bold text-warning">{selectedPoolData.used_addresses}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3 text-center">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Available</div>
                <div className="font-mono text-lg font-bold text-success">{selectedPoolData.available_addresses}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3 text-center">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Utilization</div>
                <div className="font-mono text-lg font-bold text-text-primary">{selectedPoolData.utilization_pct.toFixed(1)}%</div>
              </div>
            </div>
            {reservedAddresses.length > 0 && (
              <div>
                <div className="text-xs text-text-secondary mb-2">Assigned / Reserved ({reservedAddresses.length})</div>
                <div className="rounded-[var(--radius-md)] border border-border overflow-hidden">
                  <Table className="text-xs">
                    <TableHeader className="bg-bg-elevated">
                      <TableRow>
                        <TableHead className="text-left px-3 py-2 text-[10px] uppercase tracking-wider text-text-tertiary font-medium">IP</TableHead>
                        <TableHead className="text-left px-3 py-2 text-[10px] uppercase tracking-wider text-text-tertiary font-medium">State</TableHead>
                        <TableHead className="text-left px-3 py-2 text-[10px] uppercase tracking-wider text-text-tertiary font-medium">SIM</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {reservedAddresses.map((addr) => (
                        <TableRow key={addr.id} className="border-t border-border-subtle">
                          <TableCell className="px-3 py-1.5 font-mono text-text-primary">{addr.address_v4 || addr.address_v6}</TableCell>
                          <TableCell className="px-3 py-1.5">
                            <Badge variant={addr.state === 'assigned' ? 'warning' : 'secondary'} className="text-[9px]">
                              {addr.state.toUpperCase()}
                            </Badge>
                          </TableCell>
                          <TableCell className="px-3 py-1.5">
                            {addr.sim_iccid ? (
                              <span className="font-mono text-accent">{addr.sim_iccid}</span>
                            ) : addr.sim_id ? (
                              <span className="font-mono text-text-tertiary">{addr.sim_id.slice(0, 12) /* UUID slice ok: sim_iccid absent path, secondary info in IP allocation row */}</span>
                            ) : (
                              <span className="text-text-tertiary">-</span>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </div>
            )}
            {reservedAddresses.length === 0 && selectedPoolData.used_addresses === 0 && (
              <div className="text-xs text-text-tertiary text-center py-4">No reservations in this pool</div>
            )}
            <div className="flex items-center justify-end">
              <Button size="sm" variant="outline" className="gap-2" onClick={() => { setSelectedPoolId(null); navigate(`/settings/ip-pools/${selectedPoolId}`) }}>
                Open Full Detail
              </Button>
            </div>
          </div>
        )}
      </SlidePanel>

      <SlidePanel open={createOpen} onOpenChange={setCreateOpen} title="Create IP Pool" description="Add a new IP pool to this APN" width="md">
        <div className="space-y-4">
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">Pool Name *</label>
            <Input value={poolName} onChange={(e) => setPoolName(e.target.value)} placeholder="e.g. Fleet IPv4 Pool" />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">IPv4 CIDR</label>
            <Input value={cidrV4} onChange={(e) => setCidrV4(e.target.value)} placeholder="e.g. 10.20.0.0/24" className="font-mono" />
            <p className="text-[10px] text-text-tertiary mt-1">/24 = 254 IPs, /22 = 1022 IPs, /16 = 65534 IPs</p>
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1.5">IPv6 CIDR (optional)</label>
            <Input value={cidrV6} onChange={(e) => setCidrV6(e.target.value)} placeholder="e.g. fd00:iot::/64" className="font-mono" />
          </div>
        </div>
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
          <Button variant="outline" onClick={() => setCreateOpen(false)}>Cancel</Button>
          <Button
            onClick={handleCreate}
            disabled={!poolName || (!cidrV4 && !cidrV6) || createPool.isPending}
            className="gap-2"
          >
            {createPool.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
            Create Pool
          </Button>
        </div>
      </SlidePanel>
    </div>
  )
}

function SIMsTab({ apnId }: { apnId: string }) {
  const navigate = useNavigate()
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useAPNSims(apnId)
  const [selectedSim, setSelectedSim] = useState<SIM | null>(null)

  const allSims = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((p) => p.data)
  }, [data])

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full mb-2" />
          ))}
        </CardContent>
      </Card>
    )
  }

  if (allSims.length === 0) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-center">
          <Wifi className="h-8 w-8 text-text-tertiary mb-3" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">No connected SIMs</h3>
          <p className="text-xs text-text-secondary">No SIMs are currently using this APN.</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
    <Card className="overflow-hidden density-compact">
      <Table>
        <TableHeader className="bg-bg-elevated">
          <TableRow>
            <TableHead><InfoTooltip term="ICCID">ICCID</InfoTooltip></TableHead>
            <TableHead><InfoTooltip term="IMSI">IMSI</InfoTooltip></TableHead>
            <TableHead><InfoTooltip term="MSISDN">MSISDN</InfoTooltip></TableHead>
            <TableHead>IP Address</TableHead>
            <TableHead>State</TableHead>
            <TableHead>RAT</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {allSims.map((sim) => (
            <TableRow
              key={sim.id}
              className="cursor-pointer"
              onClick={() => setSelectedSim(sim)}
            >
              <TableCell>
                <span
                  className="font-mono text-xs text-accent hover:underline"
                  onClick={(e) => { e.stopPropagation(); navigate(`/sims/${sim.id}`) }}
                >
                  {sim.iccid}
                </span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{sim.imsi}</span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{sim.msisdn ?? '-'}</span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{sim.ip_address || '-'}</span>
              </TableCell>
              <TableCell>
                <Badge variant={stateVariant(sim.state)} className="text-[10px]">
                  {sim.state === 'active' && <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse mr-1" />}
                  {sim.state.toUpperCase()}
                </Badge>
              </TableCell>
              <TableCell>
                {sim.rat_type ? (
                  <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium">
                    {RAT_DISPLAY[sim.rat_type] ?? sim.rat_type}
                  </span>
                ) : (
                  <span className="text-text-tertiary text-xs">-</span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {hasNextPage && (
        <div className="px-4 py-3 border-t border-border-subtle">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1 flex items-center justify-center gap-2 h-auto"
          >
            {isFetchingNextPage && <Spinner className="h-3 w-3" />}
            {isFetchingNextPage ? 'Loading...' : 'Load more SIMs'}
          </Button>
        </div>
      )}
    </Card>

    <SlidePanel
      open={!!selectedSim}
      onOpenChange={(open) => { if (!open) setSelectedSim(null) }}
      title={selectedSim ? `SIM ${selectedSim.iccid?.slice(-8)}` : 'SIM Detail'}
      description={selectedSim?.iccid}
      width="lg"
    >
      {selectedSim && (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">ICCID</div>
              <div className="font-mono text-xs text-text-primary">{selectedSim.iccid}</div>
            </div>
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">IMSI</div>
              <div className="font-mono text-xs text-text-primary">{selectedSim.imsi}</div>
            </div>
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">MSISDN</div>
              <div className="font-mono text-xs text-text-primary">{selectedSim.msisdn ?? '-'}</div>
            </div>
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">IP Address</div>
              <div className="font-mono text-xs text-text-primary">{selectedSim.ip_address || '-'}</div>
            </div>
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">State</div>
              <Badge variant={stateVariant(selectedSim.state)} className="text-[10px]">
                {selectedSim.state.toUpperCase()}
              </Badge>
            </div>
            <div className="rounded-[var(--radius-sm)] border border-border p-3">
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">RAT Type</div>
              <div className="text-xs text-text-primary">{selectedSim.rat_type ? (RAT_DISPLAY[selectedSim.rat_type] ?? selectedSim.rat_type) : '-'}</div>
            </div>
          </div>
          {selectedSim.created_at && (
            <div className="text-xs text-text-tertiary">
              Created: {new Date(selectedSim.created_at).toLocaleString()}
            </div>
          )}
          <div className="flex items-center justify-end pt-2">
            <Button size="sm" className="gap-2" onClick={() => { setSelectedSim(null); navigate(`/sims/${selectedSim.id}`) }}>
              Open Full Detail
            </Button>
          </div>
        </div>
      )}
    </SlidePanel>
    </>
  )
}

function TrafficTab({ apnId }: { apnId: string }) {
  const [period, setPeriod] = useState('24h')
  const { data: trafficData, isLoading, isError } = useAPNTraffic(apnId, period)

  const series = useMemo(() => {
    if (!trafficData?.series) return []
    return trafficData.series.map((b) => ({
      label: new Date(b.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' }),
      bytes_in: b.bytes_in,
      bytes_out: b.bytes_out,
      total: b.bytes_in + b.bytes_out,
      auth_count: b.auth_count,
    }))
  }, [trafficData])

  const totals = useMemo(() => {
    const tIn = series.reduce((a, b) => a + b.bytes_in, 0)
    const tOut = series.reduce((a, b) => a + b.bytes_out, 0)
    const tRec = series.reduce((a, b) => a + b.auth_count, 0)
    return { tIn, tOut, tTotal: tIn + tOut, tRec }
  }, [series])

  const tooltipStyle = {
    backgroundColor: 'var(--color-bg-elevated)',
    border: '1px solid var(--color-border)',
    borderRadius: 'var(--radius-sm)',
    color: 'var(--color-text-primary)',
    fontSize: '12px',
  }

  if (isError) {
    return (
      <div className="rounded-lg border border-danger/30 bg-danger-dim p-6 text-center">
        <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
        <p className="text-sm text-danger">Failed to load traffic data.</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Header with period selector */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-text-primary">Traffic Overview</h3>
          <p className="text-[11px] text-text-tertiary mt-0.5">Aggregated from CDR hourly rollups</p>
        </div>
        <TimeframeSelector
          options={[
            { value: '1h', label: '1h' },
            { value: '6h', label: '6h' },
            { value: '24h', label: '24h' },
            { value: '7d', label: '7d' },
            { value: '30d', label: '30d' },
          ]}
          value={period}
          onChange={(v) => setPeriod(typeof v === 'string' ? v : v.value)}
          allowCustom={false}
        />
      </div>

      {/* KPI strip */}
      <div className="grid grid-cols-4 gap-3">
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <div className="h-2 w-2 rounded-full bg-accent" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Bytes In</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tIn)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <div className="h-2 w-2 rounded-full bg-success" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Bytes Out</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tOut)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <TrendingUp className="h-3 w-3 text-text-tertiary" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">Total Volume</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{formatBytes(totals.tTotal)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 mb-1">
              <Activity className="h-3 w-3 text-text-tertiary" />
              <div className="text-[10px] uppercase tracking-wider text-text-tertiary font-medium">CDR Records</div>
            </div>
            <div className="font-mono text-xl font-bold text-text-primary">{totals.tRec.toLocaleString()}</div>
          </CardContent>
        </Card>
      </div>

      {/* Main traffic chart */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Bytes Over Time</CardTitle>
            <div className="flex items-center gap-3 text-[10px]">
              <div className="flex items-center gap-1.5">
                <div className="h-2 w-2 rounded-full bg-accent" /><span className="text-text-tertiary">In</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="h-2 w-2 rounded-full bg-success" /><span className="text-text-tertiary">Out</span>
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <Skeleton className="h-[280px] w-full" />
          ) : series.length === 0 ? (
            <div className="h-[280px] flex flex-col items-center justify-center gap-2">
              <TrendingUp className="h-8 w-8 text-text-tertiary opacity-40" />
              <p className="text-[12px] text-text-secondary">No traffic in this period</p>
              <p className="text-[10px] text-text-tertiary">CDR rollups refresh every 30 minutes</p>
            </div>
          ) : (
            <div className="h-[280px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={series}>
                  <defs>
                    <linearGradient id="apnBytesInHero" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0.02} />
                    </linearGradient>
                    <linearGradient id="apnBytesOutHero" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-success)" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="var(--color-success)" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="label" tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} tickLine={false} axisLine={false} interval={Math.max(0, Math.floor(series.length / 8) - 1)} />
                  <YAxis tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} tickLine={false} axisLine={false} tickFormatter={(v) => formatBytes(v)} width={70} />
                  <Tooltip contentStyle={tooltipStyle} formatter={(value, name) => [formatBytes(Number(value)), name]} />
                  <Area type="monotone" dataKey="bytes_in" stackId="1" stroke="var(--color-accent)" fill="url(#apnBytesInHero)" strokeWidth={2} name="In" />
                  <Area type="monotone" dataKey="bytes_out" stackId="1" stroke="var(--color-success)" fill="url(#apnBytesOutHero)" strokeWidth={2} name="Out" />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Record count chart */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>CDR Records Per Bucket</CardTitle>
            <span className="text-[10px] text-text-tertiary">Auth + Accounting events</span>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <Skeleton className="h-[180px] w-full" />
          ) : series.length === 0 ? (
            <div className="h-[180px] flex items-center justify-center text-[12px] text-text-tertiary">
              No records yet
            </div>
          ) : (
            <div className="h-[180px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={series}>
                  <defs>
                    <linearGradient id="apnRecordCount" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.25} />
                      <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="label" tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} tickLine={false} axisLine={false} interval={Math.max(0, Math.floor(series.length / 8) - 1)} />
                  <YAxis tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} tickLine={false} axisLine={false} width={40} />
                  <Tooltip contentStyle={tooltipStyle} formatter={(value) => [value, 'Records']} />
                  <Area type="monotone" dataKey="auth_count" stroke="var(--color-accent)" fill="url(#apnRecordCount)" strokeWidth={2} name="Records" />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

const APN_TYPE_OPTIONS = [
  { value: 'private_managed', label: 'Private Managed' },
  { value: 'operator_managed', label: 'Operator Managed' },
  { value: 'customer_managed', label: 'Customer Managed' },
]

const RAT_TYPE_OPTIONS_LIST = ['nb_iot', 'lte_m', 'lte', 'nr_5g']

function PoliciesReferencingTab({ apnId }: { apnId: string }) {
  const navigate = useNavigate()
  const { data: policies = [], isLoading } = useAPNReferencingPolicies(apnId)

  if (isLoading) {
    return (
      <div className="space-y-2 mt-4">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-10 rounded bg-bg-elevated animate-pulse" />
        ))}
      </div>
    )
  }

  if (policies.length === 0) {
    return (
      <div className="mt-4 flex flex-col items-center justify-center py-12 gap-3">
        <FileText className="h-8 w-8 text-text-tertiary" />
        <p className="text-sm text-text-secondary">No policies reference this APN</p>
      </div>
    )
  }

  return (
    <div className="mt-4">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Scope</TableHead>
            <TableHead>State</TableHead>
            <TableHead className="w-8" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {policies.map((p) => (
            <TableRow key={p.id} className="cursor-pointer" onClick={() => navigate(`/policies/${p.id}`)}>
              <TableCell className="text-sm font-medium text-text-primary">{p.name}</TableCell>
              <TableCell>
                <Badge variant="secondary" className="text-[10px]">{p.scope}</Badge>
              </TableCell>
              <TableCell>
                <Badge variant={p.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">{p.state}</Badge>
              </TableCell>
              <TableCell>
                <ExternalLink className="h-3.5 w-3.5 text-text-tertiary" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function EditAPNDialog({
  open,
  onClose,
  apn,
  operators,
  onSuccess,
}: {
  open: boolean
  onClose: () => void
  apn: NonNullable<ReturnType<typeof useAPN>['data']>
  operators: { id: string; name: string }[]
  onSuccess: () => void
}) {
  const [form, setForm] = useState({
    name: apn.name,
    display_name: apn.display_name ?? '',
    operator_id: apn.operator_id,
    apn_type: apn.apn_type,
    supported_rat_types: [...apn.supported_rat_types],
  })
  const [error, setError] = useState<string | null>(null)
  const updateMutation = useUpdateAPN(apn.id)

  const toggleRat = (rat: string) => {
    setForm((f) => ({
      ...f,
      supported_rat_types: f.supported_rat_types.includes(rat)
        ? f.supported_rat_types.filter((r) => r !== rat)
        : [...f.supported_rat_types, rat],
    }))
  }

  const handleSubmit = async () => {
    setError(null)
    if (!form.name.trim()) { setError('APN name is required'); return }
    try {
      await updateMutation.mutateAsync({
        name: form.name.trim(),
        display_name: form.display_name.trim() || undefined,
        operator_id: form.operator_id,
        apn_type: form.apn_type,
        supported_rat_types: form.supported_rat_types,
      })
      onSuccess()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      setError(msg ?? 'Failed to update APN')
    }
  }

  return (
    <SlidePanel open={open} onOpenChange={(v) => { if (!v) onClose() }} title="Edit APN" description="Update APN configuration" width="md">
      <div className="space-y-4">
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">APN Name *</label>
          <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="h-8 text-sm" />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Display Name</label>
          <Input value={form.display_name} onChange={(e) => setForm((f) => ({ ...f, display_name: e.target.value }))} className="h-8 text-sm" />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Operator</label>
          <Select value={form.operator_id} onChange={(e) => setForm((f) => ({ ...f, operator_id: e.target.value }))} className="h-8 text-sm" options={operators.map((op) => ({ value: op.id, label: op.name }))} />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">APN Type</label>
          <Select value={form.apn_type} onChange={(e) => setForm((f) => ({ ...f, apn_type: e.target.value }))} className="h-8 text-sm" options={APN_TYPE_OPTIONS} />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">RAT Types</label>
          <div className="flex flex-wrap gap-2">
            {RAT_TYPE_OPTIONS_LIST.map((rat) => (
              <Button key={rat} type="button" variant="ghost" size="sm" onClick={() => toggleRat(rat)} className={cn(
                'px-2.5 py-1 rounded text-xs font-mono border transition-colors h-auto',
                form.supported_rat_types.includes(rat)
                  ? 'border-accent bg-accent-dim text-accent'
                  : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
              )}>
                {RAT_DISPLAY[rat] ?? rat}
              </Button>
            ))}
          </div>
        </div>
        {error && <p className="text-xs text-danger">{error}</p>}
      </div>
      <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
        <Button variant="outline" size="sm" onClick={onClose} disabled={updateMutation.isPending}>Cancel</Button>
        <Button size="sm" onClick={handleSubmit} disabled={updateMutation.isPending} className="gap-1.5">
          {updateMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Save Changes
        </Button>
      </div>
    </SlidePanel>
  )
}

const APN_TABS = [
  'overview', 'config', 'ip-pools', 'sims', 'traffic', 'policies', 'audit', 'alerts',
] as const

export default function ApnDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useTabUrlSync({
    defaultTab: 'overview',
    aliases: { notifications: 'alerts' },
    validTabs: [...APN_TABS],
  })

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const { data: apn, isLoading, isError, refetch } = useAPN(id ?? '')
  const { data: operators } = useOperatorList()
  const deleteMutation = useDeleteAPN(id ?? '')
  const addRecentItem = useUIStore((s) => s.addRecentItem)

  useEffect(() => {
    if (apn && id) {
      addRecentItem({ type: 'apn', id, label: `APN: ${apn.name}`, path: `/apns/${id}` })
    }
  }, [apn, id, addRecentItem])

  const operatorName = useMemo(() => {
    if (!apn || !operators) return 'Unknown'
    return operators.find((o) => o.id === apn.operator_id)?.name ?? 'Unknown'
  }, [apn, operators])

  // KPI data
  const { data: simListData } = useSIMList({ apn_id: id ?? '' })
  const { data: trafficData } = useAPNTraffic(id ?? '', '24h')

  const simTotal = simListData?.pages.flatMap((p) => p.data).length ?? 0

  const traffic24h = useMemo<number>(() => {
    if (!trafficData?.series || trafficData.series.length === 0) return 0
    return trafficData.series.reduce((acc, b) => acc + b.bytes_in + b.bytes_out, 0)
  }, [trafficData])

  const topOperator = useMemo<string>(() => {
    const firstPage = simListData?.pages[0]?.data ?? []
    if (firstPage.length === 0) return '—'
    const counts: Record<string, number> = {}
    for (const sim of firstPage) {
      const opName = (sim as { operator_name?: string }).operator_name ?? sim.operator_id ?? 'Unknown'
      counts[opName] = (counts[opName] ?? 0) + 1
    }
    const topEntry = Object.entries(counts).sort(([, a], [, b]) => b - a)[0]
    return topEntry ? topEntry[0] : '—'
  }, [simListData])

  const hasMoreSims = simListData?.pages[0]?.meta?.has_more ?? false

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-48" />
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4">
                <Skeleton className="h-32 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  if (isError || !apn) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">APN not found</h2>
          <p className="text-sm text-text-secondary mb-4">The requested APN could not be loaded.</p>
          <div className="flex gap-2 justify-center">
            <Button onClick={() => navigate('/apns')} variant="outline" className="gap-2">
              <ArrowLeft className="h-4 w-4" />
              Back to APNs
            </Button>
            <Button onClick={() => refetch()} variant="outline" className="gap-2">
              <RefreshCw className="h-4 w-4" />
              Retry
            </Button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <Breadcrumb
        items={[
          { label: 'Dashboard', href: '/' },
          { label: 'APNs', href: '/apns' },
          { label: apn.name },
        ]}
        className="mb-1"
      />
      <div className="flex items-center gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-[16px] font-semibold text-text-primary truncate">
              {apn.display_name || apn.name}
            </h1>
            <FavoriteToggle
              type="apn"
              id={id ?? ''}
              label={`APN: ${apn.name}`}
              path={`/apns/${id}`}
            />
            <Badge variant={apn.state === 'active' ? 'success' : 'secondary'} className="gap-1 flex-shrink-0">
              {apn.state === 'active' && <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />}
              {apn.state.toUpperCase()}
            </Badge>
            <Badge variant="outline" className="text-[10px] flex-shrink-0">
              {APN_TYPE_DISPLAY[apn.apn_type] ?? apn.apn_type}
            </Badge>
          </div>
          <div className="flex items-center gap-4 mt-1">
            <span className="text-xs text-text-secondary">{operatorName}</span>
            {apn.supported_rat_types.map((rat) => (
              <span
                key={rat}
                className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary"
              >
                {RAT_DISPLAY[rat] ?? rat}
              </span>
            ))}
          </div>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setEditOpen(true)}>
            <Pencil className="h-3.5 w-3.5" />
            Edit
          </Button>
          <Button variant="outline" size="sm" className="gap-1.5 border-danger/30 text-danger hover:bg-danger-dim" onClick={() => setDeleteOpen(true)}>
            <Trash2 className="h-3.5 w-3.5" />
            Delete
          </Button>
        </div>
      </div>

      {/* KPI Row — APN health summary */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <KPICard
          title="SIMs"
          value={simTotal}
          sparklineData={[]}
          color="var(--color-accent)"
          subtitle="Connected to this APN"
          delay={0}
        />
        <KPICard
          title="Traffic (24h)"
          value={traffic24h}
          formatter={(n) => formatBytes(n)}
          sparklineData={[]}
          color="var(--color-success)"
          subtitle="Inbound + outbound"
          delay={80}
        />
        <KPICard
          title="Top Operator"
          value={0}
          label={topOperator || '—'}
          sparklineData={[]}
          color="var(--color-warning)"
          subtitle={hasMoreSims ? 'Based on first 50 SIMs' : 'By SIM count'}
          delay={160}
        />
        <KPICard
          title="APN State"
          value={0}
          label={apn.state.toUpperCase()}
          sparklineData={[]}
          color={apn.state === 'active' ? 'var(--color-success)' : 'var(--color-text-tertiary)'}
          subtitle={APN_TYPE_DISPLAY[apn.apn_type] ?? apn.apn_type}
          delay={240}
        />
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <Settings className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="config" className="gap-1.5">
            <Settings className="h-3.5 w-3.5" />
            Configuration
          </TabsTrigger>
          <TabsTrigger value="ip-pools" className="gap-1.5">
            <Server className="h-3.5 w-3.5" />
            IP Pools
          </TabsTrigger>
          <TabsTrigger value="sims" className="gap-1.5">
            <Wifi className="h-3.5 w-3.5" />
            SIMs
          </TabsTrigger>
          <TabsTrigger value="traffic" className="gap-1.5">
            <BarChart3 className="h-3.5 w-3.5" />
            Traffic
          </TabsTrigger>
          <TabsTrigger value="policies" className="gap-1.5">
            <FileText className="h-3.5 w-3.5" />
            Policies
          </TabsTrigger>
          <TabsTrigger value="audit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
          {/* Alerts tab — merged with Notifications (FIX-222) */}
          <TabsTrigger value="alerts" className="gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            Alerts
          </TabsTrigger>
        </TabsList>

        {/* Overview tab — config summary (read-heavy first) */}
        <TabsContent value="overview">
          <ConfigTab apn={apn} operatorName={operatorName} />
        </TabsContent>
        <TabsContent value="config">
          <ConfigTab apn={apn} operatorName={operatorName} />
        </TabsContent>
        <TabsContent value="ip-pools">
          <IPPoolsTab apnId={apn.id} />
        </TabsContent>
        <TabsContent value="sims">
          <SIMsTab apnId={apn.id} />
        </TabsContent>
        <TabsContent value="traffic">
          <TrafficTab apnId={apn.id} />
        </TabsContent>
        <TabsContent value="audit">
          <div className="mt-4">
            <RelatedAuditTab entityId={apn.id} entityType="apn" />
          </div>
        </TabsContent>
        {/* Alerts tab — notifications concatenated below alerts */}
        <TabsContent value="alerts">
          <div className="mt-4 space-y-6">
            <RelatedAlertsPanel entityId={apn.id} entityType="apn" />
            <div>
              <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
                System Notifications
              </p>
              <RelatedNotificationsPanel entityId={apn.id} />
            </div>
          </div>
        </TabsContent>
        <TabsContent value="policies">
          <PoliciesReferencingTab apnId={apn.id} />
        </TabsContent>
      </Tabs>

      {apn && operators && (
        <EditAPNDialog
          open={editOpen}
          onClose={() => setEditOpen(false)}
          apn={apn}
          operators={(operators ?? []).map((o) => ({ id: o.id, name: o.name }))}
          onSuccess={() => { setEditOpen(false); refetch() }}
        />
      )}

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent onClose={() => setDeleteOpen(false)}>
          <DialogHeader>
            <DialogTitle>Delete APN?</DialogTitle>
            <DialogDescription>
              This will archive APN "{apn?.display_name || apn?.name}". Connected SIMs may be affected.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button
              variant="destructive"
              disabled={deleteMutation.isPending}
              className="gap-1.5"
              onClick={async () => {
                try {
                  await deleteMutation.mutateAsync()
                  navigate('/apns')
                } catch {}
              }}
            >
              {deleteMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Delete APN
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
