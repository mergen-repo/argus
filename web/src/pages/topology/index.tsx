import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  RefreshCw, AlertCircle, Wifi, Server, Cpu, Database, AlertTriangle, XCircle,
} from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/format'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { EntityLink } from '@/components/shared'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { Operator } from '@/types/operator'
import type { APN, IPPool } from '@/types/apn'
import type { ListResponse } from '@/types/sim'

interface TopologyData {
  operators: (Operator & { apns: (APN & { pools: IPPool[] })[] })[]
  totalSims: number
  totalSessions: number
  totalPools: number
}

function useTopologyData() {
  return useQuery({
    queryKey: ['topology'],
    queryFn: async () => {
      const [opRes, apnRes, poolRes] = await Promise.all([
        api.get<ListResponse<Operator>>('/operators?limit=100'),
        api.get<ListResponse<APN>>('/apns?limit=200'),
        api.get<ListResponse<IPPool>>('/ip-pools?limit=500'),
      ])
      const operators = opRes.data.data || []
      const apns = apnRes.data.data || []
      const pools = poolRes.data.data || []
      const tree = operators.map((op) => {
        const opApns = apns.filter((a) => a.operator_id === op.id)
        return { ...op, apns: opApns.map((apn) => ({ ...apn, pools: pools.filter((p) => p.apn_id === apn.id) })) }
      })
      return {
        operators: tree,
        totalSims: operators.reduce((s, o) => s + (o.sim_count || 0), 0),
        totalSessions: operators.reduce((s, o) => s + (o.active_sessions || 0), 0),
        totalPools: pools.length,
      } as TopologyData
    },
    staleTime: 60_000,
    refetchInterval: 30_000,
  })
}

function hc(status: string) {
  switch (status) {
    case 'healthy': return 'var(--color-success)'
    case 'degraded': return 'var(--color-warning)'
    case 'down': return 'var(--color-danger)'
    default: return 'var(--color-text-tertiary)'
  }
}

function FlowLine({ color = 'var(--color-accent)', active = false, severed = false, height = 28, traffic = 0 }: { color?: string; active?: boolean; severed?: boolean; height?: number; traffic?: number }) {
  const duration = traffic > 0 ? Math.max(0.4, 1.5 - traffic) : 1.5
  return (
    <div className="flex justify-center relative" style={{ height }}>
      {severed ? (
        <div className="w-px h-full" style={{ background: `repeating-linear-gradient(to bottom, var(--color-danger) 0px, var(--color-danger) 4px, transparent 4px, transparent 8px)` }} />
      ) : (
        <div className="w-px" style={{ backgroundColor: color + '30', height: '100%' }} />
      )}
      {active && !severed && (
        <div
          className="absolute top-0 left-1/2 -translate-x-1/2 w-1 rounded-full topo-flow"
          style={{
            backgroundColor: color,
            height: 8,
            boxShadow: `0 0 6px ${color}`,
            '--topo-flow-duration': `${duration}s`,
          } as React.CSSProperties}
        />
      )}
    </div>
  )
}

function TopologySkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-8 w-64" />
      <div className="flex justify-center py-8"><Skeleton className="h-24 w-24 rounded-full" /></div>
      <div className="flex gap-6 justify-center">
        {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-64 w-72" />)}
      </div>
    </div>
  )
}

