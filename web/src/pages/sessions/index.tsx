import { useState, useMemo, useEffect, useRef, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Activity,
  Clock,
  Users,
  Wifi,
  WifiOff,
  Search,
  X,
  RefreshCw,
  AlertCircle,
  Loader2,
  ExternalLink,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { RowActionsMenu } from '@/components/shared/row-actions-menu'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  useSessionList,
  useSessionStats,
  useDisconnectSession,
  useRealtimeSessionStarted,
  useRealtimeSessionEnded,
} from '@/hooks/use-sessions'
import { Skeleton } from '@/components/ui/skeleton'
import type { Session } from '@/types/session'
import { cn } from '@/lib/utils'
import { formatBytes, formatDuration, formatNumber } from '@/lib/format'
import { RATBadge } from '@/components/ui/rat-badge'

function LiveDot() {
  return (
    <span className="flex items-center gap-1">
      <span
        className="h-1.5 w-1.5 rounded-full bg-success animate-pulse"
        style={{ boxShadow: '0 0 6px rgba(0,255,136,0.4)' }}
      />
      <span className="text-[10px] text-text-tertiary">LIVE</span>
    </span>
  )
}

interface StatCardProps {
  label: string
  value: string
  icon: React.ReactNode
  color: string
}

function StatCard({ label, value, icon, color }: StatCardProps) {
  return (
    <div className="flex items-center gap-3 px-4 py-3 rounded-[var(--radius-md)] bg-bg-surface border border-border">
      <span style={{ color }} className="opacity-70">{icon}</span>
      <div>
        <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">{label}</p>
        <p className="font-mono text-lg font-bold text-text-primary leading-none mt-0.5">{value}</p>
      </div>
    </div>
  )
}

function SessionRow({
  session,
  isNew,
  isEnded,
  onDisconnect,
  onRowClick,
  onIMSIClick,
  onNavigateDetail,
}: {
  session: Session
  isNew: boolean
  isEnded: boolean
  onDisconnect: (session: Session) => void
  onRowClick: (session: Session) => void
  onIMSIClick: (simId: string) => void
  onNavigateDetail?: (session: Session) => void
}) {
  const [elapsed, setElapsed] = useState(session.duration_sec)

  useEffect(() => {
    if (session.state !== 'active') return
    const interval = setInterval(() => {
      const startMs = new Date(session.started_at).getTime()
      setElapsed((Date.now() - startMs) / 1000)
    }, 1000)
    return () => clearInterval(interval)
  }, [session.started_at, session.state])

  return (
    <TableRow
      className={cn(
        'transition-all duration-500 cursor-pointer',
        isNew && 'animate-in fade-in slide-in-from-top-1 bg-success-dim',
        isEnded && 'opacity-40',
      )}
      onClick={() => onRowClick(session)}
    >
      <TableCell>
        <span
          className="font-mono text-xs text-accent hover:underline cursor-pointer"
          onClick={(e) => { e.stopPropagation(); if (session.sim_id) onIMSIClick(session.sim_id) }}
        >
          {session.imsi}
        </span>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs text-text-tertiary">{session.ip_address ?? session.framed_ip ?? '-'}</span>
      </TableCell>
      <TableCell>
        <span className="text-xs text-text-secondary">{session.operator_name || session.operator_id.slice(0, 8)}</span>
      </TableCell>
      <TableCell>
        <span className="text-xs text-text-secondary">{session.apn_name || (session.apn_id?.slice(0, 8) ?? '-')}</span>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs text-text-tertiary">{session.nas_ip}</span>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs text-text-primary">{formatDuration(elapsed)}</span>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs text-success">{formatBytes(session.bytes_in)}</span>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs text-accent">{formatBytes(session.bytes_out)}</span>
      </TableCell>
      <TableCell>
        <RATBadge ratType={session.rat_type} />
      </TableCell>
      <TableCell>
        <Button
          variant="outline"
          size="sm"
          className="h-6 px-2 text-[11px] gap-1 border-danger/30 text-danger hover:bg-danger-dim"
          onClick={(e) => {
            e.stopPropagation()
            onDisconnect(session)
          }}
          disabled={session.state !== 'active'}
        >
          <WifiOff className="h-3 w-3" />
          Disconnect
        </Button>
      </TableCell>
      <TableCell onClick={(e) => e.stopPropagation()}>
        <RowActionsMenu
          actions={[
            { label: 'View Details', onClick: () => onNavigateDetail ? onNavigateDetail(session) : onRowClick(session) },
            { label: 'View SIM', onClick: () => session.sim_id && onIMSIClick(session.sim_id), disabled: !session.sim_id },
          ]}
        />
      </TableCell>
    </TableRow>
  )
}

