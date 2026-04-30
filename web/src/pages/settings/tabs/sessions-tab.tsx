// Tab body for /settings#sessions. Parent <Tabs> only renders active <TabsContent>, so data hooks fire once when active.
import { useState } from 'react'
import {
  Monitor,
  AlertCircle,
  RefreshCw,
  Loader2,
  ShieldOff,
  MapPin,
  Clock,
  Smartphone,
  LogOut,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
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
import { Skeleton } from '@/components/ui/skeleton'
import { useSessions, useRevokeSession, useRevokeAllSessions } from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import type { AuthSessionItem } from '@/types/settings'

function formatDate(iso: string) {
  return new Date(iso).toLocaleString('en-GB', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function parseUserAgent(ua: string | null): { browser: string; os: string } {
  if (!ua) return { browser: 'Unknown', os: 'Unknown' }

  let browser = 'Unknown'
  if (ua.includes('Firefox')) browser = 'Firefox'
  else if (ua.includes('Edg')) browser = 'Edge'
  else if (ua.includes('Chrome')) browser = 'Chrome'
  else if (ua.includes('Safari')) browser = 'Safari'
  else if (ua.includes('curl')) browser = 'curl'
  else if (ua.includes('PostmanRuntime')) browser = 'Postman'

  let os = 'Unknown'
  if (ua.includes('Windows')) os = 'Windows'
  else if (ua.includes('Macintosh') || ua.includes('Mac OS')) os = 'macOS'
  else if (ua.includes('Linux')) os = 'Linux'
  else if (ua.includes('Android')) os = 'Android'
  else if (ua.includes('iPhone') || ua.includes('iPad')) os = 'iOS'

  return { browser, os }
}

export default function SessionsTab() {
  const user = useAuthStore((s) => s.user)
  const currentSessionId = useAuthStore((s) => s.sessionId)

  const { data: sessions, isLoading, isError, refetch } = useSessions()
  const revokeMutation = useRevokeSession()
  const revokeAllMutation = useRevokeAllSessions(user?.id ?? '')

  const [confirmRevokeAll, setConfirmRevokeAll] = useState(false)
  const [confirmRevoke, setConfirmRevoke] = useState<AuthSessionItem | null>(null)

  const handleRevoke = async () => {
    if (!confirmRevoke) return
    try {
      await revokeMutation.mutateAsync(confirmRevoke.id)
      setConfirmRevoke(null)
    } catch {
    }
  }

  const handleRevokeAll = async () => {
    try {
      await revokeAllMutation.mutateAsync()
      setConfirmRevokeAll(false)
    } catch {
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load sessions</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch active sessions. Please try again.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  const otherSessions = (sessions ?? []).filter((s) => s.id !== currentSessionId)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <div>
          <p className="text-xs text-text-tertiary mt-0.5">
            Devices and browsers currently signed in to your account
          </p>
        </div>
        {otherSessions.length > 0 && (
          <Button
            variant="outline"
            size="sm"
            className="gap-2 border-danger/40 text-danger hover:bg-danger/10 hover:border-danger"
            onClick={() => setConfirmRevokeAll(true)}
          >
            <ShieldOff className="h-3.5 w-3.5" />
            Revoke All Other Sessions
          </Button>
        )}
      </div>

      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead className="w-40">
                  <span className="flex items-center gap-1.5">
                    <Clock className="h-3.5 w-3.5 text-text-tertiary" />
                    Created
                  </span>
                </TableHead>
                <TableHead>
                  <span className="flex items-center gap-1.5">
                    <Clock className="h-3.5 w-3.5 text-text-tertiary" />
                    Expires
                  </span>
                </TableHead>
                <TableHead>
                  <span className="flex items-center gap-1.5">
                    <MapPin className="h-3.5 w-3.5 text-text-tertiary" />
                    IP Address
                  </span>
                </TableHead>
                <TableHead>
                  <span className="flex items-center gap-1.5">
                    <Smartphone className="h-3.5 w-3.5 text-text-tertiary" />
                    Client
                  </span>
                </TableHead>
                <TableHead className="w-32 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 5 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-24" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && (sessions ?? []).length === 0 && (
                <TableRow>
                  <TableCell colSpan={5}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Monitor className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No active sessions</h3>
                        <p className="text-xs text-text-secondary">Your active sessions will appear here.</p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(sessions ?? []).map((session) => {
                const isCurrent = session.id === currentSessionId
                const { browser, os } = parseUserAgent(session.user_agent)
                return (
                  <TableRow key={session.id} className={isCurrent ? 'bg-accent/5' : undefined}>
                    <TableCell>
                      <span className="text-xs text-text-secondary font-mono">
                        {formatDate(session.created_at)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-xs text-text-secondary font-mono">
                        {formatDate(session.expires_at)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-xs text-text-secondary font-mono">
                        {session.ip_address ?? '—'}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="text-xs font-medium text-text-primary">{browser}</span>
                        <span className="text-[10px] text-text-tertiary">{os}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {isCurrent ? (
                        <Badge variant="success" className="text-[10px]">This session</Badge>
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-xs text-danger hover:text-danger h-7 gap-1"
                          onClick={() => setConfirmRevoke(session)}
                        >
                          <LogOut className="h-3 w-3" />
                          Revoke
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
        {!isLoading && (sessions ?? []).length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {(sessions ?? []).length} active session{(sessions ?? []).length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      <Dialog open={!!confirmRevoke} onOpenChange={() => setConfirmRevoke(null)}>
        <DialogContent onClose={() => setConfirmRevoke(null)}>
          <DialogHeader>
            <DialogTitle>Revoke Session?</DialogTitle>
            <DialogDescription>
              This will immediately sign out the session from{' '}
              <strong>{confirmRevoke?.ip_address ?? 'unknown IP'}</strong>. This action cannot be
              undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRevoke(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleRevoke}
              disabled={revokeMutation.isPending}
              className="gap-2"
            >
              {revokeMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Revoke Session
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmRevokeAll} onOpenChange={setConfirmRevokeAll}>
        <DialogContent onClose={() => setConfirmRevokeAll(false)}>
          <DialogHeader>
            <DialogTitle>Revoke All Other Sessions?</DialogTitle>
            <DialogDescription>
              This will immediately sign out all{' '}
              <strong>{otherSessions.length} other session{otherSessions.length !== 1 ? 's' : ''}</strong>.
              Your current session will remain active.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRevokeAll(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleRevokeAll}
              disabled={revokeAllMutation.isPending}
              className="gap-2"
            >
              {revokeAllMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Revoke All Other Sessions
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