export default function TopologyPage() {
  const navigate = useNavigate()
  const { data, isLoading, isError, refetch } = useTopologyData()

  if (isLoading) return <TopologySkeleton />
  if (isError || !data) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load topology</h2>
          <Button onClick={() => refetch()} variant="outline" className="gap-2"><RefreshCw className="h-4 w-4" /> Retry</Button>
        </div>
      </div>
    )
  }

  const healthyCnt = data.operators.filter((o) => o.health_status === 'healthy').length
  const degradedCnt = data.operators.filter((o) => o.health_status === 'degraded').length
  const downCnt = data.operators.filter((o) => o.health_status === 'down').length
  const hasIssues = degradedCnt > 0 || downCnt > 0
  const coreColor = downCnt > 0 ? 'var(--color-danger)' : degradedCnt > 0 ? 'var(--color-warning)' : 'var(--color-accent)'
  const maxOpSessions = Math.max(...data.operators.map((o) => o.active_sessions || 0), 1)

  return (
    <div className="space-y-5 overflow-x-auto">
      <style>{`
        @keyframes topo-flow { 0%{top:-8px;opacity:0}30%{opacity:1}100%{top:calc(100% + 8px);opacity:0} }
        .topo-flow{animation:topo-flow var(--topo-flow-duration,1.5s) ease-in-out infinite}
        @keyframes topo-ring{0%{transform:rotate(0)}100%{transform:rotate(360deg)}}
        .topo-ring{animation:topo-ring 20s linear infinite}
        @keyframes topo-breathe{0%,100%{box-shadow:0 0 0 transparent}50%{box-shadow:var(--topo-glow)}}
        .topo-breathe{animation:topo-breathe 3s ease-in-out infinite}
        @keyframes topo-bar-warn{0%,100%{opacity:1}50%{opacity:.5}}
        .topo-bar-warn{animation:topo-bar-warn 1.5s ease-in-out infinite}
        @keyframes topo-danger-flash{0%,100%{border-color:var(--color-danger)}50%{border-color:rgba(255,68,102,0.15)}}
        .topo-danger-flash{animation:topo-danger-flash 1.2s ease-in-out infinite}
        @keyframes topo-warn-pulse{0%,100%{border-color:var(--color-warning)}50%{border-color:rgba(255,184,0,0.15)}}
        .topo-warn-pulse{animation:topo-warn-pulse 2s ease-in-out infinite}
      `}</style>

      {/* Global alert banner when issues exist */}
      {hasIssues && (
        <div className={cn(
          'rounded-[var(--radius-md)] border px-4 py-2.5 flex items-center gap-3',
          downCnt > 0 ? 'border-danger/50 bg-danger-dim' : 'border-warning/50 bg-warning-dim',
        )}>
          {downCnt > 0 ? <XCircle className="h-4 w-4 text-danger shrink-0" /> : <AlertTriangle className="h-4 w-4 text-warning shrink-0" />}
          <span className={cn('text-xs font-medium', downCnt > 0 ? 'text-danger' : 'text-warning')}>
            {downCnt > 0 && `${downCnt} operator${downCnt > 1 ? 's' : ''} DOWN`}
            {downCnt > 0 && degradedCnt > 0 && ' — '}
            {degradedCnt > 0 && `${degradedCnt} degraded`}
            {' — immediate attention required'}
          </span>
          <Button variant="ghost" size="sm" className="ml-auto text-xs gap-1" onClick={() => navigate('/alerts')}>
            View Alerts
          </Button>
        </div>
      )}

      <div className="flex items-center justify-between">
        <div>
          <Breadcrumb items={[{ label: 'Dashboard', href: '/' }, { label: 'Topology' }]} className="mb-2" />
          <h1 className="text-[16px] font-semibold text-text-primary">Network Topology</h1>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-3 text-xs">
            {healthyCnt > 0 && <span className="flex items-center gap-1.5 text-success"><span className="h-2 w-2 rounded-full bg-success" />{healthyCnt} Healthy</span>}
            {degradedCnt > 0 && <span className="flex items-center gap-1.5 text-warning"><span className="h-2 w-2 rounded-full bg-warning animate-pulse" />{degradedCnt} Degraded</span>}
            {downCnt > 0 && <span className="flex items-center gap-1.5 text-danger"><span className="h-2 w-2 rounded-full bg-danger animate-pulse" />{downCnt} Down</span>}
          </div>
          <Button variant="ghost" size="sm" onClick={() => refetch()} className="gap-1.5 text-xs"><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
        </div>
      </div>

      {/* Core Node */}
      <div className="flex flex-col items-center">
        <div className="relative w-20 h-20">
          <svg className="absolute inset-0 w-20 h-20 topo-ring" viewBox="0 0 80 80">
            <circle cx="40" cy="40" r="36" fill="none" stroke={coreColor} strokeWidth="1.5" strokeDasharray="8 6" opacity="0.4" />
          </svg>
          <div className="absolute inset-2 rounded-full border flex items-center justify-center bg-bg-surface" style={{ borderColor: coreColor + '60', boxShadow: `0 0 20px ${coreColor}15` }}>
            <Cpu className="h-7 w-7" style={{ color: coreColor }} />
          </div>
        </div>
        <span className="text-[10px] font-bold uppercase tracking-widest mt-1" style={{ color: coreColor }}>ARGUS CORE</span>
        <div className="flex items-center gap-4 mt-1 text-[10px] text-text-tertiary font-mono">
          <span>{formatNumber(data.totalSims)} SIMs</span>
          <span>{formatNumber(data.totalSessions)} sess</span>
          <span>{data.operators.length} ops</span>
          <span>{data.totalPools} pools</span>
        </div>
      </div>

      <FlowLine color={coreColor} active={data.totalSessions > 0} height={32} traffic={data.totalSessions / maxOpSessions} />

      {/* Operators — always side by side, full width */}
      <div className="flex gap-5 w-full">
        {data.operators.map((op) => {
          const hasTraffic = (op.active_sessions || 0) > 0
          const isDown = op.health_status === 'down'
          const isDegraded = op.health_status === 'degraded'
          const opColor = hc(op.health_status)
          const opTraffic = (op.active_sessions || 0) / maxOpSessions

          return (
            <div key={op.id} className="flex flex-col items-center flex-1 min-w-0">
              {/* Operator Card */}
              <div
                className={cn(
                  'w-full rounded-[var(--radius-md)] border bg-bg-surface p-4 cursor-pointer transition-all topo-breathe',
                  isDown && 'topo-danger-flash bg-danger-dim/30',
                  isDegraded && 'topo-warn-pulse bg-warning-dim/20',
                  !isDown && !isDegraded && 'card-hover',
                )}
                style={{
                  borderColor: isDown ? undefined : isDegraded ? undefined : opColor + '40',
                  '--topo-glow': hasTraffic ? `0 0 16px ${opColor}25` : '0 0 0px transparent',
                } as React.CSSProperties}
                onClick={() => navigate(`/operators/${op.id}`)}
              >
                <div className="flex items-center gap-2 mb-1.5">
                  <span className={cn('h-2.5 w-2.5 rounded-full shrink-0', (isDown || isDegraded) && 'animate-pulse')} style={{ backgroundColor: opColor, boxShadow: `0 0 ${isDown ? 12 : 8}px ${opColor}80` }} />
                  <span className={cn('text-sm font-semibold truncate', isDown ? 'text-danger' : 'text-text-primary')} onClick={(e) => e.stopPropagation()}><EntityLink entityType="operator" entityId={op.id} label={op.name} /></span>
                  <Badge variant={op.health_status === 'healthy' ? 'success' : op.health_status === 'degraded' ? 'warning' : 'danger'} className="text-[9px] ml-auto shrink-0">
                    {isDown && <XCircle className="h-2.5 w-2.5 mr-0.5" />}
                    {isDegraded && <AlertTriangle className="h-2.5 w-2.5 mr-0.5" />}
                    {op.health_status?.toUpperCase()}
                  </Badge>
                </div>
                <div className="flex items-center gap-3 text-[10px] text-text-tertiary font-mono">
                  <span>MCC: {op.mcc}</span><span>MNC: {op.mnc}</span>
                </div>
                <div className="flex items-center gap-4 mt-1.5 text-[11px] font-mono">
                  <span className={isDown ? 'text-danger' : 'text-accent'}>{formatNumber(op.sim_count || 0)} SIMs</span>
                  <span className={isDown ? 'text-danger/70' : hasTraffic ? 'text-success' : 'text-text-tertiary'}>
                    {isDown ? 'OFFLINE' : `${formatNumber(op.active_sessions || 0)} sessions`}
                  </span>
                </div>
                {isDown && (
                  <div className="mt-2 text-[10px] text-danger font-medium flex items-center gap-1">
                    <XCircle className="h-3 w-3" />
                    Connection lost — check circuit breaker
                  </div>
                )}
              </div>

              {/* APNs */}
              {op.apns.length === 0 ? (
                <div className="mt-2 flex flex-col items-center">
                  <FlowLine color="var(--color-danger)" severed={isDown} height={20} />
                  <span className="text-[10px] text-danger italic">No APNs configured</span>
                </div>
              ) : (
                op.apns.map((apn) => {
                  const apnSevered = isDown || apn.state !== 'active'
                  const apnActive = hasTraffic && !apnSevered
                  const maxApnSims = Math.max(...op.apns.map((a) => a.sim_count || 0), 1)
                  const apnTraffic = (apn.sim_count || 0) / maxApnSims * opTraffic
                  return (
                    <div key={apn.id} className="flex flex-col items-center w-full">
                      <FlowLine color={opColor} active={apnActive} severed={apnSevered} height={24} traffic={apnTraffic} />
                      <div className="h-2 w-2 rounded-full" style={{
                        backgroundColor: apnSevered ? 'var(--color-danger)' : 'var(--color-cyan)',
                        boxShadow: apnSevered ? '0 0 8px var(--color-danger)' : apnActive ? '0 0 8px var(--color-cyan)' : 'none',
                      }} />

                      {/* APN Card */}
                      <div
                        className={cn(
                          'w-[90%] rounded-[var(--radius-sm)] border p-3 mt-1 cursor-pointer transition-colors',
                          apnSevered ? 'border-danger/30 bg-danger-dim/20 opacity-60' : 'border-border bg-bg-elevated hover:border-cyan',
                        )}
                        onClick={() => navigate(`/apns/${apn.id}`)}
                      >
                        <div className="flex items-center gap-2">
                          <Wifi className={cn('h-3.5 w-3.5 shrink-0', apnSevered ? 'text-danger' : apnActive ? 'text-cyan' : 'text-text-tertiary')} />
                          <span className={cn('text-xs font-medium truncate', apnSevered ? 'text-danger/80' : 'text-text-primary')} onClick={(e) => e.stopPropagation()}><EntityLink entityType="apn" entityId={apn.id} label={apn.name} /></span>
                          <Badge variant={apnSevered ? 'danger' : apn.state === 'active' ? 'success' : 'secondary'} className="text-[9px] ml-auto shrink-0">
                            {isDown ? 'UNREACHABLE' : apn.state}
                          </Badge>
                        </div>
                        <div className="text-[10px] font-mono text-text-tertiary mt-1">{apn.supported_rat_types?.join(', ')}</div>
                      </div>

                      {/* IP Pools */}
                      {apn.pools.map((pool) => {
                        const pct = pool.total_addresses > 0 ? (pool.used_addresses / pool.total_addresses) * 100 : 0
                        const pctColor = pct > 90 ? 'text-danger' : pct > 70 ? 'text-warning' : 'text-success'
                        const barColor = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'
                        const critical = pct > 85
                        return (
                          <div key={pool.id} className="flex flex-col items-center w-full">
                            <FlowLine color="var(--color-border)" height={16} />
                            <div
                              className={cn(
                                'w-[80%] rounded-[var(--radius-sm)] border p-2.5 cursor-pointer transition-colors',
                                apnSevered ? 'border-danger/20 opacity-50' : critical ? 'border-danger/30 bg-danger-dim/10' : 'border-border-subtle bg-bg-primary hover:border-accent/40',
                              )}
                              onClick={() => navigate(`/settings/ip-pools/${pool.id}`)}
                            >
                              <div className="flex items-center gap-2">
                                <Database className={cn('h-3 w-3 shrink-0', critical ? 'text-danger' : 'text-text-tertiary')} />
                                <span className="text-[11px] font-medium text-text-primary truncate">{pool.name}</span>
                              </div>
                              <div className="font-mono text-[10px] text-text-tertiary mt-0.5">{pool.cidr_v4 || pool.cidr_v6}</div>
                              <div className="flex items-center gap-3 mt-1 text-[10px] font-mono">
                                <span className={pctColor}>{pct.toFixed(0)}%</span>
                                <span className="text-text-tertiary">{pool.used_addresses}/{pool.total_addresses}</span>
                                <span className="text-text-tertiary">{pool.available_addresses} free</span>
                              </div>
                              <div className={cn('w-full h-1 bg-bg-hover rounded-full overflow-hidden mt-1.5', critical && 'topo-bar-warn')}>
                                <div className={cn('h-full rounded-full transition-all duration-700', barColor)} style={{ width: `${Math.min(pct, 100)}%` }} />
                              </div>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )
                })
              )}
            </div>
          )
        })}
      </div>

      {data.operators.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <Server className="h-10 w-10 text-text-tertiary mb-3" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">No operators</h3>
          <p className="text-xs text-text-secondary">Add operators to see the network topology.</p>
        </div>
      )}
    </div>
  )
}
