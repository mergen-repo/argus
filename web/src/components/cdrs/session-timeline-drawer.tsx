import { useMemo } from 'react'
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { EntityLink } from '@/components/shared/entity-link'
import { CopyableId } from '@/components/shared/copyable-id'
import { EmptyState } from '@/components/shared/empty-state'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { formatBytes, formatDuration } from '@/lib/format'
import { formatCDRTime, formatCDRTimestamp } from '@/lib/time'
import { useSessionTimeline, type CDR } from '@/hooks/use-cdrs'
import { recordTypeBadgeClass } from '@/lib/cdr'
import { ExternalLink } from 'lucide-react'

interface SessionTimelineDrawerProps {
  sessionID: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onNavigateDetail?: (sessionID: string) => void
}

interface TimelineRow {
  cdr: CDR
  deltaIn: number
  deltaOut: number
  cumulative: number
}

function buildTimelineRows(items: CDR[]): TimelineRow[] {
  const rows: TimelineRow[] = []
  let cumulative = 0
  let prevIn = 0
  let prevOut = 0
  for (let i = 0; i < items.length; i++) {
    const c = items[i]
    // Δ uses MAX-guard to absorb counter resets on session stop.
    const deltaIn = i === 0 ? 0 : Math.max(0, c.bytes_in - prevIn)
    const deltaOut = i === 0 ? 0 : Math.max(0, c.bytes_out - prevOut)
    cumulative += deltaIn + deltaOut
    if (i === 0) {
      cumulative = c.bytes_in + c.bytes_out
    }
    rows.push({ cdr: c, deltaIn, deltaOut, cumulative })
    prevIn = c.bytes_in
    prevOut = c.bytes_out
  }
  return rows
}

