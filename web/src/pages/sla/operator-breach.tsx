import { ShieldCheck, AlertTriangle, Download } from 'lucide-react'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useSLAOperatorBreaches, useSLAPDFDownload } from '@/hooks/use-sla'
import { cn } from '@/lib/utils'

const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
]

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  operatorId: string | null
  operatorName: string
  operatorCode?: string
  year: number
  month: number
}

function causeBadgeClass(cause: 'down' | 'latency' | 'mixed'): string {
  if (cause === 'down') return 'bg-danger/15 text-danger border border-danger/30'
  if (cause === 'latency') return 'bg-warning/15 text-warning border border-warning/30'
  return 'bg-purple/15 text-purple border border-purple/30'
}

function causeLabel(cause: 'down' | 'latency' | 'mixed'): string {
  if (cause === 'down') return 'DOWN'
  if (cause === 'latency') return 'LATENCY'
  return 'MIXED'
}

function formatDuration(secs: number): string {
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  if (m < 60) return s > 0 ? `${m}m ${s}s` : `${m}m`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm > 0 ? `${h}h ${rm}m` : `${h}h`
}

function formatTs(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString('en-GB', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hour12: false,
  }).replace(',', '')
}

export function SLAOperatorBreachPanel({ open, onOpenChange, operatorId, operatorName, operatorCode, year, month, }: Props) {
  const { data, isLoading, isError } = useSLAOperatorBreaches(operatorId, year, month)
  const { download, pending: pdfPending } = useSLAPDFDownload()
  const monthLabel = `${MONTH_NAMES[month - 1] ?? ''} ${year}`

  const breaches = data?.data.breaches ?? []
  const totals = data?.data.totals

  return (
    <SlidePanel
      open={open}
      onOpenChange={onOpenChange}
      title={`Breaches — ${operatorName}`}
      description={monthLabel}
      width="lg"
    >
      {isLoading && (
        <div className="space-y-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full rounded-[var(--radius-sm)]" />
          ))}
        </div>
      )}

      {isError && (
        <div className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          Failed to load breach data.
        </div>
      )}

      {data && !isLoading && (
        <div className="space-y-4">
          {data.meta.breach_source === 'persisted' && (
            <div className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-info/30 bg-info/10 px-4 py-2.5 text-xs text-info">
              <span className="shrink-0">i</span>
              Historical data (&gt;90 days old, from stored rollup)
            </div>
          )}

          {totals && (
            <div className="flex items-center justify-between rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-4 py-3">
              <span className="text-xs font-mono uppercase tracking-[1.5px] text-text-tertiary">
                Totals
              </span>
              <div className="flex items-center gap-4 font-mono text-xs text-text-secondary">
                <span>
                  <span className="font-bold text-text-primary">{totals.breaches_count}</span> breaches
                </span>
                <span className="text-text-tertiary">·</span>
                <span>
                  <span className="font-bold text-text-primary">{formatDuration(totals.downtime_seconds)}</span> downtime
                </span>
                <span className="text-text-tertiary">·</span>
                <span>
                  ~<span className="font-bold text-text-primary">{totals.affected_sessions_est.toLocaleString()}</span> sessions
                </span>
              </div>
            </div>
          )}

          {breaches.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-14 text-center">
              <ShieldCheck className="h-10 w-10 text-success mb-3" />
              <p className="text-sm font-semibold text-text-primary">No breaches recorded</p>
              <p className="mt-1 text-xs text-text-secondary">
                {operatorName} was fully compliant in {monthLabel}
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {breaches.map((breach, i) => (
                <div
                  key={i}
                  className="flex items-start gap-3 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-4 py-3 transition-colors hover:bg-bg-hover"
                >
                  <span
                    className={cn(
                      'mt-0.5 shrink-0 rounded px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider',
                      causeBadgeClass(breach.cause),
                    )}
                  >
                    {causeLabel(breach.cause)}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="font-mono text-xs text-text-secondary">
                      {formatTs(breach.started_at)}
                      <span className="mx-1.5 text-text-tertiary">→</span>
                      {formatTs(breach.ended_at)}
                    </p>
                    <div className="mt-1 flex items-center gap-3 flex-wrap">
                      <span className="font-mono text-sm font-bold text-text-primary">
                        {formatDuration(breach.duration_sec)}
                      </span>
                      <Badge variant="outline" className="font-mono text-[10px] h-5">
                        ~{breach.affected_sessions_est.toLocaleString()} sessions
                      </Badge>
                      <span className="text-xs text-text-tertiary">
                        {breach.samples_count} samples
                      </span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {operatorId && (
        <SlidePanelFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void download({ year, month, operatorId, operatorCode })}
            disabled={pdfPending}
            className={cn(
              'gap-1.5 text-xs',
              pdfPending && 'opacity-70 cursor-wait',
            )}
          >
            <Download className={cn('h-3.5 w-3.5', pdfPending && 'animate-pulse')} />
            {pdfPending ? 'Preparing PDF…' : 'Download operator-month PDF'}
          </Button>
        </SlidePanelFooter>
      )}
    </SlidePanel>
  )
}
