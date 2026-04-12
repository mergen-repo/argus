import { useState } from 'react'
import {
  AlertCircle,
  RefreshCw,
  Copy,
  Check,
  Archive,
  KeyRound,
  CalendarClock,
  ShieldCheck,
  History,
} from 'lucide-react'
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
import { Skeleton } from '@/components/ui/skeleton'
import { useBackupStatus, useJwtRotationHistory } from '@/hooks/use-settings'
import type { BackupRunEntry } from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'

function relativeTime(ts?: string): string {
  if (!ts) return '–'
  const diff = Date.now() - new Date(ts).getTime()
  const mins = Math.floor(diff / 60000)
  const hours = Math.floor(mins / 60)
  const days = Math.floor(hours / 24)
  if (days > 0) return `${days}d ago`
  if (hours > 0) return `${hours}h ago`
  if (mins > 0) return `${mins}m ago`
  return 'just now'
}

function statusColor(status: string) {
  switch (status) {
    case 'succeeded':
    case 'ok':
      return 'var(--color-success)'
    case 'running':
      return 'var(--color-warning)'
    case 'failed':
    case 'unhealthy':
      return 'var(--color-danger)'
    default:
      return 'var(--color-text-tertiary)'
  }
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-6 w-6 flex-shrink-0 text-text-tertiary hover:text-text-primary transition-colors"
      onClick={handleCopy}
      title="Copy to clipboard"
    >
      {copied ? <Check className="h-3 w-3" style={{ color: 'var(--color-success)' }} /> : <Copy className="h-3 w-3" />}
    </Button>
  )
}

const SCHEDULE_CONFIG = [
  { kind: 'Daily', cron: '@daily', description: 'Every day at midnight UTC' },
  { kind: 'Weekly', cron: '@weekly', description: 'Every Sunday at midnight UTC' },
  { kind: 'Monthly', cron: '@monthly', description: 'First of month at midnight UTC' },
]

