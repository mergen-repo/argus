import { Radio, Wifi, AlertCircle, Activity } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useOpsSnapshot } from '@/hooks/use-ops'
import { wsClient } from '@/lib/ws'
import { useQueryClient } from '@tanstack/react-query'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import { useMemo, useRef, useEffect } from 'react'

interface AAADataPoint {
  t: string
  radius: number
  diameter: number
  sba: number
}

export default function AAATraffic() {
  // Snapshot drives the per-protocol gauges (5s polling). WebSocket
  // metrics.realtime events also invalidate the snapshot query so the
  // gauges feel "live" between polls (AC-3).
  const { data, isLoading } = useOpsSnapshot(5_000)
  const historyRef = useRef<AAADataPoint[]>([])
  const qc = useQueryClient()

  useEffect(() => {
    const unsub = wsClient.on('metrics.realtime', () => {
      qc.invalidateQueries({ queryKey: ['ops', 'snapshot'] })
    })
    return unsub
  }, [qc])

  useEffect(() => {
    if (!data?.aaa?.by_protocol) return
    const protocols = data.aaa.by_protocol
    const point: AAADataPoint = {
      t: new Date().toLocaleTimeString(),
      radius: protocols.find((p) => p.protocol === 'radius')?.req_per_sec ?? 0,
      diameter: protocols.find((p) => p.protocol === 'diameter')?.req_per_sec ?? 0,
      sba: protocols.find((p) => p.protocol === 'sba' || p.protocol === '5g')?.req_per_sec ?? 0,
    }
    historyRef.current = [...historyRef.current.slice(-59), point]
  }, [data])

  const chartData = historyRef.current.length > 0 ? historyRef.current : [{ t: 'now', radius: 0, diameter: 0, sba: 0 }]

  const protocols = useMemo(() => data?.aaa?.by_protocol ?? [], [data])

  const isSpike = useMemo(() => {
    const history = historyRef.current
    if (history.length < 2) return false
    const latest = history[history.length - 1]
    const totalCurrent = (latest?.radius ?? 0) + (latest?.diameter ?? 0) + (latest?.sba ?? 0)
    const avg = history.slice(0, -1).reduce((sum, h) => sum + h.radius + h.diameter + h.sba, 0) / (history.length - 1)
    return avg > 0 && totalCurrent > avg * 2
  }, [])

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <div className="grid grid-cols-3 gap-4">
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
        </div>
        <Skeleton className="h-48" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-3">
        <Radio className="h-4 w-4 text-accent" />
        <h1 className="text-[15px] font-semibold text-text-primary">AAA Live Traffic</h1>
        <Badge className={isSpike ? 'bg-danger-dim text-danger border-0 animate-pulse' : 'bg-success-dim text-success border-0'}>
          <span className="mr-1">{isSpike ? '▲ SPIKE' : '● LIVE'}</span>
        </Badge>
        <span className="text-[11px] text-text-tertiary ml-auto">Polling 5s — `aaa.auth.tick` WS not available, using snapshot fallback</span>
      </div>

      {protocols.length === 0 ? (
        <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
          <CardContent className="p-6 text-center">
            <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
            <p className="text-[14px] text-text-secondary">No AAA traffic data available. Start RADIUS/Diameter/5G traffic to populate.</p>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {['radius', 'diameter', 'sba'].map((proto) => {
              const p = protocols.find((x) => x.protocol === proto || (proto === 'sba' && x.protocol === '5g'))
              return (
                <Card
                  key={proto}
                  className={`bg-bg-surface border-border rounded-[10px] shadow-card ${isSpike ? 'shadow-[0_0_12px_rgba(255,68,102,0.3)] border-danger' : ''}`}
                >
                  <CardContent className="p-6">
                    <div className="flex items-center gap-2 mb-3">
                      <Wifi className="h-4 w-4 text-accent" />
                      <span className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary">
                        {proto === 'sba' ? '5G SBA' : proto.toUpperCase()} req/s
                      </span>
                    </div>
                    <div className="text-[28px] font-mono font-bold text-text-primary">
                      {p ? p.req_per_sec.toLocaleString(undefined, { maximumFractionDigits: 0 }) : '0'}
                    </div>
                    {p && (
                      <div className="mt-2 flex gap-3 text-[11px]">
                        <span className="text-text-tertiary">
                          Success: <span className="text-success">{(p.success_rate * 100).toFixed(1)}%</span>
                        </span>
                        <span className="text-text-tertiary">
                          p99: <span className="font-mono text-text-secondary">{p.p99_ms.toFixed(1)}ms</span>
                        </span>
                      </div>
                    )}
                  </CardContent>
                </Card>
              )
            })}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {protocols.map((p) => (
              <Card key={`stat-${p.protocol}`} className="bg-bg-surface border-border rounded-[10px] shadow-card">
                <CardContent className="p-6">
                  <div className="flex items-center gap-2 mb-1">
                    <Activity className="h-4 w-4 text-text-tertiary" />
                    <span className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary">{p.protocol} auth latency p99</span>
                  </div>
                  <div className="text-[28px] font-mono font-bold text-text-primary">{p.p99_ms.toFixed(1)}ms</div>
                </CardContent>
              </Card>
            ))}
          </div>

          <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
            <CardHeader className="pb-3">
              <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
                Protocol Traffic — Rolling 60s
              </CardTitle>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={200}>
                <AreaChart data={chartData}>
                  <XAxis dataKey="t" tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} />
                  <YAxis tick={{ fill: 'var(--color-text-tertiary)', fontSize: 10 }} />
                  <Tooltip
                    contentStyle={{ background: 'var(--color-bg-elevated)', border: '1px solid var(--color-border)', borderRadius: 8 }}
                    labelStyle={{ color: 'var(--color-text-secondary)' }}
                    itemStyle={{ color: 'var(--color-text-primary)' }}
                  />
                  <Legend iconType="circle" iconSize={8} />
                  <Area type="monotone" dataKey="radius" stackId="1" stroke="var(--color-accent)" fill="var(--color-accent)" fillOpacity={0.15} name="RADIUS" />
                  <Area type="monotone" dataKey="diameter" stackId="1" stroke="var(--color-success)" fill="var(--color-success)" fillOpacity={0.12} name="Diameter" />
                  <Area type="monotone" dataKey="sba" stackId="1" stroke="var(--color-purple)" fill="var(--color-purple)" fillOpacity={0.12} name="5G SBA" />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
