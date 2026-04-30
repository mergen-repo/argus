import { useState } from 'react'
import { Download, Eye, AlertTriangle, FileBarChart } from 'lucide-react'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { EmptyState } from '@/components/shared/empty-state'
import { useSLAMonthDetail, useSLAPDFDownload, SLANotAvailableError } from '@/hooks/use-sla'
import { SLAOperatorBreachPanel } from './operator-breach'
import { cn } from '@/lib/utils'
import { classifyUptime, uptimeStatusColor } from '@/lib/sla'
import type { SLAOperatorMonthAgg } from '@/types/sla'

const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
]

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  year: number
  month: number
}

interface BreachTarget {
  operatorId: string
  operatorName: string
  operatorCode: string
}

function formatMttr(secs: number): string {
  if (secs < 60) return `${Math.round(secs)}s`
  const m = Math.floor(secs / 60)
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

function KpiChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2">
      <span className="text-[9px] uppercase tracking-[1.5px] text-text-tertiary font-medium">{label}</span>
      <span className="font-mono text-sm font-bold text-text-primary mt-0.5 tabular-nums">{value}</span>
    </div>
  )
}

function OperatorRow({
  row, year, month, onViewBreaches,
}: {
  row: SLAOperatorMonthAgg
  year: number
  month: number
  onViewBreaches: (t: BreachTarget) => void
}) {
  const status = classifyUptime(row.uptime_pct, row.sla_uptime_target)
  const palette = uptimeStatusColor(status)
  const { download, pending } = useSLAPDFDownload()

  return (
    <TableRow>
      <TableCell>
        <div>
          <p className="text-sm font-medium text-text-primary leading-none">{row.operator_name}</p>
          <p className="font-mono text-[10px] text-text-tertiary mt-0.5">{row.operator_code}</p>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col gap-1 min-w-[80px]">
          <span className={cn('font-mono text-sm font-bold tabular-nums', palette.text)}>
            {row.uptime_pct.toFixed(3)}%
          </span>
          <div className="h-1 w-full rounded-full bg-bg-hover overflow-hidden">
            <div
              className={cn('h-full rounded-full transition-all duration-500', palette.bar)}
              style={{ width: `${Math.min(row.uptime_pct, 100)}%` }}
            />
          </div>
          <span className="text-[9px] text-text-tertiary font-mono">
            target {row.sla_uptime_target.toFixed(1)}%
          </span>
        </div>
      </TableCell>
      <TableCell className="font-mono text-sm text-text-primary tabular-nums">
        {row.incident_count}
      </TableCell>
      <TableCell className="font-mono text-sm text-text-primary tabular-nums">
        {row.breach_minutes} min
      </TableCell>
      <TableCell className="font-mono text-sm text-text-primary tabular-nums">
        {row.latency_p95_ms} ms
      </TableCell>
      <TableCell className="font-mono text-sm text-text-secondary tabular-nums">
        {row.sessions_total.toLocaleString()}
      </TableCell>
      <TableCell>
        <Button
          type="button"
          variant="link"
          size="sm"
          onClick={(e) => {
            e.stopPropagation()
            void download({ year, month, operatorId: row.operator_id, operatorCode: row.operator_code })
          }}
          disabled={pending}
          className={cn(
            'h-auto p-0 gap-1 font-mono text-[10px] text-accent hover:text-accent/70 hover:no-underline',
            pending && 'cursor-wait',
          )}
          aria-label={`Download PDF for ${row.operator_name}`}
        >
          <Download className={cn('h-3 w-3', pending && 'animate-pulse')} />
          {pending ? 'PDF…' : 'PDF'}
        </Button>
      </TableCell>
      <TableCell>
        <Button
          size="sm"
          variant="ghost"
          className="h-7 px-2 text-xs text-text-secondary hover:text-text-primary"
          onClick={() => onViewBreaches({ operatorId: row.operator_id, operatorName: row.operator_name, operatorCode: row.operator_code })}
        >
          <Eye className="h-3 w-3 mr-1" />
          View
        </Button>
      </TableCell>
    </TableRow>
  )
}

export function SLAMonthDetailPanel({ open, onOpenChange, year, month }: Props) {
  const { data, isLoading, isError, error } = useSLAMonthDetail(year, month)
  const [breachTarget, setBreachTarget] = useState<BreachTarget | null>(null)
  const monthLabel = `${MONTH_NAMES[month - 1] ?? ''} ${year}`
  const notAvailable = error instanceof SLANotAvailableError

  const overall = data?.overall
  const avgMttr = data?.operators.length
    ? data.operators.reduce((s, o) => s + o.mttr_sec, 0) / data.operators.length
    : 0
  const avgLatency = data?.operators.length
    ? data.operators.reduce((s, o) => s + o.latency_p95_ms, 0) / data.operators.length
    : 0

  return (
    <>
      <SlidePanel
        open={open}
        onOpenChange={onOpenChange}
        title={`Month Detail — ${monthLabel}`}
        description="SLA performance breakdown by operator"
        width="xl"
      >
        {isLoading && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-[var(--radius-sm)]" />
              ))}
            </div>
            <Skeleton className="h-48 w-full rounded-[var(--radius-sm)]" />
          </div>
        )}

        {!isLoading && notAvailable && (
          <EmptyState
            icon={FileBarChart}
            title="No SLA data for this month"
            description={`No SLA rollup exists for ${monthLabel}. Either the reporting job hasn't run yet, or this month predates your retention window.`}
          />
        )}

        {!isLoading && isError && !notAvailable && (
          <div className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            Failed to load month detail.
          </div>
        )}

        {data && !isLoading && overall && (
          <div className="space-y-5">
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2">
              <KpiChip
                label="Overall Uptime"
                value={`${overall.uptime_pct.toFixed(3)}%`}
              />
              <KpiChip
                label="Incidents"
                value={String(overall.incident_count)}
              />
              <KpiChip
                label="Breach Min"
                value={`${overall.breach_minutes}`}
              />
              <KpiChip
                label="MTTR Avg"
                value={formatMttr(avgMttr)}
              />
              <KpiChip
                label="Avg Latency p95"
                value={`${Math.round(avgLatency)} ms`}
              />
            </div>

            <div className="rounded-[var(--radius-md)] border border-border overflow-hidden">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Operator</TableHead>
                    <TableHead>Uptime</TableHead>
                    <TableHead>Incidents</TableHead>
                    <TableHead>Breach</TableHead>
                    <TableHead>Latency p95</TableHead>
                    <TableHead>Sessions</TableHead>
                    <TableHead>PDF</TableHead>
                    <TableHead>Breaches</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.operators.map((op) => (
                    <OperatorRow
                      key={op.operator_id}
                      row={op}
                      year={year}
                      month={month}
                      onViewBreaches={setBreachTarget}
                    />
                  ))}
                  {data.operators.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={8} className="py-10 text-center text-text-tertiary text-sm">
                        No operator data for this month.
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </div>
        )}
      </SlidePanel>

      {breachTarget && (
        <SLAOperatorBreachPanel
          open={Boolean(breachTarget)}
          onOpenChange={(v) => { if (!v) setBreachTarget(null) }}
          operatorId={breachTarget.operatorId}
          operatorName={breachTarget.operatorName}
          operatorCode={breachTarget.operatorCode}
          year={year}
          month={month}
        />
      )}
    </>
  )
}