export function SessionTimelineDrawer({ sessionID, open, onOpenChange, onNavigateDetail }: SessionTimelineDrawerProps) {
  const { data, isLoading, error } = useSessionTimeline(sessionID ?? undefined)

  const timelineRows = useMemo(() => {
    if (!data?.items) return [] as TimelineRow[]
    return buildTimelineRows(data.items)
  }, [data])

  const chartData = useMemo(
    () => timelineRows.map((r) => ({
      label: formatCDRTime(r.cdr.timestamp),
      cumulative: r.cumulative,
    })),
    [timelineRows],
  )

  const firstTs = data?.items?.[0]?.timestamp
  const lastTs = data?.items?.[data.items.length - 1]?.timestamp
  const firstRow = data?.items?.[0]

  return (
    <SlidePanel
      open={open}
      onOpenChange={onOpenChange}
      title="Oturum Zaman Çizelgesi"
      description={sessionID ?? ''}
      width="xl"
    >
      {isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-48 w-full" />
          <Skeleton className="h-56 w-full" />
        </div>
      )}

      {error && (
        <EmptyState
          title="Oturum bulunamadı"
          description="Bu oturum için CDR kaydı yok (veya başka tenant'a ait)."
        />
      )}

      {data && (
        <div className="space-y-5">
          {/* Metadata header (F-U6) */}
          <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface p-4 space-y-2">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-[12px]">
              {firstRow && (
                <>
                  <MetaField label="SIM">
                    <EntityLink entityType="sim" entityId={firstRow.sim_id} truncate />
                  </MetaField>
                  <MetaField label="Operatör">
                    <EntityLink entityType="operator" entityId={firstRow.operator_id} truncate />
                  </MetaField>
                  <MetaField label="APN">
                    {firstRow.apn_id ? (
                      <EntityLink entityType="apn" entityId={firstRow.apn_id} truncate />
                    ) : (
                      <span className="text-text-tertiary">—</span>
                    )}
                  </MetaField>
                </>
              )}
              <MetaField label="Süre">
                <span className="font-mono text-text-primary">{formatDuration(data.stats?.duration_sec ?? 0)}</span>
              </MetaField>
              <MetaField label="Başlangıç">
                <span className="font-mono text-text-primary text-[11px]">
                  {firstTs ? formatCDRTimestamp(firstTs) : '—'}
                </span>
              </MetaField>
              <MetaField label="Son">
                <span className="font-mono text-text-primary text-[11px]">
                  {lastTs ? formatCDRTimestamp(lastTs) : '—'}
                </span>
              </MetaField>
            </div>
          </div>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <StatCell label="Satır" value={data.count.toLocaleString('tr-TR')} />
            <StatCell label="↓ Bayt" value={formatBytes(data.stats?.total_bytes_in ?? 0)} tone="success" />
            <StatCell label="↑ Bayt" value={formatBytes(data.stats?.total_bytes_out ?? 0)} tone="accent" />
            <StatCell label="Süre" value={formatDuration(data.stats?.duration_sec ?? 0)} />
          </div>

          <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface p-3">
            <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium mb-2">
              Kümülatif Bayt (start → stop)
            </p>
            <div className="h-44">
              <ResponsiveContainer>
                <LineChart data={chartData} margin={{ top: 8, right: 12, left: 0, bottom: 4 }}>
                  <XAxis
                    dataKey="label"
                    stroke="var(--text-tertiary)"
                    fontSize={10}
                    tick={{ fill: 'var(--text-tertiary)' }}
                  />
                  <YAxis
                    stroke="var(--text-tertiary)"
                    fontSize={10}
                    tick={{ fill: 'var(--text-tertiary)' }}
                    tickFormatter={(v: number) => formatBytes(v)}
                  />
                  <Tooltip
                    contentStyle={{ background: 'var(--color-bg-surface)', border: '1px solid var(--color-border)', fontSize: 11 }}
                    formatter={(v) => formatBytes(typeof v === 'number' ? v : Number(v ?? 0))}
                  />
                  <Line type="monotone" dataKey="cumulative" stroke="var(--color-accent)" dot={false} strokeWidth={1.5} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="space-y-2">
            <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
              CDR Kayıtları ({data.count})
            </p>
            <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface max-h-[340px] overflow-y-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ZAMAN</TableHead>
                    <TableHead>TİP</TableHead>
                    <TableHead className="text-right">↓ BYTES</TableHead>
                    <TableHead className="text-right">Δ↓</TableHead>
                    <TableHead className="text-right">↑ BYTES</TableHead>
                    <TableHead className="text-right">Δ↑</TableHead>
                    <TableHead className="text-right">KÜMÜLATİF</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {timelineRows.map((r) => (
                    <TableRow key={r.cdr.id}>
                      <TableCell>
                        <span className="font-mono text-[11px] text-text-tertiary" title={formatCDRTimestamp(r.cdr.timestamp)}>
                          {formatCDRTime(r.cdr.timestamp)}
                        </span>
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary" className={`text-[10px] font-mono uppercase ${recordTypeBadgeClass(r.cdr.record_type)}`}>
                          {r.cdr.record_type}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono text-[11px] text-success">{formatBytes(r.cdr.bytes_in)}</span>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono text-[11px] text-text-secondary">
                          {r.deltaIn > 0 ? `+${formatBytes(r.deltaIn)}` : '—'}
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono text-[11px] text-accent">{formatBytes(r.cdr.bytes_out)}</span>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono text-[11px] text-text-secondary">
                          {r.deltaOut > 0 ? `+${formatBytes(r.deltaOut)}` : '—'}
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono text-[11px] text-text-primary">{formatBytes(r.cumulative)}</span>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <CopyableId value={sessionID ?? ''} mono />
          </div>
        </div>
      )}

      {sessionID && (
        <SlidePanelFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Kapat
          </Button>
          <Button
            variant="default"
            onClick={() => {
              if (onNavigateDetail) onNavigateDetail(sessionID)
            }}
            className="gap-1.5"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Oturum detayına git
          </Button>
        </SlidePanelFooter>
      )}
    </SlidePanel>
  )
}

function MetaField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">{label}</p>
      <div className="mt-0.5">{children}</div>
    </div>
  )
}

function StatCell({ label, value, tone }: { label: string; value: string; tone?: 'success' | 'accent' }) {
  const toneClass = tone === 'success' ? 'text-success' : tone === 'accent' ? 'text-accent' : 'text-text-primary'
  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-bg-surface px-3 py-2">
      <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">{label}</p>
      <p className={`font-mono text-[15px] font-bold leading-none mt-1 ${toneClass}`}>{value}</p>
    </div>
  )
}
