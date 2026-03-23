import { useState, useMemo, useEffect, useRef } from 'react'
import {
  Activity,
  Clock,
  Users,
  Wifi,
  WifiOff,
  Search,
  RefreshCw,
  AlertCircle,
  Loader2,
} from 'lucide-react'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
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
}: {
  session: Session
  isNew: boolean
  isEnded: boolean
  onDisconnect: (session: Session) => void
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
        'transition-all duration-500',
        isNew && 'animate-in fade-in slide-in-from-top-1 bg-success-dim',
        isEnded && 'opacity-40',
      )}
    >
      <TableCell>
        <span className="font-mono text-xs text-accent">{session.imsi}</span>
      </TableCell>
      <TableCell>
        <span className="text-xs text-text-secondary">{session.operator_id.slice(0, 8)}</span>
      </TableCell>
      <TableCell>
        <span className="text-xs text-text-secondary">{session.apn_id?.slice(0, 8) ?? '-'}</span>
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
        <span className="font-mono text-xs text-text-tertiary">{session.ip_address ?? session.framed_ip ?? '-'}</span>
      </TableCell>
      <TableCell>
        {session.rat_type ? (
          <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium">
            {session.rat_type}
          </span>
        ) : (
          <span className="text-text-tertiary text-xs">-</span>
        )}
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
    </TableRow>
  )
}

export default function SessionListPage() {
  const [disconnectTarget, setDisconnectTarget] = useState<Session | null>(null)
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

  const disconnectMutation = useDisconnectSession()
  const newSessionIds = useRealtimeSessionStarted()
  const endedSessionIds = useRealtimeSessionEnded()

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
    <div className="p-6 space-y-4">
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
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>IMSI</TableHead>
                <TableHead>Operator</TableHead>
                <TableHead>APN</TableHead>
                <TableHead>NAS IP</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Bytes In</TableHead>
                <TableHead>Bytes Out</TableHead>
                <TableHead>IP Address</TableHead>
                <TableHead>RAT</TableHead>
                <TableHead className="w-24" />
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

              {!isLoading && allSessions.length === 0 && (
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

              {allSessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  isNew={newSessionIds.current.has(session.id)}
                  isEnded={endedSessionIds.current.has(session.id)}
                  onDisconnect={setDisconnectTarget}
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
            <button
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1"
            >
              Load more sessions
            </button>
          ) : allSessions.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              Showing all {allSessions.length} active sessions
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
    </div>
  )
}