function BackupSection() {
  const { data: backup, isLoading, isError, refetch } = useBackupStatus()

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-16 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load backup status</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch backup data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  const latestByKind: Record<string, BackupRunEntry | undefined> = {
    Daily: backup?.last_daily,
    Weekly: backup?.last_weekly,
    Monthly: backup?.last_monthly,
  }

  return (
    <div className="space-y-4">
      {/* Schedule cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        {SCHEDULE_CONFIG.map(({ kind, cron, description }) => {
          const run = latestByKind[kind]
          return (
            <Card key={kind} className="relative overflow-hidden">
              <div
                className="absolute top-0 left-0 right-0 h-[2px]"
                style={{
                  backgroundColor: run
                    ? statusColor(run.status)
                    : 'var(--color-text-tertiary)',
                }}
              />
              <CardContent className="p-4">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">{kind}</span>
                  <span className="font-mono text-[10px] text-text-tertiary">{cron}</span>
                </div>
                <p className="text-[11px] text-text-tertiary mb-2">{description}</p>
                {isLoading ? (
                  <Skeleton className="h-5 w-20" />
                ) : run ? (
                  <div className="flex items-center gap-2">
                    <Badge
                      variant="outline"
                      className="text-[10px] h-4 px-1.5"
                      style={{ borderColor: `${statusColor(run.status)}40`, color: statusColor(run.status) }}
                    >
                      {run.status}
                    </Badge>
                    <span className="text-[11px] text-text-secondary">{relativeTime(run.finished_at ?? run.started_at)}</span>
                  </div>
                ) : (
                  <span className="text-[11px] text-text-tertiary">Not yet run</span>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>

      {/* History table */}
      <Card className="overflow-hidden density-compact">
        <CardHeader className="pb-0 px-4 pt-4">
          <CardTitle className="text-sm font-medium text-text-primary flex items-center gap-2">
            <History className="h-4 w-4 text-text-tertiary" />
            Backup History (last 30)
          </CardTitle>
        </CardHeader>
        <div className="overflow-x-auto mt-3">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>S3 Key</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 5 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && (!backup?.history || backup.history.length === 0) && (
                <TableRow>
                  <TableCell colSpan={5}>
                    <div className="flex flex-col items-center justify-center py-12 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Archive className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No backups yet</h3>
                        <p className="text-xs text-text-secondary max-w-xs">
                          Backups start after{' '}
                          <span className="font-mono">BACKUP_ENABLED=true</span>{' '}
                          and first @daily cron fires.
                        </p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(backup?.history ?? []).map((run, idx) => (
                <TableRow key={`${run.kind}-${run.started_at}-${idx}`}>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(run.started_at).toLocaleString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary" className="text-[10px]">{run.kind}</Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1.5">
                      <span
                        className="h-1.5 w-1.5 rounded-full flex-shrink-0"
                        style={{ backgroundColor: statusColor(run.status) }}
                      />
                      <span className="text-xs" style={{ color: statusColor(run.status) }}>
                        {run.status}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">
                      {run.size_mb > 0 ? `${run.size_mb.toFixed(1)} MB` : '–'}
                    </span>
                  </TableCell>
                  <TableCell>
                    {run.s3_key ? (
                      <div className="flex items-center gap-1.5 max-w-xs">
                        <span className="font-mono text-[11px] text-text-tertiary truncate" title={run.s3_key}>
                          {run.s3_key}
                        </span>
                        <CopyButton text={run.s3_key} />
                      </div>
                    ) : (
                      <span className="text-xs text-text-tertiary">–</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && backup?.history && backup.history.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {backup.history.length} backup run{backup.history.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>
    </div>
  )
}

function JWTRotationSection() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: jwtData, isLoading, isError, refetch } = useJwtRotationHistory()

  if (!isSuperAdmin) {
    return (
      <div className="rounded-xl border border-border bg-bg-surface p-6 text-center">
        <ShieldCheck className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
        <h3 className="text-sm font-semibold text-text-primary mb-1">Super Admin Required</h3>
        <p className="text-xs text-text-secondary">JWT rotation history is visible to super_admin only.</p>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-12 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-6 text-center">
          <AlertCircle className="h-8 w-8 text-danger mx-auto mb-2" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">Failed to load rotation history</h3>
          <Button onClick={() => refetch()} variant="outline" size="sm" className="gap-2 mt-2">
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Fingerprints */}
      <Card>
        <CardContent className="p-4 space-y-3">
          <div>
            <div className="flex items-center gap-2 mb-1.5">
              <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Current Key Fingerprint</span>
            </div>
            {isLoading ? (
              <Skeleton className="h-7 w-48" />
            ) : (
              <div className="flex items-center gap-2">
                <div
                  className="flex items-center gap-2 px-3 py-1.5 rounded-[var(--radius-sm)] border"
                  style={{ borderColor: 'var(--color-accent)30', backgroundColor: 'var(--color-accent)08' }}
                >
                  <span className="font-mono text-[12px]" style={{ color: 'var(--color-accent)' }}>
                    {jwtData?.current_fingerprint ?? '–'}
                  </span>
                </div>
                {jwtData?.current_fingerprint && <CopyButton text={jwtData.current_fingerprint} />}
              </div>
            )}
          </div>

          {(isLoading || jwtData?.previous_fingerprint) && (
            <div>
              <div className="flex items-center gap-2 mb-1.5">
                <span className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary">Previous Key Fingerprint</span>
              </div>
              {isLoading ? (
                <Skeleton className="h-7 w-48" />
              ) : (
                <div className="flex items-center gap-2">
                  <div
                    className="flex items-center gap-2 px-3 py-1.5 rounded-[var(--radius-sm)] border border-border"
                    style={{ backgroundColor: 'var(--color-bg-elevated)' }}
                  >
                    <span className="font-mono text-[12px] text-text-secondary">
                      {jwtData?.previous_fingerprint}
                    </span>
                  </div>
                  {jwtData?.previous_fingerprint && <CopyButton text={jwtData.previous_fingerprint} />}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Rotation history table */}
      <Card className="overflow-hidden density-compact">
        <CardHeader className="pb-0 px-4 pt-4">
          <CardTitle className="text-sm font-medium text-text-primary flex items-center gap-2">
            <History className="h-4 w-4 text-text-tertiary" />
            Rotation History
          </CardTitle>
        </CardHeader>
        <div className="overflow-x-auto mt-3">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Actor</TableHead>
                <TableHead>Correlation ID</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 3 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-24" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && (!jwtData?.history || jwtData.history.length === 0) && (
                <TableRow>
                  <TableCell colSpan={3}>
                    <div className="flex flex-col items-center justify-center py-10 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-5 shadow-[var(--shadow-card)]">
                        <KeyRound className="h-7 w-7 text-text-tertiary mx-auto mb-2" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No rotations recorded</h3>
                        <p className="text-xs text-text-secondary">
                          Key rotations are logged when{' '}
                          <span className="font-mono">JWT_SECRET_PREVIOUS</span>{' '}
                          is set on startup.
                        </p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(jwtData?.history ?? []).map((entry, idx) => (
                <TableRow key={`${entry.correlation_id}-${idx}`}>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(entry.when).toLocaleString()}
                    </span>
                    <div className="font-mono text-[10px] text-text-tertiary">
                      {relativeTime(entry.when)}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{entry.actor}</span>
                  </TableCell>
                  <TableCell>
                    {entry.correlation_id ? (
                      <div className="flex items-center gap-1.5">
                        <span className="font-mono text-[11px] text-text-tertiary">{entry.correlation_id}</span>
                        <CopyButton text={entry.correlation_id} />
                      </div>
                    ) : (
                      <span className="text-xs text-text-tertiary">–</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && jwtData?.history && jwtData.history.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {jwtData.history.length} rotation{jwtData.history.length !== 1 ? 's' : ''} recorded
            </p>
          </div>
        )}
      </Card>
    </div>
  )
}

export default function ReliabilityPage() {
  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Reliability</h1>
      </div>

      {/* Backup Schedule & Retention */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <CalendarClock className="h-4 w-4 text-text-tertiary" />
          <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary">
            Backup Schedule & Retention
          </h2>
        </div>
        <BackupSection />
      </div>

      {/* JWT Signing Key Rotation */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-text-tertiary" />
          <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary">
            JWT Signing Key Rotation
          </h2>
        </div>
        <JWTRotationSection />
      </div>
    </div>
  )
}