export default function SessionListPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [disconnectTarget, setDisconnectTarget] = useState<Session | null>(null)
  const [selectedSession, setSelectedSession] = useState<Session | null>(null)
  const filterText = searchParams.get('q') ?? ''
  const setFilterText = useCallback((val: string) => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      if (val) p.set('q', val); else p.delete('q')
      return p
    }, { replace: true })
  }, [setSearchParams])
  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  const { data: stats, isLoading: statsLoading } = useSessionStats()
  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useSessionList({})

  const sessionFilters = useMemo(() => ({}), [])
  const disconnectMutation = useDisconnectSession()
  const newSessionIds = useRealtimeSessionStarted(sessionFilters)
  const endedSessionIds = useRealtimeSessionEnded(sessionFilters)

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el) return
    observerRef.current = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 },
    )
    observerRef.current.observe(el)
    return () => { observerRef.current?.disconnect() }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const allSessions = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((page) => page.data)
  }, [data])

  const filteredSessions = useMemo(() => {
    if (!filterText.trim()) return allSessions
    const q = filterText.trim().toLowerCase()
    return allSessions.filter((s) => {
      const ip = (s.ip_address ?? s.framed_ip ?? '').toLowerCase()
      return s.imsi.toLowerCase().includes(q) || ip.includes(q)
    })
  }, [allSessions, filterText])

  const handleDisconnect = async () => {
    if (!disconnectTarget) return
    try {
      await disconnectMutation.mutateAsync({ sessionId: disconnectTarget.id })
      setDisconnectTarget(null)
    } catch {
      // handled by api interceptor
    }
  }

  const topOperators = useMemo(() => {
    if (!stats?.by_operator) return []
    return Object.entries(stats.by_operator)
      .sort(([, a], [, b]) => b - a)
      .slice(0, 3)
  }, [stats])

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load sessions</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch session data. Please try again.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-3">
          <h1 className="text-[16px] font-semibold text-text-primary">Live Sessions</h1>
          <LiveDot />
        </div>
        <Button variant="outline" size="sm" className="gap-2" onClick={() => refetch()}>
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh
        </Button>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={filterText}
            onChange={(e) => setFilterText(e.target.value)}
            placeholder="Filter by IMSI or IP..."
            className="pl-9 h-8 text-sm"
          />
          {filterText && (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Clear search"
              onClick={() => setFilterText('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors h-5 w-5"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
        {filterText && (
          <span className="text-xs text-text-tertiary">
            {filteredSessions.length} of {allSessions.length} sessions
          </span>
        )}
      </div>

      {/* Stats Bar */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        {statsLoading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="px-4 py-3 rounded-[var(--radius-md)] bg-bg-surface border border-border">
              <Skeleton className="h-3 w-20 mb-2" />
              <Skeleton className="h-6 w-16" />
            </div>
          ))
        ) : (
          <>
            <StatCard
              label="Total Active"
              value={formatNumber(stats?.total_active ?? 0)}
              icon={<Activity className="h-4 w-4" />}
              color="var(--color-success)"
            />
            <StatCard
              label="Avg Duration"
              value={formatDuration(stats?.avg_duration_sec ?? 0)}
              icon={<Clock className="h-4 w-4" />}
              color="var(--color-accent)"
            />
            {topOperators.length > 0 && (
              <StatCard
                label={`Top: ${topOperators[0][0].slice(0, 8)}`}
                value={formatNumber(topOperators[0][1])}
                icon={<Users className="h-4 w-4" />}
                color="var(--color-purple)"
              />
            )}
            <StatCard
              label="Avg Usage"
              value={formatBytes(stats?.avg_bytes ?? 0)}
              icon={<Wifi className="h-4 w-4" />}
              color="var(--color-warning)"
            />
          </>
        )}
      </div>

      {/* Session Table */}
      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>IMSI</TableHead>
                <TableHead>IP Address</TableHead>
                <TableHead>Operator</TableHead>
                <TableHead>APN</TableHead>
                <TableHead>NAS IP</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Bytes In</TableHead>
                <TableHead>Bytes Out</TableHead>
                <TableHead>RAT</TableHead>
                <TableHead className="w-24" />
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 8 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 10 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && filteredSessions.length === 0 && (
                <TableRow>
                  <TableCell colSpan={10}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Wifi className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No active sessions</h3>
                        <p className="text-xs text-text-secondary">Sessions will appear here as devices connect.</p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {filteredSessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  isNew={newSessionIds.current.has(session.id)}
                  isEnded={endedSessionIds.current.has(session.id)}
                  onDisconnect={setDisconnectTarget}
                  onRowClick={(s) => setSelectedSession(s)}
                  onIMSIClick={(simId) => navigate(`/sims/${simId}`)}
                  onNavigateDetail={(s) => navigate(`/sessions/${s.id}`)}
                />
              ))}
            </TableBody>
          </Table>
        </div>

        <div ref={loadMoreRef} className="px-4 py-3 border-t border-border-subtle">
          {isFetchingNextPage ? (
            <div className="flex items-center justify-center gap-2 text-text-tertiary text-xs">
              <Spinner className="h-3.5 w-3.5" />
              Loading more...
            </div>
          ) : hasNextPage ? (
            <Button
              variant="ghost"
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent py-1"
            >
              Load more sessions
            </Button>
          ) : filteredSessions.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {filteredSessions.length} active sessions
            </p>
          ) : null}
        </div>
      </Card>

      {/* Disconnect Confirmation Dialog */}
      <Dialog open={!!disconnectTarget} onOpenChange={() => setDisconnectTarget(null)}>
        <DialogContent onClose={() => setDisconnectTarget(null)}>
          <DialogHeader>
            <DialogTitle>Force Disconnect Session?</DialogTitle>
            <DialogDescription>
              This will immediately terminate the session for IMSI{' '}
              <span className="font-mono text-accent">{disconnectTarget?.imsi}</span>.
              The device will need to re-authenticate to establish a new session.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDisconnectTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDisconnect}
              disabled={disconnectMutation.isPending}
              className="gap-2"
            >
              {disconnectMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Disconnect
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <SlidePanel
        open={!!selectedSession}
        onOpenChange={(open) => { if (!open) setSelectedSession(null) }}
        title={selectedSession ? `Session — ${selectedSession.imsi}` : 'Session Detail'}
        description={selectedSession?.id}
        width="lg"
      >
        {selectedSession && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">IMSI</div>
                <div className="font-mono text-xs text-text-primary">{selectedSession.imsi}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">IP Address</div>
                <div className="font-mono text-xs text-text-primary">{selectedSession.ip_address || selectedSession.framed_ip || '-'}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Operator</div>
                <div className="text-xs text-text-primary">{selectedSession.operator_name || selectedSession.operator_id.slice(0, 12)}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">APN</div>
                <div className="text-xs text-text-primary">{selectedSession.apn_name || selectedSession.apn_id || '-'}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">NAS IP</div>
                <div className="font-mono text-xs text-text-primary">{selectedSession.nas_ip}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">RAT Type</div>
                <div className="text-xs text-text-primary">{selectedSession.rat_type || '-'}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Bytes In</div>
                <div className="font-mono text-xs text-success">{formatBytes(selectedSession.bytes_in)}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Bytes Out</div>
                <div className="font-mono text-xs text-accent">{formatBytes(selectedSession.bytes_out)}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Duration</div>
                <div className="font-mono text-xs text-text-primary">{formatDuration(selectedSession.duration_sec)}</div>
              </div>
              <div className="rounded-[var(--radius-sm)] border border-border p-3">
                <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Started At</div>
                <div className="text-xs text-text-primary">{new Date(selectedSession.started_at).toLocaleString()}</div>
              </div>
            </div>
            {selectedSession.sim_id && (
              <div className="flex items-center justify-end pt-2">
                <Button size="sm" className="gap-2" onClick={() => { setSelectedSession(null); navigate(`/sims/${selectedSession.sim_id}`) }}>
                  <ExternalLink className="h-3.5 w-3.5" />
                  Open SIM Detail
                </Button>
              </div>
            )}
          </div>
        )}
      </SlidePanel>
    </div>
  )
}
