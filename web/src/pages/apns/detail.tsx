import { useState, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  RefreshCw,
  AlertCircle,
  Wifi,
  Server,
  BarChart3,
  Settings,
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
import { Spinner } from '@/components/ui/spinner'
import { useAPN, useAPNIPPools, useAPNSims } from '@/hooks/use-apns'
import { useOperatorList } from '@/hooks/use-operators'
import { Skeleton } from '@/components/ui/skeleton'
import type { SIM, SIMState } from '@/types/sim'
import { cn } from '@/lib/utils'
import { RAT_DISPLAY } from '@/lib/constants'
import { formatBytes } from '@/lib/format'
import { stateVariant } from '@/lib/sim-utils'

const APN_TYPE_DISPLAY: Record<string, string> = {
  private_managed: 'Private Managed',
  operator_managed: 'Operator Managed',
  customer_managed: 'Customer Managed',
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-xs text-text-secondary">{label}</span>
      <span className={cn('text-sm text-text-primary', mono && 'font-mono text-xs')}>
        {value}
      </span>
    </div>
  )
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
  const { data: pools, isLoading } = useAPNIPPools(apnId)

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

      {(!pools || pools.length === 0) ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Server className="h-8 w-8 text-text-tertiary mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No IP Pools</h3>
            <p className="text-xs text-text-secondary">No IP pools configured for this APN.</p>
          </CardContent>
        </Card>
      ) : (
        <Card className="overflow-hidden">
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
                <TableRow key={pool.id}>
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
    </div>
  )
}

function SIMsTab({ apnId }: { apnId: string }) {
  const navigate = useNavigate()
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useAPNSims(apnId)

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
    <Card className="overflow-hidden">
      <Table>
        <TableHeader className="bg-bg-elevated">
          <TableRow>
            <TableHead>ICCID</TableHead>
            <TableHead>IMSI</TableHead>
            <TableHead>MSISDN</TableHead>
            <TableHead>State</TableHead>
            <TableHead>RAT</TableHead>
            <TableHead>Created</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {allSims.map((sim) => (
            <TableRow
              key={sim.id}
              className="cursor-pointer"
              onClick={() => navigate(`/sims/${sim.id}`)}
            >
              <TableCell>
                <span className="font-mono text-xs text-accent hover:underline">{sim.iccid}</span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{sim.imsi}</span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-xs text-text-secondary">{sim.msisdn ?? '-'}</span>
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
              <TableCell>
                <span className="text-xs text-text-secondary">{new Date(sim.created_at).toLocaleDateString()}</span>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {hasNextPage && (
        <div className="px-4 py-3 border-t border-border-subtle">
          <button
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1 flex items-center justify-center gap-2"
          >
            {isFetchingNextPage && <Spinner className="h-3 w-3" />}
            {isFetchingNextPage ? 'Loading...' : 'Load more SIMs'}
          </button>
        </div>
      )}
    </Card>
  )
}

function TrafficTab() {
  const mockData = useMemo(() => {
    return Array.from({ length: 24 }, (_, i) => ({
      hour: `${String(i).padStart(2, '0')}:00`,
      bytes_in: Math.floor(Math.random() * 200_000_000) + 10_000_000,
      bytes_out: Math.floor(Math.random() * 100_000_000) + 5_000_000,
    }))
  }, [])

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>24h Traffic Trend</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[280px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={mockData}>
                <defs>
                  <linearGradient id="apnGradIn" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--color-accent)" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="var(--color-accent)" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="apnGradOut" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--color-purple)" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="var(--color-purple)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis
                  dataKey="hour"
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
                  interval={3}
                />
                <YAxis
                  tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(v) => formatBytes(v)}
                  width={65}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--color-bg-elevated)',
                    border: '1px solid var(--color-border)',
                    borderRadius: 'var(--radius-sm)',
                    color: 'var(--color-text-primary)',
                    fontSize: '12px',
                  }}
                  formatter={(value) => [formatBytes(Number(value))]}
                />
                <Area
                  type="monotone"
                  dataKey="bytes_in"
                  stroke="var(--color-accent)"
                  fill="url(#apnGradIn)"
                  strokeWidth={2}
                  name="Bytes In"
                />
                <Area
                  type="monotone"
                  dataKey="bytes_out"
                  stroke="var(--color-purple)"
                  fill="url(#apnGradOut)"
                  strokeWidth={2}
                  name="Bytes Out"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="font-mono text-xl font-bold text-accent">
              {formatBytes(mockData.reduce((a, d) => a + d.bytes_in, 0))}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total In (24h)</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="font-mono text-xl font-bold text-purple">
              {formatBytes(mockData.reduce((a, d) => a + d.bytes_out, 0))}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total Out (24h)</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="font-mono text-xl font-bold text-text-primary">
              {formatBytes(mockData.reduce((a, d) => a + d.bytes_in + d.bytes_out, 0))}
            </div>
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-1">Total (24h)</div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

export default function ApnDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState('config')

  const { data: apn, isLoading, isError, refetch } = useAPN(id ?? '')
  const { data: operators } = useOperatorList()

  const operatorName = useMemo(() => {
    if (!apn || !operators) return 'Unknown'
    return operators.find((o) => o.id === apn.operator_id)?.name ?? 'Unknown'
  }, [apn, operators])

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
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
    <div className="p-6 space-y-4">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="icon" onClick={() => navigate('/apns')}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-[16px] font-semibold text-text-primary truncate">
              {apn.display_name || apn.name}
            </h1>
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
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
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
            Connected SIMs
          </TabsTrigger>
          <TabsTrigger value="traffic" className="gap-1.5">
            <BarChart3 className="h-3.5 w-3.5" />
            Traffic
          </TabsTrigger>
        </TabsList>

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
          <TrafficTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
